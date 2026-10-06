package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/encryption"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/pitr"
	"github.com/sung2708/DBVault/internal/storage"
	"github.com/sung2708/DBVault/internal/storage/local"
)

type nativeMonitorStore struct {
	storage.Provider
	writes, archiveReads int
	listErr              error
}

func (p *nativeMonitorStore) Put(context.Context, string, io.Reader, int64) error {
	p.writes++
	return errors.New("read-only monitor")
}
func (p *nativeMonitorStore) Delete(context.Context, string) error {
	p.writes++
	return errors.New("read-only monitor")
}
func (p *nativeMonitorStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !strings.HasSuffix(key, ".pitr.json") {
		p.archiveReads++
	}
	return p.Provider.Get(ctx, key)
}
func (p *nativeMonitorStore) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.Provider.List(ctx, prefix)
}

func TestNativeStorageOnlyHealth(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		age    time.Duration
		verify bool
		mutate string
		want   HealthStatus
		code   int
	}{
		{"fresh", time.Hour, false, "", Warning, 1},
		{"verified", time.Hour, true, "", Healthy, 0},
		{"boundary", 12 * time.Hour, true, "", Healthy, 0},
		{"overdue", 12*time.Hour + time.Second, false, "", Critical, 1},
		{"future", -time.Hour, false, "", Unknown, 1},
		{"no backup", time.Hour, false, "empty", Critical, 1},
		{"other source", time.Hour, false, "identity", Critical, 1},
		{"missing ancestor", time.Hour, false, "missing", Critical, 1},
		{"bad parent", time.Hour, false, "parent", Critical, 4},
		{"ancestor corrupt", time.Hour, true, "corrupt", Critical, 4},
		{"storage unavailable", time.Hour, false, "storage", Unknown, 1},
		{"no policy", time.Hour, false, "policy", Unknown, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := local.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			data := []byte("payload")
			sum := sha256.Sum256(data)
			base := pitr.Record{Version: 1, Name: "pitr_00000000000000000000000000000001.tar.enc", Engine: "mysql", Identity: "source-uuid", ServerVersion: "8.4.0", GTIDMode: "OFF", Kind: "base", Start: "binlog.000001:4", End: "binlog.000001:4", From: now.Add(-tc.age - time.Hour), Until: now.Add(-tc.age - time.Hour), Hash: hex.EncodeToString(sum[:]), Size: int64(len(data)), KeyID: "unavailable-wrapping-key", Algorithm: encryption.Algorithm}
			child := base
			child.Name = "pitr_00000000000000000000000000000002.tar.enc"
			child.Kind = "logs"
			child.Parent = base.Name
			child.ParentHash = base.Hash
			child.From = base.Until
			child.Until = now.Add(-tc.age)
			child.End = "binlog.000002:4"
			if tc.mutate == "parent" {
				child.ParentHash = strings.Repeat("b", 64)
			}
			if tc.mutate != "empty" {
				for _, m := range []pitr.Record{base, child} {
					encoded, _ := json.Marshal(m)
					if err = store.Put(ctx, m.Name+".pitr.json", bytes.NewReader(encoded), int64(len(encoded))); err != nil {
						t.Fatal(err)
					}
					if err = store.Put(ctx, m.Name, bytes.NewReader(data), int64(len(data))); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.mutate == "missing" {
				store.Delete(ctx, base.Name)
			}
			if tc.mutate == "corrupt" {
				store.Delete(ctx, base.Name)
				store.Put(ctx, base.Name, bytes.NewReader([]byte("changed")), 7)
			}
			readonly := &nativeMonitorStore{Provider: store}
			cfg := config.Config{Database: config.Database{Type: "mysql", Database: "app"}, Health: config.Health{MaxBackupAge: "12h", BackupScope: "pitr", SourceIdentity: "source-uuid"}}
			if tc.mutate == "identity" {
				cfg.Health.SourceIdentity = "wrong-source"
			}
			if tc.mutate == "policy" {
				cfg.Health.MaxBackupAge = ""
			}
			if tc.mutate == "storage" {
				readonly.listErr = errors.New("offline")
			}
			svc := Service{Config: cfg, Store: readonly, Now: func() time.Time { return now }}
			r, e := svc.Health(ctx, tc.verify)
			code := fault.ExitCode(e)
			if e == nil && r.Status != Healthy {
				code = fault.ExitCode(&HealthFailure{r.Status})
			}
			if r.Status != tc.want || code != tc.code || readonly.writes != 0 {
				t.Fatal(r, e, code, readonly.writes)
			}
			if !tc.verify && readonly.archiveReads != 0 {
				t.Fatal("default monitor read archive")
			}
			if tc.name == "verified" && readonly.archiveReads != 2 {
				t.Fatal("did not verify complete chain", readonly.archiveReads)
			}
		})
	}
}
