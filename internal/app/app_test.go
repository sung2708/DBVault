package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/storage"
	"github.com/sung2708/DBVault/internal/storage/local"
)

type fakeDB struct {
	data                []byte
	dumpErr, restoreErr error
	restoreCalled       bool
	cancelDump          bool
}

func (*fakeDB) Name() string      { return "postgres" }
func (*fakeDB) Format() string    { return "custom" }
func (*fakeDB) Extension() string { return ".dump" }
func (*fakeDB) Capabilities() database.Capabilities {
	return database.Capabilities{FullBackup: true, FullRestore: true, SelectiveRestore: true}
}
func (*fakeDB) Preflight(context.Context) (database.Info, error) {
	return database.Info{ServerVersion: "160004", ToolVersion: "pg_dump 16.4"}, nil
}
func (*fakeDB) Compatible(database.Info, string, string) error { return nil }
func (f *fakeDB) Dump(ctx context.Context, w io.Writer) error {
	if f.cancelDump {
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err := w.Write(f.data); err != nil {
		return err
	}
	return f.dumpErr
}
func (f *fakeDB) Restore(_ context.Context, r io.Reader, _ database.RestoreOptions) error {
	f.restoreCalled = true
	if f.restoreErr != nil {
		return f.restoreErr
	}
	b, err := io.ReadAll(r)
	if err == nil {
		f.data = b
	}
	return err
}
func setup(t *testing.T, kind string) (*Service, *fakeDB, string) {
	t.Helper()
	dir := t.TempDir()
	p, err := local.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	db := &fakeDB{data: bytes.Repeat([]byte("known fixture\n"), 1024)}
	cfg := config.Defaults()
	cfg.Version = "1"
	cfg.Database.Database = "fixture"
	cfg.Compression.Type = kind
	return &Service{Config: cfg, DB: db, Store: p, Version: "dev"}, db, dir
}
func TestRoundTrip(t *testing.T) {
	for _, kind := range []string{"none", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _ := setup(t, kind)
			before := bytes.Clone(db.data)
			m, err := s.Backup(context.Background(), "full", false)
			if err != nil {
				t.Fatal(err)
			}
			db.data = nil
			if err = s.Restore(context.Background(), m.Name, true, false, database.RestoreOptions{}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(db.data, before) {
				t.Fatal("restore data differs")
			}
			items, err := s.List(context.Background(), "")
			if err != nil || len(items) != 1 {
				t.Fatal(items, err)
			}
			if err = s.Delete(context.Background(), m.Name, true, false); err != nil {
				t.Fatal(err)
			}
			objects, _ := s.Store.List(context.Background(), "")
			if len(objects) != 0 {
				t.Fatal("delete left registered resources")
			}
		})
	}
}
func TestCorruptionAbortsBeforeDatabase(t *testing.T) {
	s, db, dir := setup(t, "gzip")
	m, err := s.Backup(context.Background(), "full", false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, m.Name), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte("corrupt"), 0)
	f.Close()
	err = s.Restore(context.Background(), m.Name, true, false, database.RestoreOptions{})
	if err == nil || fault.ExitCode(err) != 4 || db.restoreCalled {
		t.Fatal("corrupted backup reached DB", err)
	}
	if _, err = s.Verify(context.Background(), m.Name); err == nil {
		t.Fatal("corruption missed")
	}
}

type failingStore struct {
	storage.Provider
	manifestFail, putFail bool
}

func (f failingStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if f.putFail || (f.manifestFail && strings.HasSuffix(key, ".meta.json")) {
		return errors.New("injected storage failure")
	}
	return f.Provider.Put(ctx, key, r, size)
}
func TestFailuresCleanResources(t *testing.T) {
	for _, mode := range []string{"dump", "empty", "manifest", "storage", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, db, _ := setup(t, "gzip")
			ctx := context.Background()
			switch mode {
			case "dump":
				db.dumpErr = errors.New("native dump failed")
			case "empty":
				db.data = nil
			case "manifest":
				s.Store = failingStore{Provider: s.Store, manifestFail: true}
			case "storage":
				s.Store = failingStore{Provider: s.Store, putFail: true}
			case "cancel":
				db.cancelDump = true
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			if _, err := s.Backup(ctx, "full", false); err == nil {
				t.Fatal("failure not propagated")
			}
			items, err := s.Store.List(context.Background(), "")
			if err != nil || len(items) != 0 {
				t.Fatal("left incomplete resources", items, err)
			}
		})
	}
}
func TestGuardsAndDryRun(t *testing.T) {
	s, db, _ := setup(t, "none")
	if _, err := s.Backup(context.Background(), "differential", false); err == nil {
		t.Fatal("fake differential accepted")
	}
	if _, err := s.Backup(context.Background(), "full", true); err != nil {
		t.Fatal(err)
	}
	objects, _ := s.Store.List(context.Background(), "")
	if len(objects) != 0 {
		t.Fatal("dry run wrote")
	}
	if err := s.Restore(context.Background(), "missing", false, false, database.RestoreOptions{}); err == nil || db.restoreCalled {
		t.Fatal("guard bypassed")
	}
}
func TestRestoreEarlyFailureDoesNotHang(t *testing.T) {
	s, db, _ := setup(t, "zstd")
	m, err := s.Backup(context.Background(), "full", false)
	if err != nil {
		t.Fatal(err)
	}
	db.restoreErr = errors.New("restore failed immediately")
	if err = s.Restore(context.Background(), m.Name, true, false, database.RestoreOptions{}); err == nil {
		t.Fatal("failure swallowed")
	}
}
func TestLogsAndMetadataHaveNoSecrets(t *testing.T) {
	s, _, _ := setup(t, "none")
	var logs bytes.Buffer
	s.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	m, err := s.Backup(context.Background(), "full", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := metadata.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SUPER_SECRET_DB_PASSWORD_12345", "SUPER_SECRET_WEBHOOK_12345"} {
		if strings.Contains(logs.String(), secret) || bytes.Contains(data, []byte(secret)) {
			t.Fatal("secret in log/metadata")
		}
	}
}
