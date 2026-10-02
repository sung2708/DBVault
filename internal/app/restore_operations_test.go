package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/storage"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (f *fakeDB) ForFullBackup() database.Adapter { return f }

type newFake struct {
	*fakeDB
	created   bool
	exists    bool
	createErr error
}

func (f *newFake) PreflightNew(ctx context.Context) (database.Info, error) {
	if f.exists {
		return database.Info{}, errors.New("already exists")
	}
	return f.fakeDB.Preflight(ctx)
}
func (f *newFake) CreateNew(ctx context.Context) error {
	if f.exists {
		return errors.New("exists")
	}
	if f.createErr != nil {
		return f.createErr
	}
	f.created = true
	f.exists = true
	return nil
}

type failHistoryStore struct{ storage.Provider }

func (s failHistoryStore) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	if strings.HasPrefix(key, "restore_history_") {
		return errors.New("history unavailable")
	}
	return s.Provider.Put(ctx, key, r, n)
}

func TestRestoreOperationsSafetyAndEvidence(t *testing.T) {
	for _, scenario := range []string{"success", "safety_failure", "restore_failure", "history_failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, _ := setup(t, "gzip")
			s.Config.Database.Type = "postgres"
			m, e := s.Backup(context.Background(), "full", false)
			if e != nil {
				t.Fatal(e)
			}
			before := []byte("destination before restore")
			db.data = bytes.Clone(before)
			if scenario == "safety_failure" {
				db.dumpErr = errors.New("cannot backup")
			}
			if scenario == "restore_failure" {
				db.restoreErr = errors.New("native failed")
			}
			if scenario == "cancelled" {
				db.restoreErr = context.Canceled
			}
			if scenario == "history_failure" {
				s.Store = failHistoryStore{s.Store}
			}
			result, e := s.RestoreWithResult(context.Background(), m.Name, true, false, RestoreRequest{BackupBefore: true})
			if scenario == "success" && e != nil {
				t.Fatal(e)
			}
			if scenario != "success" && e == nil {
				t.Fatal("missing failure", result)
			}
			if scenario == "safety_failure" {
				if db.restoreCalled || !bytes.Equal(db.data, before) {
					t.Fatal("restore wrote after safety backup failure")
				}
			} else {
				if result.SafetyBackup == "" {
					t.Fatal("missing safety artifact")
				}
				export, e := s.Export(context.Background(), result.SafetyBackup, filepath.Join(t.TempDir(), "safety.dump"), true)
				if e != nil {
					t.Fatal(e)
				}
				saved, _ := os.ReadFile(export.File)
				if !bytes.Equal(saved, before) {
					t.Fatal("safety artifact is not destination data")
				}
			}
			if scenario == "history_failure" {
				if result.Status != "completed" || result.EvidenceStatus != "failed" {
					t.Fatal(result)
				}
				return
			}
			history, e := s.RestoreHistory(context.Background())
			if e != nil || len(history) != 1 || history[0].Backup.ID != m.ID || history[0].Database != "fixture" {
				t.Fatal(history, e)
			}
			if scenario == "success" && history[0].Status != "completed" {
				t.Fatal(history)
			}
			if scenario == "cancelled" && history[0].Status != "cancelled" {
				t.Fatal(history)
			}
		})
	}
}

func TestNewRestoreNeverCreatesOnDryRunCorruptionOrExisting(t *testing.T) {
	for _, scenario := range []string{"dry", "corrupt", "exists", "success"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, dir := setup(t, "gzip")
			s.Config.Database.Type = "postgres"
			m, e := s.Backup(context.Background(), "full", false)
			if e != nil {
				t.Fatal(e)
			}
			f := &newFake{fakeDB: db, exists: scenario == "exists"}
			s.DB = f
			s.Config.Database.Database = "new_destination"
			if scenario == "corrupt" {
				if e = os.WriteFile(filepath.Join(dir, m.Name), []byte("bad"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			result, e := s.RestoreWithResult(context.Background(), m.Name, true, scenario == "dry", RestoreRequest{NewDatabase: true})
			if scenario == "success" {
				if e != nil || !f.created || !f.restoreCalled {
					t.Fatal(result, e)
				}
			} else if f.created || f.restoreCalled {
				t.Fatal("unexpected writes", scenario)
			}
			if (scenario == "corrupt" || scenario == "exists") && e == nil {
				t.Fatal("missing failure")
			}
			if scenario == "dry" {
				history, e := s.RestoreHistory(context.Background())
				if e != nil || len(history) != 0 {
					t.Fatal(history, e)
				}
			}
		})
	}
}
func TestExportVerifiedAndExclusive(t *testing.T) {
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			s, db, dir := setup(t, codec)
			want := bytes.Clone(db.data)
			m, e := s.Backup(context.Background(), "full", false)
			if e != nil {
				t.Fatal(e)
			}
			for _, decompress := range []bool{false, true} {
				path := filepath.Join(t.TempDir(), "export")
				result, e := s.Export(context.Background(), m.Name, path, decompress)
				if e != nil {
					t.Fatal(e)
				}
				got, _ := os.ReadFile(path)
				if decompress && !bytes.Equal(got, want) {
					t.Fatal("native bytes differ")
				}
				if !decompress {
					stored, _ := os.ReadFile(filepath.Join(dir, m.Name))
					if !bytes.Equal(got, stored) {
						t.Fatal("stored bytes differ")
					}
				}
				if result.Bytes != int64(len(got)) {
					t.Fatal(result)
				}
				if _, e = s.Export(context.Background(), m.Name, path, decompress); e == nil {
					t.Fatal("overwrote existing file")
				}
			}
			os.WriteFile(filepath.Join(dir, m.Name), []byte("bad"), 0600)
			path := filepath.Join(t.TempDir(), "bad")
			if _, e = s.Export(context.Background(), m.Name, path, true); e == nil {
				t.Fatal("exported corruption")
			}
			if _, e = os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("corrupt export created destination")
			}
		})
	}
}
func TestGeneratedDestinations(t *testing.T) {
	now := time.Date(2026, 10, 2, 11, 30, 0, 0, time.FixedZone("ICT", 7*3600))
	for _, engine := range []string{"postgres", "mysql", "mongodb", "sqlite"} {
		a, e := NewDestination(engine, "app", now)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := NewDestination(engine, "app", now)
		if a == b || !strings.Contains(a, "20261002_043000") {
			t.Fatal(a, b)
		}
		if e = ValidateNewDestination(engine, a); e != nil {
			t.Fatal(e)
		}
	}
}

func TestExportRemovesPartialOutputOnRawSizeMismatch(t *testing.T) {
	s, _, dir := setup(t, "gzip")
	m, e := s.Backup(context.Background(), "full", false)
	if e != nil {
		t.Fatal(e)
	}
	m.Pipeline.Raw++
	encoded, e := metadata.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, m.Name+".meta.json"), encoded, 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "partial.dump")
	if _, e = s.Export(context.Background(), m.Name, path, true); e == nil {
		t.Fatal("raw mismatch passed")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("partial output left behind", e)
	}
}
