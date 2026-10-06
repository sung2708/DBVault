// Package recoveryserver confines recovery to a newly created Docker server.
// The Docker daemon and preloaded official image are operator-trusted.
package recoveryserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/mongodb"
	"github.com/sung2708/DBVault/internal/database/mysql"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/security"
	"os"
	"path/filepath"
)

const MySQLValidation = "MySQL CHECK TABLE and table readability checks"
const MongoValidation = "MongoDB collection validate and readability checks"

var databaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var dockerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Target struct {
	database.Adapter
	Config         config.Database
	Password       string
	engine         string
	native         runner.Runner
	docker         string
	token, imageID string
	ID             string
	Image          string
	major          int
}

func New(engine, name, source string, native runner.Runner) (*Target, error) {
	if err := database.ValidateNewName(name); err != nil {
		return nil, err
	}
	if name == source {
		return nil, fmt.Errorf("recovery name matches source")
	}
	if engine != "mysql" && engine != "mongodb" {
		return nil, fmt.Errorf("unsupported recovery engine")
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(secret[:32])
	if native == nil {
		native = runner.Native{Redactor: security.New(password)}
	}
	t := &Target{native: native, token: hex.EncodeToString(secret[32:]), Password: password, engine: engine}
	t.Config = config.Database{Type: engine, Host: "127.0.0.1", Port: config.DefaultPort(engine), User: "root", Database: name, SSLMode: "disable", AuthDatabase: "admin", Options: config.Options{Quiesced: true}}
	r := &containerRunner{target: t}
	if engine == "mysql" {
		t.Config.SSLMode = "require"
		t.Adapter = &mysql.Adapter{Config: t.Config, Password: password, Runner: r}
	} else {
		t.Adapter = &mongodb.Adapter{Config: t.Config, Password: password, Runner: r, Probe: t.mongoVersion}
	}
	return t, nil
}

func (t *Target) capture(ctx context.Context, args ...string) (string, error) {
	var b limitedOutput
	err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: args, Stdout: &b})
	return strings.TrimSpace(b.String()), err
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("Docker diagnostic output exceeds limit")
	}
	return b.Buffer.Write(p)
}

// CheckImage never pulls images or creates a container, including in dry-run.
func (t *Target) CheckImage(ctx context.Context, version, toolVersion string) error {
	var err error
	if t.engine == "mysql" {
		series, e := mysql.Series(version)
		if e != nil {
			return e
		}
		tools, e := mysql.Series(toolVersion)
		if e != nil || tools != series {
			return fmt.Errorf("MySQL source tool/server series mismatch")
		}
		t.Image = "mysql:" + series
	} else {
		if err := mongodb.ValidateToolVersions(toolVersion, toolVersion); err != nil {
			return err
		}
		v := regexp.MustCompile(`^(\d+)\.(\d+)\.\d+`).FindStringSubmatch(version)
		if len(v) != 3 {
			return fmt.Errorf("invalid MongoDB server version")
		}
		t.Image = "mongo:" + v[1] + "." + v[2]
	}
	t.docker, err = t.native.LookPath("docker")
	if err != nil {
		return err
	}
	id, err := t.capture(ctx, "image", "inspect", "--format={{.Id}}", t.Image)
	if err != nil {
		return fmt.Errorf("preload the trusted recovery image with docker pull %s: %w", t.Image, err)
	}
	if !strings.HasPrefix(id, "sha256:") || !dockerID.MatchString(strings.TrimPrefix(id, "sha256:")) {
		return fmt.Errorf("invalid Docker image identity")
	}
	t.imageID = id
	return nil
}

func (t *Target) Start(ctx context.Context) error {
	if t.imageID == "" {
		return fmt.Errorf("recovery image was not checked")
	}
	// A random owned name is reserved by create; all later operations use its ID.
	var b limitedOutput
	mount := "/var/lib/mysql"
	env := map[string]string{"MYSQL_ROOT_PASSWORD": t.Password, "MYSQL_DATABASE": t.Config.Database}
	if t.engine == "mongodb" {
		mount = "/data/db"
		env = map[string]string{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": t.Password}
	}
	args := []string{"create", "--pull=never", "--name", "dbvault-recovery-" + t.token, "--label", "io.dbvault.recovery=" + t.token, "--network=none", "--security-opt=no-new-privileges", "--mount", "type=volume,destination=" + mount}
	for k := range env {
		args = append(args, "--env", k)
	}
	args = append(args, t.imageID)
	err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: args, Env: env, Stdout: &b})
	id := strings.TrimSpace(b.String())
	if dockerID.MatchString(id) {
		t.ID = id
	}
	if err != nil {
		// A cancelled CLI may have created the container before losing stdout.
		// Recover only an identity with this run's label and isolation contract.
		if t.ID == "" {
			recoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			id, inspectErr := t.capture(recoverCtx, "inspect", "--format={{.Id}}", "dbvault-recovery-"+t.token)
			if inspectErr == nil && dockerID.MatchString(id) {
				t.ID = id
				if checkErr := t.Check(recoverCtx); checkErr != nil {
					t.ID = ""
				}
			}
		}
		return err
	}
	if t.ID == "" {
		return fmt.Errorf("Docker did not return a valid recovery container ID; inspect dbvault-recovery-%s", t.token)
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	if _, err := t.capture(ctx, "start", t.ID); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastError error
	for {
		// Mongo's entrypoint authenticates a temporary TCP server during init.
		// Never restore into that server: wait until PID 1 is the final daemon.
		process, processErr := t.capture(ready, "exec", t.ID, "cat", "/proc/1/comm")
		expectedProcess := "mongod"
		if t.engine == "mysql" {
			expectedProcess = "mysqld"
		}
		if processErr != nil || process != expectedProcess {
			lastError = fmt.Errorf("recovery server entrypoint has not finished initialization")
		} else if _, err := t.Preflight(ready); err == nil {
			return nil
		} else {
			lastError = err
		}
		select {
		case <-ready.Done():
			return errors.Join(ready.Err(), lastError)
		case <-ticker.C:
		}
	}
}

func (t *Target) Check(ctx context.Context) error {
	if !dockerID.MatchString(t.ID) {
		return fmt.Errorf("recovery container was not created")
	}
	data, err := t.capture(ctx, "inspect", t.ID)
	if err != nil {
		return err
	}
	var entries []struct {
		ID, Image  string
		Config     struct{ Labels map[string]string }
		HostConfig struct {
			NetworkMode  string
			Privileged   bool
			Binds        []string
			VolumesFrom  []string
			PortBindings map[string]json.RawMessage
		}
		Mounts          []struct{ Type, Destination string }
		NetworkSettings struct{ Networks map[string]json.RawMessage }
	}
	if err := json.Unmarshal([]byte(data), &entries); err != nil || len(entries) != 1 {
		return fmt.Errorf("invalid recovery container inspection")
	}
	c := entries[0]
	if c.ID != t.ID || c.Image != t.imageID || c.Config.Labels["io.dbvault.recovery"] != t.token || c.HostConfig.NetworkMode != "none" || c.HostConfig.Privileged || len(c.HostConfig.Binds) != 0 || len(c.HostConfig.VolumesFrom) != 0 || len(c.HostConfig.PortBindings) != 0 {
		return fmt.Errorf("recovery container ownership or isolation changed")
	}
	for network := range c.NetworkSettings.Networks {
		if network != "none" {
			return fmt.Errorf("recovery container acquired an external network")
		}
	}
	for _, mount := range c.Mounts {
		if mount.Type != "volume" || !t.allowedMount(mount.Destination) {
			return fmt.Errorf("unexpected recovery container mount")
		}
	}
	return nil
}

func (t *Target) Cleanup(ctx context.Context) error {
	if err := t.Check(ctx); err != nil {
		return err
	}
	_, err := t.capture(ctx, "rm", "--force", "--volumes", t.ID)
	return err
}

// Stop retained targets so a cancelled Docker exec cannot keep restoring in the
// background. Removal remains an explicit success-only operation.
func (t *Target) Stop(ctx context.Context) error {
	if t.ID == "" {
		return nil
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	_, err := t.capture(ctx, "stop", "--time=5", t.ID)
	return err
}

func (t *Target) allowedMount(path string) bool {
	if t.engine == "mysql" {
		return path == "/var/lib/mysql"
	}
	return path == "/data/db" || path == "/data/configdb"
}

func (t *Target) mongoScript(ctx context.Context, script string) (string, error) {
	if err := t.Check(ctx); err != nil {
		return "", err
	}
	var b limitedOutput
	code := `const a=db.getSiblingDB("admin");if(!a.auth("root",process.env.DBVAULT_RECOVERY_PASSWORD))throw new Error("auth failed");` + script
	err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: []string{"exec", "--env", "DBVAULT_RECOVERY_PASSWORD", t.ID, "mongosh", "--quiet", "--norc", "--host", "127.0.0.1", "--port", "27017", "--eval", code}, Env: map[string]string{"DBVAULT_RECOVERY_PASSWORD": t.Password}, Stdout: &b})
	return strings.TrimSpace(b.String()), security.New(t.Password).Error(err)
}
func (t *Target) mongoVersion(ctx context.Context) (string, error) {
	return t.mongoScript(ctx, `print(a.runCommand({buildInfo:1}).version);`)
}
func (t *Target) ValidateRecovery(ctx context.Context) (database.RecoveryValidation, error) {
	if err := t.Check(ctx); err != nil {
		return database.RecoveryValidation{}, err
	}
	if t.engine == "mongodb" {
		name, _ := json.Marshal(t.Config.Database)
		output, err := t.mongoScript(ctx, `const d=db.getSiblingDB(`+string(name)+`);const cs=d.getCollectionInfos({type:"collection"});for(const c of cs){const v=d.runCommand({validate:c.name,full:true});if(v.ok!==1||v.valid!==true)throw new Error("collection validation failed");d.getCollection(c.name).countDocuments({});}print("DBVAULT_COUNT="+cs.length);`)
		if err != nil {
			return database.RecoveryValidation{}, err
		}
		re := regexp.MustCompile(`DBVAULT_COUNT=(\d+)`)
		v := re.FindStringSubmatch(output)
		if len(v) != 2 {
			return database.RecoveryValidation{}, fmt.Errorf("invalid MongoDB validation output")
		}
		n, e := strconv.ParseInt(v[1], 10, 64)
		return database.RecoveryValidation{Method: MongoValidation, Objects: n}, e
	}
	args := []string{"--no-defaults", "--no-login-paths", "--host=127.0.0.1", "--port=3306", "--user=root", "--protocol=TCP", "--ssl-mode=REQUIRED", "--batch", "--skip-column-names", "--database=" + t.Config.Database}
	var names limitedOutput
	r := &containerRunner{target: t}
	env := map[string]string{"MYSQL_PWD": t.Password, "MYSQL_HISTFILE": ""}
	if err := r.Run(ctx, runner.Spec{Executable: "mysql", Args: append(args, "--execute=SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE'"), Env: env, Stdout: &names}); err != nil {
		return database.RecoveryValidation{}, err
	}
	var count int64
	for _, name := range strings.Split(strings.TrimSpace(names.String()), "\n") {
		if name == "" {
			continue
		}
		quoted := "`" + strings.ReplaceAll(strings.TrimSpace(name), "`", "``") + "`"
		var out limitedOutput
		sql := "CHECK TABLE " + quoted + "; SELECT COUNT(*) FROM " + quoted
		if err := r.Run(ctx, runner.Spec{Executable: "mysql", Args: append(args, "--execute="+sql), Env: env, Stdout: &out}); err != nil {
			return database.RecoveryValidation{}, err
		}
		if !strings.Contains(out.String(), "\tstatus\tOK") {
			return database.RecoveryValidation{}, fmt.Errorf("MySQL table check failed")
		}
		count++
	}
	return database.RecoveryValidation{Method: MySQLValidation, Objects: count}, nil
}

type containerRunner struct{ target *Target }

func (r *containerRunner) LookPath(name string) (string, error) {
	allowed := map[string]bool{"mysql": true, "mysqldump": true}
	if r.target.engine == "mongodb" {
		allowed = map[string]bool{"mongodump": true, "mongorestore": true}
	}
	if !allowed[name] {
		return "", fmt.Errorf("unsupported recovery executable")
	}
	return name, nil
}
func (r *containerRunner) Run(ctx context.Context, s runner.Spec) error {
	t := r.target
	if err := t.Check(ctx); err != nil {
		return err
	}
	if _, err := r.LookPath(s.Executable); err != nil {
		return err
	}
	nativeArgs := append([]string(nil), s.Args...)
	for i, arg := range nativeArgs {
		if strings.HasPrefix(arg, "--config=") {
			source := strings.TrimPrefix(arg, "--config=")
			if !filepath.IsAbs(source) {
				return fmt.Errorf("invalid private credentials path")
			}
			info, err := os.Lstat(source)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("invalid credentials file")
			}
			dest := "/tmp/dbvault-credentials-" + t.token + ".yaml"
			if err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: []string{"cp", source, t.ID + ":" + dest}, Stdout: io.Discard}); err != nil {
				return err
			}
			defer func() {
				c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				t.native.Run(c, runner.Spec{Executable: t.docker, Args: []string{"exec", t.ID, "rm", "-f", dest}, Stdout: io.Discard})
			}()
			nativeArgs[i] = "--config=" + dest
		}
	}
	args := []string{"exec", "--interactive"}
	for k := range s.Env {
		args = append(args, "--env", k)
	}
	args = append(args, t.ID, s.Executable)
	args = append(args, nativeArgs...)
	s.Executable, s.Args = t.docker, args
	return security.New(t.Password).Error(t.native.Run(ctx, s))
}

var _ database.Adapter = (*Target)(nil)
var _ io.Writer = (*limitedOutput)(nil)
