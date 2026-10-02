//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/cli"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/postgres"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/security"
	"github.com/sung2708/DBVault/internal/storage/local"
	"gopkg.in/yaml.v3"
)

// containerRunner uses vendor tools inside an isolated container. Secret
// values are inherited by docker through environment variable names only.
type containerRunner struct {
	native        runner.Native
	container     string
	taintHostAddr bool
}

func (r containerRunner) LookPath(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := r.Run(ctx, runner.Spec{Executable: name, Args: []string{"--version"}, Stdout: io.Discard})
	return name, err
}
func (r containerRunner) Run(ctx context.Context, s runner.Spec) error {
	args := []string{"exec", "-i"}
	if r.taintHostAddr {
		args = append(args, "--env", "PGHOSTADDR=198.51.100.23")
	}
	for k := range s.Env {
		args = append(args, "--env", k)
	}
	args = append(args, r.container, s.Executable)
	if len(s.UnsetEnv) > 0 {
		args = append(args[:len(args)-1], "env")
		for _, name := range s.UnsetEnv {
			args = append(args, "-u", name)
		}
		args = append(args, s.Executable)
	}
	args = append(args, s.Args...)
	s.Executable = "docker"
	s.Args = args
	return r.native.Run(ctx, s)
}
func TestPostgresBackupDestroyRestore(t *testing.T) {
	secret := "development-only-db-vault-password"
	native := runner.Native{Redactor: security.New(secret)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := native.LookPath("docker"); err != nil {
		t.Fatalf("integration infrastructure required (NOT RUN): %v", err)
	}
	if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"info"}, Stdout: io.Discard}); err != nil {
		t.Fatalf("Docker daemon unavailable (NOT RUN): %v", err)
	}
	name := fmt.Sprintf("dbvault-integration-pg-%d", time.Now().UnixNano())
	if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"run", "--detach", "--rm", "--name", name, "--network", "none", "--env", "POSTGRES_PASSWORD", "--env", "POSTGRES_DB", "postgres:16-alpine"}, Env: map[string]string{"POSTGRES_PASSWORD": secret, "POSTGRES_DB": "dbvault_test"}, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if err := native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"rm", "--force", name}, Stdout: io.Discard}); err != nil {
			t.Errorf("container cleanup: %v", err)
		}
	}()
	r := containerRunner{native: native, container: name, taintHostAddr: true}
	cfg := config.Defaults()
	cfg.Version = "1"
	cfg.Database = config.Database{Type: "postgres", Host: "127.0.0.1", Port: 5432, User: "postgres", Database: "dbvault_test", SSLMode: "disable"}
	adapter := &postgres.Adapter{Config: cfg.Database, Password: secret, Runner: r}
	readyCtx, readyCancel := context.WithTimeout(ctx, 60*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastReadinessErr error
	for {
		if _, err := adapter.Preflight(readyCtx); err == nil {
			break
		} else {
			if readyCtx.Err() == nil {
				lastReadinessErr = err
			}
		}
		select {
		case <-readyCtx.Done():
			t.Fatalf("PostgreSQL readiness deadline expired: %v", lastReadinessErr)
		case <-ticker.C:
		}
	}
	query := func(sql string) string {
		t.Helper()
		var b bytes.Buffer
		err := r.Run(ctx, runner.Spec{Executable: "psql", Args: []string{"--no-psqlrc", "--no-password", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=" + sql}, Env: map[string]string{"PGPASSWORD": secret, "PGDATABASE": "dbvault_test", "PGUSER": "postgres"}, UnsetEnv: []string{"PGHOSTADDR"}, Stdout: &b})
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(b.String())
	}
	query("CREATE TABLE items (id integer PRIMARY KEY, name text NOT NULL); INSERT INTO items SELECT n, 'fixture_' || n FROM generate_series(1,1000) AS n")
	query("CREATE MATERIALIZED VIEW empty_view AS SELECT 42 AS value WITH NO DATA")
	if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"image", "inspect", "postgres:16-bookworm"}, Stdout: io.Discard}); err != nil {
		if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"pull", "postgres:16-bookworm"}, Stdout: io.Discard}); err != nil {
			t.Fatal(err)
		}
	}
	before := query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items")
	for _, kind := range []string{"none", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			backupDir := t.TempDir()
			store, err := local.New(backupDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cfg.Compression.Type = kind
			s := &app.Service{Config: cfg, DB: adapter, Store: store, Version: "integration"}
			m, err := s.Backup(ctx, "full", false)
			if err != nil {
				t.Fatal(err)
			}
			assertBackupHealth(t, ctx, s, m)
			dry, err := s.RecoveryDrill(ctx, app.DrillOptions{Target: m.Name, RecoveryDatabase: "dbvault_recovery", DryRun: true})
			if err != nil || dry.Status != "preflight_passed" || dry.RecoveryTarget != "" || dry.RecordKey != "" {
				t.Fatal(dry, err)
			}
			drill, err := s.RecoveryDrill(ctx, app.DrillOptions{Target: m.Name, RecoveryDatabase: "dbvault_recovery", Confirm: true})
			if err != nil || drill.Status != "passed" || drill.Validation == nil || drill.Validation.Objects != 2 {
				t.Fatal(drill, err)
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"rm", "--force", "--volumes", drill.RecoveryTarget}, Stdout: io.Discard}); err != nil {
					t.Error(err)
				}
			}()
			drillRunner := containerRunner{native: native, container: drill.RecoveryTarget}
			if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"start", drill.RecoveryTarget}, Stdout: io.Discard}); err != nil {
				t.Fatal(err)
			}
			// Retained targets are stopped by the drill. Wait for PostgreSQL after
			// restarting this exact container solely to compare the fixture dataset.
			for {
				if err := drillRunner.Run(ctx, runner.Spec{Executable: "pg_isready", Args: []string{"--quiet"}, Stdout: io.Discard}); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(250 * time.Millisecond):
				}
			}
			var restored bytes.Buffer
			if err := drillRunner.Run(ctx, runner.Spec{Executable: "psql", Args: []string{"--no-psqlrc", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items"}, Env: map[string]string{"PGDATABASE": "dbvault_recovery", "PGUSER": "postgres"}, Stdout: &restored}); err != nil || strings.TrimSpace(restored.String()) != before {
				t.Fatal("drill dataset mismatch", err)
			}
			if query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items") != before {
				t.Fatal("drill changed production")
			}
			evidence, err := s.Store.Get(ctx, drill.RecordKey)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := app.DecodeDrill(evidence)
			evidence.Close()
			if err != nil || decoded.Engine != "postgres" || decoded.Status != "passed" {
				t.Fatal(decoded, err)
			}
			health, err := s.Health(ctx, false)
			if err != nil || health.Databases[0].RestoreTest != "passed" {
				t.Fatal(health, err)
			}
			cliConfig := cfg
			cliConfig.Database.Host = "unreachable.invalid" // Drill must not contact production.
			cliConfig.Database.PasswordEnv = "DBVAULT_UNUSED_PRODUCTION_PASSWORD"
			cliConfig.Storage.Type = "local"
			cliConfig.Storage.Local.Path = backupDir
			configData, err := yaml.Marshal(cliConfig)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "drill.yaml")
			if err := os.WriteFile(configPath, configData, 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			root := cli.New(cli.Build{}, &stdout, &stderr)
			root.SetContext(ctx)
			root.SetArgs([]string{"recovery", "drill", "--config", configPath, "--target", m.Name, "--recovery-database", "cleanup_recovery", "--confirm", "--cleanup", "--output", "json"})
			if err := root.Execute(); err != nil {
				t.Fatal(err, stderr.String())
			}
			var removed app.DrillResult
			if err := json.Unmarshal(stdout.Bytes(), &removed); err != nil || stderr.Len() != 0 || removed.Status != "passed" || removed.TargetState != "removed" {
				t.Fatal(removed, err, stdout.String(), stderr.String())
			}
			if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"inspect", removed.RecoveryTarget}, Stdout: io.Discard}); err == nil {
				t.Fatal("recovery cleanup left container")
			}
			query("DROP MATERIALIZED VIEW empty_view; DROP TABLE items")
			if err = s.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); err != nil {
				t.Fatal(err)
			}
			if got := query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items"); got != before {
				t.Fatalf("dataset differs after restore: %s != %s", got, before)
			}
			query("UPDATE items SET name='mutated'")
			if err = s.Restore(ctx, m.Name, true, false, database.RestoreOptions{Clean: true, Tables: []string{"items"}}); err != nil {
				t.Fatal(err)
			}
			if got := query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items"); got != before {
				t.Fatal("selective clean restore lost data")
			}
		})
	}
	t.Run("selected-backup", func(t *testing.T) {
		query("CREATE TABLE excluded(id integer); INSERT INTO excluded VALUES(42)")
		cfg.Database.Options.IncludeTables = []string{"public.items"}
		adapter.Config = cfg.Database
		cfg.Compression.Type = "gzip"
		p, e := local.New(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		s := &app.Service{Config: cfg, DB: adapter, Store: p, Version: "integration"}
		m, e := s.Backup(ctx, "full", false)
		if e != nil {
			t.Fatal(e)
		}
		query("DROP TABLE items")
		if e = s.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); e != nil {
			t.Fatal(e)
		}
		if got := query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items"); got != before {
			t.Fatal("selected backup lost data")
		}
		if query("SELECT id FROM excluded") != "42" {
			t.Fatal("excluded table changed")
		}
	})
}
