package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/sqlite"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/security"
	"github.com/sung2708/DBVault/internal/storage/local"
)

func recoveryFixture(t *testing.T, compression string) (*Service, metadata.Manifest, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "production.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"CREATE TABLE records(id INTEGER PRIMARY KEY, value TEXT)", "INSERT INTO records VALUES(1,'Việt Nam'),(2,'recovery'),(3,'fixture')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	cfg := config.Defaults()
	cfg.Version = "1"
	cfg.Database = config.Database{Type: "sqlite", Database: path}
	cfg.Compression.Type = compression
	cfg.Health.MaxBackupAge = "12h"
	p, err := local.New(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	s := &Service{Config: cfg, DB: &sqlite.Adapter{Config: cfg.Database}, Store: p, Now: func() time.Time { return healthNow }}
	m, err := s.Backup(context.Background(), "full", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return healthNow.Add(time.Hour) }
	return s, m, dir
}

func TestRecoveryDrillRealSQLiteAllCodecs(t *testing.T) {
	for _, compression := range []string{"none", "gzip", "zstd"} {
		t.Run(compression, func(t *testing.T) {
			s, m, dir := recoveryFixture(t, compression)
			before, err := os.ReadFile(filepath.Join(dir, "backups", m.Name+".meta.json"))
			if err != nil {
				t.Fatal(err)
			}
			db, _ := sql.Open("sqlite", s.Config.Database.Database)
			if _, err := db.Exec("DROP TABLE records; CREATE TABLE production_marker(value); INSERT INTO production_marker VALUES('untouched')"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			target := filepath.Join(dir, "isolated.sqlite")
			r, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true})
			if err != nil || r.Status != "passed" || r.TargetState != "preserved" || r.Validation == nil || r.Validation.Objects != 1 {
				t.Fatalf("%+v %v", r, err)
			}
			db, _ = sql.Open("sqlite", target)
			var values string
			if err := db.QueryRow("SELECT group_concat(value,'|') FROM records ORDER BY id").Scan(&values); err != nil || values != "Việt Nam|recovery|fixture" {
				t.Fatal(values, err)
			}
			db.Close()
			db, _ = sql.Open("sqlite", s.Config.Database.Database)
			if err := db.QueryRow("SELECT value FROM production_marker").Scan(&values); err != nil || values != "untouched" {
				t.Fatal(values, err)
			}
			db.Close()
			after, _ := os.ReadFile(filepath.Join(dir, "backups", m.Name+".meta.json"))
			if string(before) != string(after) {
				t.Fatal("immutable manifest was rewritten")
			}
			reader, err := s.Store.Get(context.Background(), r.RecordKey)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := DecodeDrill(reader)
			reader.Close()
			if err != nil || evidence.Status != "passed" {
				t.Fatal(evidence, err)
			}
			health, err := s.Health(context.Background(), false)
			if err != nil || health.Databases[0].RestoreTest != "passed" || health.Databases[0].Integrity != "unknown" || health.Status != Warning {
				t.Fatal(health, err)
			}
			items, err := s.List(context.Background(), "")
			if err != nil || len(items) != 1 {
				t.Fatal(items, err)
			}
		})
	}
}

func TestRecoveryDryRunAndOwnedCleanup(t *testing.T) {
	s, m, dir := recoveryFixture(t, "gzip")
	target := filepath.Join(dir, "isolated.sqlite")
	r, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, DryRun: true})
	if err != nil || r.Status != "preflight_passed" || r.RecordKey != "" || r.TargetState != "not_created" {
		t.Fatal(r, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("dry run created target", err)
	}
	objects, _ := s.Store.List(context.Background(), "")
	if len(objects) != 2 {
		t.Fatal("dry run persisted drill evidence")
	}
	r, err = s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true, Cleanup: true})
	if err != nil || r.Status != "passed" || r.TargetState != "removed" {
		t.Fatal(r, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("owned successful target not removed", err)
	}
}

func TestRecoveryIsolationGuards(t *testing.T) {
	s, m, dir := recoveryFixture(t, "none")
	before, _ := os.ReadFile(s.Config.Database.Database)
	if runtime.GOOS == "windows" {
		if _, err := isolatedSQLitePath(s.Config.Database.Database, strings.ToUpper(s.Config.Database.Database)); err == nil {
			t.Fatal("Windows case alias accepted")
		}
	}
	for _, target := range []string{s.Config.Database.Database, filepath.Join(dir, "sub", "..", "production.sqlite"), filepath.Join(dir, "backups", m.Name), filepath.Join(dir, "missing", "recovery.sqlite")} {
		if _, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true}); err == nil {
			t.Fatal("accepted unsafe target", target)
		}
	}
	for _, name := range []string{"production.sqlite:stream", "CON", "NUL.sqlite", "recovery.sqlite."} {
		if _, err := isolatedSQLitePath(s.Config.Database.Database, filepath.Join(dir, name)); err == nil {
			t.Fatal("unsafe recovery filename accepted", name)
		}
	}
	hardlink := filepath.Join(dir, "hardlink.sqlite")
	if err := os.Link(s.Config.Database.Database, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := isolatedSQLitePath(s.Config.Database.Database, hardlink); err == nil {
		t.Fatal("hardlink accepted")
	}
	symlink := filepath.Join(dir, "symlink.sqlite")
	if err := os.Symlink(s.Config.Database.Database, symlink); err == nil {
		if _, err := isolatedSQLitePath(s.Config.Database.Database, symlink); err == nil {
			t.Fatal("symlink accepted")
		}
	} else {
		t.Log("symlink test unavailable on this host", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		target := filepath.Join(dir, "sidecar"+suffix+".sqlite")
		if err := os.WriteFile(target+suffix, []byte("owned by someone else"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := isolatedSQLitePath(s.Config.Database.Database, target); err == nil {
			t.Fatal("pre-existing SQLite sidecar accepted")
		}
	}
	after, _ := os.ReadFile(s.Config.Database.Database)
	if string(before) != string(after) {
		t.Fatal("production file changed")
	}
}

func TestRecoveryEvidenceAssociationAndRedaction(t *testing.T) {
	s, m, dir := recoveryFixture(t, "gzip")
	secret := "SECRET_IN_PATH"
	r, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: filepath.Join(dir, secret+".sqlite"), Confirm: true, Redact: security.New(secret).Text})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.Store.Get(context.Background(), r.RecordKey)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || strings.Contains(string(data), secret) {
		t.Fatal("secret in drill record", err)
	}
	s.Now = func() time.Time { return healthNow.Add(2 * time.Hour) }
	target := filepath.Join(dir, "failed-validation.sqlite")
	a := &failingRecovery{Adapter: &sqlite.Adapter{Config: config.Database{Type: "sqlite", Database: target}}, validationErr: errors.New("secret validation failure")}
	if r, err := s.recoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true}, a); err == nil || r.Status != "failed" {
		t.Fatal(r, err)
	}
	health, err := s.Health(context.Background(), false)
	if err != nil || health.Databases[0].RestoreTest != "failed" {
		t.Fatal(health, err)
	}
	s.Now = func() time.Time { return healthNow.Add(3 * time.Hour) }
	if _, err := s.Backup(context.Background(), "full", false); err != nil {
		t.Fatal(err)
	}
	health, err = s.Health(context.Background(), false)
	if err != nil || health.Databases[0].RestoreTest != "unknown" || health.Databases[0].LastRecoveryDrillAt != nil {
		t.Fatal("older backup drill transferred to new backup", health, err)
	}
}

type failingRecovery struct {
	*sqlite.Adapter
	restoreErr, validationErr error
}

func (a *failingRecovery) Restore(ctx context.Context, r io.Reader, o database.RestoreOptions) error {
	if a.restoreErr != nil {
		return a.restoreErr
	}
	return a.Adapter.Restore(ctx, r, o)
}
func (a *failingRecovery) ValidateRecovery(ctx context.Context) (database.RecoveryValidation, error) {
	if a.validationErr != nil {
		return database.RecoveryValidation{}, a.validationErr
	}
	return a.Adapter.ValidateRecovery(ctx)
}

func TestRecoveryFailureStates(t *testing.T) {
	for _, mode := range []string{"corrupt", "missing", "restore", "validation", "cancel", "space", "record"} {
		t.Run(mode, func(t *testing.T) {
			s, m, dir := recoveryFixture(t, "gzip")
			target := filepath.Join(dir, "isolated.sqlite")
			opts := DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true, Cleanup: true}
			a := &failingRecovery{Adapter: &sqlite.Adapter{Config: config.Database{Type: "sqlite", Database: target}}}
			artifact := filepath.Join(dir, "backups", m.Name)
			switch mode {
			case "corrupt":
				if err := os.WriteFile(artifact, []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
			case "restore":
				a.restoreErr = errors.New("restore failed")
			case "validation":
				a.validationErr = errors.New("validation failed")
			case "cancel":
				a.restoreErr = context.Canceled
			case "space":
				opts.CheckSpace = func(metadata.Manifest, string) error { return errors.New("insufficient temp storage") }
			case "record":
				s.Store = failingStore{Provider: s.Store, putFail: true}
			}
			r, err := s.recoveryDrill(context.Background(), opts, a)
			if err == nil || r.Status == "passed" {
				t.Fatal(r, err)
			}
			if mode == "cancel" && (r.Status != "cancelled" || fault.ExitCode(err) != 5) {
				t.Fatal(r, err)
			}
			if mode != "cancel" && r.Status != "failed" {
				t.Fatal("failure incorrectly classified as cancellation", mode, r, err)
			}
			created := mode == "restore" || mode == "validation" || mode == "cancel"
			if created && r.TargetState != "preserved" {
				t.Fatal("failed target not preserved", r)
			}
			if !created && mode != "record" {
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatal("target created before preflight completed", err)
				}
			}
			if mode == "record" && (r.RecordKey != "" || r.TargetState != "removed") {
				t.Fatal(r)
			}
		})
	}
}

func TestRecoveryCleanupOwnership(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owned.sqlite")
	owned, err := createDrillTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.root.Close()
	if err := os.Rename(path, filepath.Join(dir, "old.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unowned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owned.cleanup(); err == nil {
		t.Fatal("deleted replacement target")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "unowned" {
		t.Fatal("replacement changed")
	}
}

func TestRecoveryReplacementBeforeRestoreCannotReachProduction(t *testing.T) {
	s, m, dir := recoveryFixture(t, "gzip")
	target := filepath.Join(dir, "isolated.sqlite")
	before, err := os.ReadFile(s.Config.Database.Database)
	if err != nil {
		t.Fatal(err)
	}
	s.Observe = func(e Event) {
		if e.Stage == "preflight" && e.State == "complete" {
			if err := os.Rename(target, filepath.Join(dir, "owned-original.sqlite")); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(s.Config.Database.Database, target); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: target, Confirm: true, Cleanup: true})
	if err == nil || r.Status != "failed" || r.TargetState != "ownership_changed" {
		t.Fatal(r, err)
	}
	after, err := os.ReadFile(s.Config.Database.Database)
	if err != nil || string(before) != string(after) {
		t.Fatal("production touched after replacement", err)
	}
}

func TestRecoveryUnsupportedAndEvidenceValidation(t *testing.T) {
	for _, engine := range []string{"mysql", "mongodb"} {
		s, _, dir := recoveryFixture(t, "none")
		s.Config.Database.Type = engine
		if _, err := s.RecoveryDrill(context.Background(), DrillOptions{Target: "backup", RecoveryDatabase: filepath.Join(dir, "new.sqlite"), Confirm: true}); err == nil {
			t.Fatal("native engine did not fail closed")
		}
	}
	for _, data := range []string{"{}", `{"version":1,"status":"passed"}`, strings.Repeat("x", (1<<20)+1)} {
		if _, err := DecodeDrill(strings.NewReader(data)); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
}
