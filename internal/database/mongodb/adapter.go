package mongodb

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/toolresolve"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"gopkg.in/yaml.v3"
)

type Adapter struct {
	Config   config.Database
	Password string
	Runner   runner.Runner
	Probe    func(context.Context) (string, error)
}

func (*Adapter) Name() string      { return "mongodb" }
func (*Adapter) Format() string    { return "archive" }
func (*Adapter) Extension() string { return ".archive" }
func (*Adapter) Capabilities() database.Capabilities {
	return database.Capabilities{ConnectionTest: true, FullBackup: true, FullRestore: true, SelectiveBackup: true, SelectiveRestore: true, StreamingBackup: true}
}
func (a *Adapter) probe(ctx context.Context) (string, error) {
	if a.Probe != nil {
		return a.Probe(ctx)
	}
	auth := a.Config.AuthDatabase
	if auth == "" {
		auth = "admin"
	}
	opts := options.Client().SetHosts([]string{net.JoinHostPort(a.Config.Host, strconv.Itoa(a.Config.Port))}).SetAuth(options.Credential{Username: a.Config.User, Password: a.Password, AuthSource: auth}).SetServerSelectionTimeout(10 * time.Second).SetConnectTimeout(10 * time.Second)
	if a.Config.SSLMode == "require" || a.Config.SSLMode == "verify-full" {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		return "", err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		client.Disconnect(c)
	}()
	if err = client.Ping(ctx, readpref.Primary()); err != nil {
		return "", err
	}
	var build struct {
		Version string `bson:"version"`
	}
	err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&build)
	return build.Version, err
}

type bounded struct{ data []byte }

func (b *bounded) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 64<<10 {
		return 0, fmt.Errorf("tool diagnostics exceed limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

var toolVersion = regexp.MustCompile(`(?:(?:version:|version)\s*)?(100\.\d+\.\d+)`)

func ValidateToolVersions(dump, restore string) error {
	d := toolVersion.FindStringSubmatch(dump)
	r := toolVersion.FindStringSubmatch(restore)
	if len(d) != 2 || len(r) != 2 {
		return fmt.Errorf("requires valid MongoDB Database Tools version 100.x")
	}
	if d[1] != r[1] {
		return fmt.Errorf("mongodump and mongorestore must use the same Database Tools version")
	}
	return nil
}

func (a *Adapter) Preflight(ctx context.Context) (database.Info, error) {
	info := database.Info{}
	paths := make([]string, 0, 2)
	for _, tool := range []string{"mongodump", "mongorestore"} {
		path, err := toolresolve.Resolve(tool, a.Config.Tools[tool], a.Runner)
		if err != nil {
			return info, err
		}
		paths = append(paths, path)
		b := &bounded{}
		if err := a.Runner.Run(ctx, runner.Spec{Executable: path, Args: []string{"--version"}, Stdout: b}); err != nil {
			return info, fault.Wrap(fault.Dependency, "discover "+tool, err)
		}
		v := toolVersion.FindStringSubmatch(string(b.data))
		if len(v) != 2 {
			return info, fault.Wrap(fault.Unsupported, "MongoDB Database Tools", fmt.Errorf("requires version 100.x"))
		}
		if tool == "mongodump" {
			info.ToolVersion = v[1]
		} else {
			info.RestoreToolVersion = v[1]
		}
	}
	if err := toolresolve.SameToolchain(paths); err != nil {
		return info, fault.Wrap(fault.Dependency, "MongoDB toolchain", err)
	}
	if err := ValidateToolVersions(info.ToolVersion, info.RestoreToolVersion); err != nil {
		return info, fault.Wrap(fault.Unsupported, "MongoDB tool compatibility", err)
	}
	v, err := a.probe(ctx)
	info.ServerVersion = v
	return info, fault.Wrap(fault.Connection, "MongoDB authenticated connection test", err)
}
func (a *Adapter) run(ctx context.Context, tool string, args []string, r io.Reader, w io.Writer) error {
	auth := a.Config.AuthDatabase
	if auth == "" {
		auth = "admin"
	}
	// Tools support a credential-only YAML file; never expose passwords via argv.
	f, err := os.CreateTemp("", "dbvault-mongo-credentials-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	b, err := yaml.Marshal(map[string]string{"password": a.Password})
	if err == nil {
		_, err = f.Write(b)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	base := []string{"--config=" + f.Name(), "--host=" + a.Config.Host, "--port=" + strconv.Itoa(a.Config.Port), "--username=" + a.Config.User, "--authenticationDatabase=" + auth}
	if a.Config.SSLMode == "require" || a.Config.SSLMode == "verify-full" {
		base = append(base, "--ssl")
	}
	base = append(base, args...)
	path, err := toolresolve.Resolve(tool, a.Config.Tools[tool], a.Runner)
	if err != nil {
		return err
	}
	return a.Runner.Run(ctx, runner.Spec{Executable: path, Args: base, Stdin: r, Stdout: w})
}
func (a *Adapter) Dump(ctx context.Context, w io.Writer) error {
	if !a.Config.Options.Quiesced {
		return fault.Wrap(fault.Unsupported, "MongoDB snapshot consistency", fmt.Errorf("database.options.quiesced must be true: stop writes during database-scoped logical dumps"))
	}
	args := []string{"--archive", "--db=" + a.Config.Database}
	if len(a.Config.Options.IncludeCollections) > 1 {
		return fault.Wrap(fault.Unsupported, "MongoDB collection filter", fmt.Errorf("one included collection per archive is supported"))
	}
	for _, v := range a.Config.Options.IncludeCollections {
		args = append(args, "--collection="+v)
	}
	for _, v := range a.Config.Options.ExcludeCollections {
		args = append(args, "--excludeCollection="+v)
	}
	return a.run(ctx, "mongodump", args, nil, w)
}
func (a *Adapter) Restore(ctx context.Context, r io.Reader, o database.RestoreOptions) error {
	for _, collection := range o.Collections {
		if collection == "" || strings.ContainsAny(collection, "*?[]\x00\r\n") {
			return fault.Wrap(fault.Unsupported, "MongoDB collection selector", fmt.Errorf("collection selectors must be literal nonempty names"))
		}
	}
	if len(o.Tables) > 0 || len(o.Schemas) > 0 {
		return fault.Wrap(fault.Unsupported, "MongoDB restore", fmt.Errorf("use --collection rather than table/schema filters"))
	}
	source := o.SourceDatabase
	if source == "" {
		source = a.Config.Database
	}
	args := []string{"--archive", "--stopOnError", "--numParallelCollections=1", "--nsInclude=" + source + ".*"}
	if source != a.Config.Database {
		args = append(args, "--nsFrom="+source+".*", "--nsTo="+a.Config.Database+".*")
	}
	if o.Clean {
		args = append(args, "--drop")
	}
	for _, v := range o.Collections {
		args = append(args, "--nsInclude="+source+"."+v)
	}
	if len(o.Collections) > 0 {
		args = remove(args, "--nsInclude="+source+".*")
	}
	return a.run(ctx, "mongorestore", args, r, io.Discard)
}
func remove(a []string, s string) []string {
	result := []string{}
	for _, v := range a {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
func (a *Adapter) Compatible(target database.Info, server, tool string) error {
	src, _, _ := strings.Cut(server, ".")
	dst, _, _ := strings.Cut(target.ServerVersion, ".")
	if src == "" || src != dst {
		return fmt.Errorf("MongoDB restore requires the same server major version")
	}
	if tool != target.RestoreToolVersion {
		return fmt.Errorf("mongorestore must match the source tools version")
	}
	return nil
}
