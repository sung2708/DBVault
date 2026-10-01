package postgres

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/toolresolve"
)

type Adapter struct {
	Config   config.Database
	Password string
	Runner   runner.Runner
}

func (*Adapter) Name() string      { return "postgres" }
func (*Adapter) Format() string    { return "custom" }
func (*Adapter) Extension() string { return ".dump" }
func (*Adapter) Capabilities() database.Capabilities {
	return database.Capabilities{ConnectionTest: true, FullBackup: true, FullRestore: true, SelectiveBackup: true, SelectiveRestore: true, StreamingBackup: true}
}
func (a *Adapter) env() map[string]string {
	return map[string]string{"PGHOST": a.Config.Host, "PGPORT": strconv.Itoa(a.Config.Port), "PGUSER": a.Config.User, "PGDATABASE": a.Config.Database, "PGPASSWORD": a.Password, "PGSSLMODE": a.Config.SSLMode, "PGCONNECT_TIMEOUT": "10", "PGOPTIONS": ""}
}

type bounded struct{ data []byte }

func (b *bounded) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 64<<10 {
		return 0, fmt.Errorf("native diagnostic output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (a *Adapter) capture(ctx context.Context, tool string, args ...string) (string, error) {
	path, err := toolresolve.Resolve(tool, a.Config.Tools[tool], a.Runner)
	if err != nil {
		return "", err
	}
	b := &bounded{}
	err = a.Runner.Run(ctx, runner.Spec{Executable: path, Args: args, Env: a.env(), UnsetEnv: []string{"PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR"}, Stdout: b})
	return strings.TrimSpace(string(b.data)), err
}
func Major(tool string) (int, error) {
	fields := strings.Fields(tool)
	for _, f := range fields {
		if len(f) > 0 && f[0] >= '0' && f[0] <= '9' {
			v, err := strconv.Atoi(strings.Split(f, ".")[0])
			if err == nil {
				return v, nil
			}
		}
	}
	return 0, fmt.Errorf("unrecognized PostgreSQL tool version")
}
func ValidateToolVersions(dump, restore, psql string) error {
	d, e := Major(dump)
	if e != nil {
		return fmt.Errorf("invalid pg_dump version: %w", e)
	}
	r, e := Major(restore)
	if e != nil {
		return fmt.Errorf("invalid pg_restore version: %w", e)
	}
	p, e := Major(psql)
	if e != nil {
		return fmt.Errorf("invalid psql version: %w", e)
	}
	if p != d || r < d {
		return fmt.Errorf("pg_dump and psql majors must match, and pg_restore must be at least as new as pg_dump")
	}
	return nil
}
func (a *Adapter) Preflight(ctx context.Context) (database.Info, error) {
	info := database.Info{}
	psqlToolVersion := ""
	paths := make([]string, 0, 3)
	for _, tool := range []string{"pg_dump", "pg_restore", "psql"} {
		path, err := toolresolve.Resolve(tool, a.Config.Tools[tool], a.Runner)
		if err != nil {
			return info, err
		}
		paths = append(paths, path)
		v, err := a.capture(ctx, tool, "--version")
		if err != nil {
			return info, fault.Wrap(fault.Dependency, "discover "+tool+" version", err)
		}
		if tool == "pg_dump" {
			info.ToolVersion = v
		}
		if tool == "pg_restore" {
			info.RestoreToolVersion = v
		}
		if tool == "psql" {
			psqlToolVersion = v
		}
	}
	if err := ValidateToolVersions(info.ToolVersion, info.RestoreToolVersion, psqlToolVersion); err != nil {
		return info, fault.Wrap(fault.Unsupported, "PostgreSQL tool compatibility", err)
	}
	if err := toolresolve.SameToolchain(paths); err != nil {
		return info, fault.Wrap(fault.Dependency, "PostgreSQL toolchain", err)
	}
	version, err := a.capture(ctx, "psql", "--no-psqlrc", "--no-password", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=SHOW server_version_num")
	if err != nil {
		return info, fault.Wrap(fault.Connection, "PostgreSQL connection test", err)
	}
	n, err := strconv.Atoi(version)
	if err != nil || n < 100000 {
		return info, fault.Wrap(fault.Unsupported, "PostgreSQL version", fmt.Errorf("requires PostgreSQL 10 or later"))
	}
	info.ServerVersion = version
	major, err := Major(info.ToolVersion)
	if err != nil {
		return info, fault.Wrap(fault.Dependency, "parse pg_dump version", err)
	}
	if major != n/10000 {
		return info, fault.Wrap(fault.Unsupported, "PostgreSQL version compatibility", fmt.Errorf("use pg_dump matching server major version %d", n/10000))
	}
	return info, nil
}
func (a *Adapter) Dump(ctx context.Context, w io.Writer) error {
	args := []string{"--no-password", "--format=custom", "--compress=0", "--no-owner", "--no-privileges"}
	for _, t := range a.Config.Options.IncludeTables {
		args = append(args, "--table="+t)
	}
	for _, t := range a.Config.Options.ExcludeTables {
		args = append(args, "--exclude-table="+t)
	}
	p, err := toolresolve.Resolve("pg_dump", a.Config.Tools["pg_dump"], a.Runner)
	if err != nil {
		return err
	}
	return a.Runner.Run(ctx, runner.Spec{Executable: p, Args: args, Env: a.env(), UnsetEnv: []string{"PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR"}, Stdout: w})
}
func (a *Adapter) Restore(ctx context.Context, r io.Reader, o database.RestoreOptions) error {
	if len(o.Collections) > 0 {
		return fault.Wrap(fault.Unsupported, "PostgreSQL restore", fmt.Errorf("collection filters are unsupported"))
	}
	args := []string{"--no-password", "--dbname=" + a.Config.Database, "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges"}
	if o.Clean {
		args = append(args, "--clean", "--if-exists")
	}
	for _, t := range o.Tables {
		args = append(args, "--table="+t)
	}
	for _, s := range o.Schemas {
		args = append(args, "--schema="+s)
	}
	p, err := toolresolve.Resolve("pg_restore", a.Config.Tools["pg_restore"], a.Runner)
	if err != nil {
		return err
	}
	return a.Runner.Run(ctx, runner.Spec{Executable: p, Args: args, Env: a.env(), UnsetEnv: []string{"PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR"}, Stdin: r, Stdout: io.Discard})
}
func (a *Adapter) Compatible(target database.Info, sourceServer, sourceTool string) error {
	src, err := strconv.Atoi(sourceServer)
	if err != nil {
		return fmt.Errorf("invalid source server version")
	}
	dst, err := strconv.Atoi(target.ServerVersion)
	if err != nil || dst/10000 < src/10000 {
		return fmt.Errorf("restoring to an older PostgreSQL major version is unsupported")
	}
	restoreMajor, err := Major(target.RestoreToolVersion)
	if err != nil {
		return err
	}
	dumpMajor, err := Major(sourceTool)
	if err != nil {
		return err
	}
	if restoreMajor < dumpMajor {
		return fmt.Errorf("pg_restore must be at least the source pg_dump major version")
	}
	return nil
}
