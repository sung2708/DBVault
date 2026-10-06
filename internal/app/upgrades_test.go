package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/retention"
)

func TestEncryptedIncrementalRestoreAndRetention(t *testing.T) {
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			ctx := context.Background()
			s, db, dir := setup(t, codec)
			s.Config.Database.Type = "postgres"
			s.Config.Metrics.RecordOperations = true
			key := make([]byte, 32)
			rand.Read(key)
			t.Setenv("DBVAULT_TEST_KEY1", base64.StdEncoding.EncodeToString(key))
			rand.Read(key)
			t.Setenv("DBVAULT_TEST_KEY2", base64.StdEncoding.EncodeToString(key))
			s.Config.Encryption = &config.Encryption{KeyID: "v1", Keys: map[string]string{"v1": "DBVAULT_TEST_KEY1", "v2": "DBVAULT_TEST_KEY2"}}
			db.data = make([]byte, 4*65536+5)
			rand.Read(db.data)
			full, err := s.Backup(ctx, "full", false)
			if err != nil {
				t.Fatal(err)
			}
			db.data[65536+3] ^= 1
			db.data = append(db.data, []byte("more")...)
			expected := bytes.Clone(db.data)
			s.Config.Encryption.KeyID = "v2"
			increment, err := s.Backup(ctx, "incremental", false)
			if err != nil {
				t.Fatal(err)
			}
			if increment.Delta == nil || increment.Delta.BaseID != full.ID || increment.Encryption.KeyID != "v2" || increment.Pipeline.Stored >= full.Pipeline.Stored {
				t.Fatal("missing chain/rotation/savings")
			}
			for i := 0; i < 2; i++ {
				db.data[100+i] ^= 1
				expected = bytes.Clone(db.data)
				increment, err = s.Backup(ctx, "incremental", false)
				if err != nil {
					t.Fatal(err)
				}
			}
			db.data = nil
			db.restoreCalled = false
			if err = s.Restore(ctx, increment.Name, true, false, database.RestoreOptions{}); err != nil || !bytes.Equal(db.data, expected) {
				t.Fatal("restore chain", err)
			}
			// Sidecar codec changes cannot bypass authenticated archive context.
			altered := increment
			altered.Pipeline.Level = 1
			encoded, e := metadata.Encode(altered)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(dir, increment.Name+".meta.json"), encoded, 0600); e != nil {
				t.Fatal(e)
			}
			db.restoreCalled = false
			if e = s.Restore(ctx, increment.Name, true, false, database.RestoreOptions{}); e == nil || db.restoreCalled {
				t.Fatal("metadata tamper reached destination")
			}
			encoded, e = metadata.Encode(increment)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(dir, increment.Name+".meta.json"), encoded, 0600); e != nil {
				t.Fatal(e)
			}
			file := filepath.Join(t.TempDir(), "export.dump")
			if _, err = s.Export(ctx, increment.Name, file, true); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(file)
			if !bytes.Equal(data, expected) {
				t.Fatal("export differed")
			}
			if err = s.Delete(ctx, full.Name, true, false); err == nil {
				t.Fatal("deleted referenced base")
			}
			if err = s.Delete(ctx, full.Name, false, true); err == nil {
				t.Fatal("deletion preview accepted referenced base")
			}
			if err = s.VerifyChain(ctx, increment.Name); err != nil {
				t.Fatal("valid chain rejected", err)
			}
			items, err := s.List(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := retention.Select(items, config.Retention{KeepCount: 1}, time.Now().Add(24*time.Hour))
			if err != nil || len(candidates) != 0 {
				t.Fatal("retention breaks chain", err)
			}
			metrics, err := s.Metrics(ctx)
			if err != nil || !strings.Contains(metrics, `operation="backup",status="success"} 4`) {
				t.Fatal("metrics", metrics, err)
			}
			// A missing old wrapping key fails before the destination adapter is called.
			delete(s.Config.Encryption.Keys, "v1")
			db.restoreCalled = false
			if err = s.Restore(ctx, increment.Name, true, false, database.RestoreOptions{}); err == nil || db.restoreCalled {
				t.Fatal("missing ancestor key reached restore")
			}
			s.Config.Encryption.Keys["v1"] = "DBVAULT_TEST_KEY1"
			// Stored-byte verification catches corruption in an ancestor before writes.
			path := filepath.Join(dir, full.Name)
			corrupted, _ := os.ReadFile(path)
			corrupted[80] ^= 1
			os.WriteFile(path, corrupted, 0600)
			if err = s.VerifyChain(ctx, increment.Name); err == nil {
				t.Fatal("chain verification accepted corrupt ancestor")
			}
			db.restoreCalled = false
			if err = s.Restore(ctx, increment.Name, true, false, database.RestoreOptions{}); err == nil || db.restoreCalled {
				t.Fatal("corrupt ancestor reached restore")
			}
			s.Config.Health.MaxBackupAge = "24h"
			if report, e := s.Health(ctx, true); e == nil || report.Status != Critical {
				t.Fatal("health accepted corrupt ancestor", report, e)
			}
			if e = os.Remove(path); e != nil {
				t.Fatal(e)
			}
			if report, e := s.Health(ctx, false); e == nil || report.Status != Critical {
				t.Fatal("health accepted missing ancestor", report, e)
			}
		})
	}
}

func TestIncrementalLockAndDryRun(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	if _, err := s.Backup(ctx, "incremental", false); err == nil {
		t.Fatal("incremental without base")
	}
	if _, err := s.Backup(ctx, "full", false); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Store.List(ctx, "")
	if _, err := s.Backup(ctx, "incremental", true); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Store.List(ctx, "")
	if len(after) != len(before) {
		t.Fatal("dry run wrote")
	}
	release, err := s.chainLock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = s.Backup(ctx, "incremental", false); err == nil {
		t.Fatal("concurrent chain writer accepted")
	}
}
