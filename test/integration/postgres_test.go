//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/postgres"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/security"
	"github.com/sung2708/DBVault/internal/storage/local"
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
	before := query("SELECT md5(string_agg(id || ':' || name, ',' ORDER BY id)) FROM items")
	for _, kind := range []string{"none", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			store, err := local.New(t.TempDir())
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
			query("DROP TABLE items")
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
