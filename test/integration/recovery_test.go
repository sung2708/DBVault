//go:build integration

package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database/sqlite"
	"github.com/sung2708/DBVault/internal/storage/local"
)

// SQLite is embedded: this is a real database drill and requires no Docker.
func TestSQLiteRecoveryDrill(t *testing.T) {
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "production.sqlite")
			target := filepath.Join(dir, "isolated.sqlite")
			db, err := sql.Open("sqlite", source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("CREATE TABLE records(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO records VALUES(1,'one'),(2,'two'),(3,'three')"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			cfg := config.Defaults()
			cfg.Version = "1"
			cfg.Database = config.Database{Type: "sqlite", Database: source}
			cfg.Compression.Type = codec
			p, err := local.New(filepath.Join(dir, "backups"))
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			s := &app.Service{Config: cfg, DB: &sqlite.Adapter{Config: cfg.Database}, Store: p}
			m, err := s.Backup(context.Background(), "full", false)
			if err != nil {
				t.Fatal(err)
			}
			assertBackupHealth(t, context.Background(), s, m)
			db, _ = sql.Open("sqlite", source)
			if _, err := db.Exec("UPDATE records SET value='production-only'"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			r, err := s.RecoveryDrill(context.Background(), app.DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true})
			if err != nil || r.Status != "passed" {
				t.Fatal(r, err)
			}
			db, _ = sql.Open("sqlite", target)
			var values string
			if err := db.QueryRow("SELECT group_concat(value,'|') FROM records ORDER BY id").Scan(&values); err != nil || values != "one|two|three" {
				t.Fatal(values, err)
			}
			db.Close()
			db, _ = sql.Open("sqlite", source)
			if err := db.QueryRow("SELECT value FROM records WHERE id=1").Scan(&values); err != nil || values != "production-only" {
				t.Fatal(values, err)
			}
			db.Close()
			if _, err := s.RecoveryDrill(context.Background(), app.DrillOptions{Target: m.Name, RecoveryDatabase: source, Confirm: true}); err == nil {
				t.Fatal("production target accepted")
			}
			cleanupTarget := filepath.Join(dir, "cleanup.sqlite")
			if r, err := s.RecoveryDrill(context.Background(), app.DrillOptions{Target: m.Name, RecoveryDatabase: cleanupTarget, Confirm: true, Cleanup: true}); err != nil || r.Status != "passed" || r.TargetState != "removed" {
				t.Fatal(r, err)
			}
			if _, err := os.Stat(cleanupTarget); !os.IsNotExist(err) {
				t.Fatal("cleanup failed", err)
			}
		})
	}
}
