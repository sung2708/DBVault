package mysql

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/fault"
)

type Adapter struct {
	Config   config.Database
	Password string
	Runner   runner.Runner
}

func (*Adapter) Name() string      { return "mysql" }
func (*Adapter) Format() string    { return "sql" }
func (*Adapter) Extension() string { return ".sql" }
func (*Adapter) Capabilities() database.Capabilities {
	return database.Capabilities{ConnectionTest: true, FullBackup: true, FullRestore: true, SelectiveBackup: true, StreamingBackup: true}
}
func (a *Adapter) env() map[string]string {
	return map[string]string{"MYSQL_PWD": a.Password, "MYSQL_HISTFILE": ""}
}
func (a *Adapter) args() []string {
	tls := map[string]string{"disable": "DISABLED", "prefer": "PREFERRED", "require": "REQUIRED", "verify-ca": "VERIFY_CA", "verify-full": "VERIFY_IDENTITY"}[a.Config.SSLMode]
	return []string{"--no-defaults", "--no-login-paths", "--host=" + a.Config.Host, "--port=" + strconv.Itoa(a.Config.Port), "--user=" + a.Config.User, "--protocol=TCP", "--ssl-mode=" + tls}
}

type bounded struct{ data []byte }

func (b *bounded) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 64<<10 {
		return 0, fmt.Errorf("native diagnostics too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (a *Adapter) capture(ctx context.Context, tool string, args []string) (string, error) {
	b := &bounded{}
	err := a.Runner.Run(ctx, runner.Spec{Executable: tool, Args: args, Env: a.env(), Stdout: b})
	return strings.TrimSpace(string(b.data)), err
}

var version = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)`)

func Series(v string) (string, error) {
	if strings.Contains(strings.ToLower(v), "mariadb") {
		return "", fmt.Errorf("MariaDB requires a separate strategy")
	}
	parts := version.FindStringSubmatch(v)
	if len(parts) != 4 || parts[1] != "8" {
		return "", fmt.Errorf("requires Oracle MySQL 8.x")
	}
	return parts[1] + "." + parts[2], nil
}
func (a *Adapter) Preflight(ctx context.Context) (database.Info, error) {
	info := database.Info{}
	for _, tool := range []string{"mysqldump", "mysql"} {
		if _, err := a.Runner.LookPath(tool); err != nil {
			return info, err
		}
		v, err := a.capture(ctx, tool, []string{"--no-defaults", "--no-login-paths", "--version"})
		if err != nil {
			return info, fault.Wrap(fault.Dependency, "discover "+tool, err)
		}
		if tool == "mysqldump" {
			info.ToolVersion = v
		} else {
			info.RestoreToolVersion = v
		}
	}
	args := append(a.args(), "--connect-timeout=10", "--batch", "--skip-column-names", "--database="+a.Config.Database, "--execute=SELECT VERSION()")
	v, err := a.capture(ctx, "mysql", args)
	if err != nil {
		return info, fault.Wrap(fault.Connection, "MySQL connection test", err)
	}
	info.ServerVersion = v
	server, err := Series(v)
	if err != nil {
		return info, fault.Wrap(fault.Unsupported, "MySQL server", err)
	}
	dump, err := Series(info.ToolVersion)
	if err != nil || dump != server {
		return info, fault.Wrap(fault.Unsupported, "MySQL client compatibility", fmt.Errorf("mysqldump must match the server 8.x release series"))
	}
	// --single-transaction only guarantees snapshots of transactional tables.
	args = append(a.args(), "--connect-timeout=10", "--batch", "--skip-column-names", "--database="+a.Config.Database, "--execute=SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE' AND engine <> 'InnoDB'")
	count, err := a.capture(ctx, "mysql", args)
	if err != nil {
		return info, fault.Wrap(fault.Connection, "validate MySQL table engines", err)
	}
	if count != "0" {
		return info, fault.Wrap(fault.Unsupported, "consistent MySQL strategy", fmt.Errorf("all base tables must use InnoDB; nontransactional tables are unsupported"))
	}
	return info, nil
}
func (a *Adapter) Dump(ctx context.Context, w io.Writer) error {
	args := append(a.args(), "--single-transaction", "--quick", "--routines", "--triggers", "--events", "--no-tablespaces", "--set-gtid-purged=OFF", "--hex-blob")
	for _, table := range a.Config.Options.ExcludeTables {
		args = append(args, "--ignore-table="+a.Config.Database+"."+table)
	}
	args = append(args, "--", a.Config.Database)
	for _, table := range a.Config.Options.IncludeTables {
		if strings.HasPrefix(table, "-") || strings.ContainsAny(table, "\x00\r\n") {
			return fault.Wrap(fault.Unsupported, "MySQL table selector", fmt.Errorf("option-like table names are unsupported"))
		}
		args = append(args, table)
	}
	return a.Runner.Run(ctx, runner.Spec{Executable: "mysqldump", Args: args, Env: a.env(), Stdout: w})
}
func (a *Adapter) Restore(ctx context.Context, r io.Reader, o database.RestoreOptions) error {
	if o.Clean || len(o.Tables) > 0 || len(o.Schemas) > 0 {
		return fault.Wrap(fault.Unsupported, "MySQL restore selectors/clean", fmt.Errorf("restore the entire SQL artifact; selective restore and --clean are unsupported"))
	}
	args := append(a.args(), "--connect-timeout=10", "--binary-mode", "--batch", "--database="+a.Config.Database)
	return a.Runner.Run(ctx, runner.Spec{Executable: "mysql", Args: args, Env: a.env(), Stdin: r, Stdout: io.Discard})
}
func (a *Adapter) Compatible(target database.Info, sourceServer, sourceTool string) error {
	src, err := Series(sourceServer)
	if err != nil {
		return err
	}
	dst, err := Series(target.ServerVersion)
	if err != nil || dst != src {
		return fmt.Errorf("cross-series MySQL restores are unsupported")
	}
	tool, err := Series(target.RestoreToolVersion)
	if err != nil || tool != src {
		return fmt.Errorf("mysql restore client must match source release series")
	}
	_, err = Series(sourceTool)
	return err
}
