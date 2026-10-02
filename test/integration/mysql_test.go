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
	"github.com/sung2708/DBVault/internal/database/mysql"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/security"
	"github.com/sung2708/DBVault/internal/storage/local"
)

func TestMySQLBackupDestroyRestore(t *testing.T) {
	secret := "development-only-db-vault-password"
	native := runner.Native{Redactor: security.New(secret)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if _, err := native.LookPath("docker"); err != nil {
		t.Fatalf("Docker required (NOT RUN): %v", err)
	}
	name := fmt.Sprintf("dbvault-integration-mysql-%d", time.Now().UnixNano())
	if err := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"run", "--detach", "--rm", "--name", name, "--network", "none", "--env", "MYSQL_ROOT_PASSWORD", "--env", "MYSQL_DATABASE", "mysql:8.4"}, Env: map[string]string{"MYSQL_ROOT_PASSWORD": secret, "MYSQL_DATABASE": "dbvault_test"}, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if err := native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"rm", "--force", name}, Stdout: io.Discard}); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	r := containerRunner{native: native, container: name}
	cfg := config.Defaults()
	cfg.Version = "1"
	cfg.Database = config.Database{Type: "mysql", Host: "127.0.0.1", Port: 3306, User: "root", Database: "dbvault_test", SSLMode: "require"}
	adapter := &mysql.Adapter{Config: cfg.Database, Password: secret, Runner: r}
	readyCtx, readyCancel := context.WithTimeout(ctx, 90*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		if _, err := adapter.Preflight(readyCtx); err == nil {
			break
		} else {
			if last == nil {
				t.Logf("Initial native preflight: %v", err)
			}
			if readyCtx.Err() == nil {
				last = err
			}
		}
		select {
		case <-readyCtx.Done():
			t.Fatalf("MySQL readiness deadline: %v", last)
		case <-ticker.C:
		}
	}
	query := func(sql string) string {
		t.Helper()
		var b bytes.Buffer
		err := r.Run(ctx, runner.Spec{Executable: "mysql", Args: []string{"--no-defaults", "--no-login-paths", "--host=127.0.0.1", "--user=root", "--ssl-mode=REQUIRED", "--batch", "--skip-column-names", "--database=dbvault_test", "--execute=" + sql}, Env: map[string]string{"MYSQL_PWD": secret}, Stdout: &b})
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(b.String())
	}
	query("CREATE TABLE items (id integer PRIMARY KEY, name varchar(100) NOT NULL) ENGINE=InnoDB; INSERT INTO items VALUES (1,'known'),(2,'dataset'),(3,'unicode_é')")
	hashQuery := "SELECT MD5(GROUP_CONCAT(CONCAT(id, ':', name) ORDER BY id SEPARATOR ',')) FROM items"
	before := query(hashQuery)
	for _, kind := range []string{"none", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			p, err := local.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			cfg.Compression.Type = kind
			s := &app.Service{Config: cfg, DB: adapter, Store: p, Version: "integration"}
			m, err := s.Backup(ctx, "full", false)
			if err != nil {
				t.Fatal(err)
			}
			assertBackupHealth(t, ctx, s, m)
			if kind == "none" {
				assertNewRestoreWorkflow(t, ctx, s, m)
			}
			query("DROP TABLE items")
			if err = s.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); err != nil {
				t.Fatal(err)
			}
			if got := query(hashQuery); got != before {
				t.Fatalf("dataset differs: %s != %s", got, before)
			}
		})
	}
	t.Run("selected-backup", func(t *testing.T) {
		query("CREATE TABLE excluded(id integer) ENGINE=InnoDB; INSERT INTO excluded VALUES(42)")
		cfg.Database.Options.IncludeTables = []string{"items"}
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
		if query(hashQuery) != before || query("SELECT id FROM excluded") != "42" {
			t.Fatal("selected backup lost data or modified excluded table")
		}
	})
	query("CREATE TABLE unsafe_table(id integer) ENGINE=MyISAM")
	if _, err := adapter.Preflight(ctx); err == nil {
		t.Fatal("nontransactional database was accepted")
	}
}
