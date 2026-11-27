package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/storage"
)

var healthNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestHealthPolicyAndIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name, policy     string
		age              time.Duration
		verify, noBackup bool
		status           HealthStatus
		integrity        string
	}{
		{"fresh", "12h", time.Hour, true, false, Healthy, "verified"},
		{"unverified", "12h", time.Hour, false, false, Warning, "unknown"},
		{"stale", "12h", 13 * time.Hour, true, false, Critical, "verified"},
		{"boundary", "12h", 12 * time.Hour, true, false, Healthy, "verified"},
		{"beyond boundary", "12h", 12*time.Hour + time.Nanosecond, false, false, Critical, "unknown"},
		{"missing policy", "", time.Hour, true, false, Unknown, "verified"},
		{"no backup", "12h", 0, false, true, Critical, "unknown"},
		{"future", "12h", -time.Hour, false, false, Unknown, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := setup(t, "none")
			s.Config.Database.Type = "postgres"
			s.Config.Health.MaxBackupAge = tc.policy
			s.Now = func() time.Time { return healthNow.Add(-tc.age) }
			if !tc.noBackup {
				if _, err := s.Backup(context.Background(), "full", false); err != nil {
					t.Fatal(err)
				}
			}
			s.Now = func() time.Time { return healthNow.In(time.FixedZone("Bangkok", 7*3600)) }
			r, err := s.Health(context.Background(), tc.verify)
			if err != nil || r.Status != tc.status || r.Databases[0].Integrity != tc.integrity {
				t.Fatalf("%+v: %v", r, err)
			}
			h := r.Databases[0]
			if h.RestoreTest != "unknown" {
				t.Fatal("invented recovery evidence")
			}
			if !tc.noBackup && tc.age >= 0 && (h.AgeSeconds == nil || *h.AgeSeconds != tc.age.Seconds()) {
				t.Fatalf("age: %+v", h)
			}
			if h.Stale != nil && *h.Stale != (tc.age > 12*time.Hour) {
				t.Fatal("boundary arithmetic")
			}
		})
	}
}

func TestHealthBrokenRegistryAndArtifact(t *testing.T) {
	for _, mode := range []string{"missing", "size", "checksum", "metadata", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			s, _, dir := setup(t, "none")
			s.Config.Database.Type = "postgres"
			s.Config.Health.MaxBackupAge = "12h"
			s.Now = func() time.Time { return healthNow.Add(-time.Hour) }
			m, err := s.Backup(context.Background(), "full", false)
			if err != nil {
				t.Fatal(err)
			}
			s.Now = func() time.Time { return healthNow }
			switch mode {
			case "missing":
				err = os.Remove(filepath.Join(dir, m.Name))
			case "size":
				err = os.WriteFile(filepath.Join(dir, m.Name), []byte("short"), 0600)
			case "checksum":
				err = os.WriteFile(filepath.Join(dir, m.Name), []byte(strings.Repeat("x", int(m.Pipeline.Stored))), 0600)
			case "metadata":
				err = os.WriteFile(filepath.Join(dir, m.Name+".meta.json"), []byte("{}"), 0600)
			case "orphan":
				err = os.Remove(filepath.Join(dir, m.Name+".meta.json"))
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.Health(context.Background(), mode == "checksum")
			if mode == "metadata" {
				if r.Status != Unknown || fault.ExitCode(err) != 4 {
					t.Fatalf("%+v %v", r, err)
				}
				return
			}
			if r.Status != Critical {
				t.Fatalf("%+v %v", r, err)
			}
			if mode == "checksum" && fault.ExitCode(err) != 4 {
				t.Fatal(err)
			}
			if mode != "checksum" && err != nil {
				t.Fatal(err)
			}
		})
	}
}

type healthStore struct {
	storage.Provider
	artifactReads, existsCalls int
	listErr, existsErr         error
	onArtifactRead             func()
}

func (p *healthStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !strings.HasSuffix(key, ".meta.json") {
		p.artifactReads++
		if p.onArtifactRead != nil {
			p.onArtifactRead()
		}
	}
	return p.Provider.Get(ctx, key)
}
func (p *healthStore) Exists(ctx context.Context, key string) (bool, error) {
	p.existsCalls++
	if p.existsErr != nil {
		return false, p.existsErr
	}
	return p.Provider.Exists(ctx, key)
}
func (p *healthStore) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.Provider.List(ctx, prefix)
}

func TestHealthFreshnessAfterSlowVerification(t *testing.T) {
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	current := healthNow
	s.Now = func() time.Time { return current }
	if _, err := s.Backup(context.Background(), "full", false); err != nil {
		t.Fatal(err)
	}
	current = healthNow.Add(time.Hour)
	s.Store = &healthStore{Provider: s.Store, onArtifactRead: func() { current = healthNow.Add(13 * time.Hour) }}
	r, err := s.Health(context.Background(), true)
	if err != nil || r.Status != Critical || r.Databases[0].Integrity != "verified" || *r.Databases[0].AgeSeconds != (13*time.Hour).Seconds() || !*r.Databases[0].Stale {
		t.Fatal(r, err)
	}
}

func TestHealthLatestTargetAndCheapDefault(t *testing.T) {
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	ctx := context.Background()
	s.Now = func() time.Time { return healthNow.Add(-2 * time.Hour) }
	if _, err := s.Backup(ctx, "full", false); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return healthNow.Add(-time.Hour) }
	latest, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.Database.Database = "other"
	s.Now = func() time.Time { return healthNow }
	if _, err := s.Backup(ctx, "full", false); err != nil {
		t.Fatal(err)
	}
	s.Config.Database.Database = "fixture"
	p := &healthStore{Provider: s.Store}
	s.Store = p
	r, err := s.Health(ctx, false)
	if err != nil || r.Databases[0].BackupName != latest.Name || p.artifactReads != 0 || p.existsCalls != 1 {
		t.Fatalf("%+v reads=%d exists=%d err=%v", r, p.artifactReads, p.existsCalls, err)
	}
	p.listErr = errors.New("storage unavailable")
	r, err = s.Health(ctx, false)
	if r.Status != Unknown || fault.ExitCode(err) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	p.listErr = nil
	p.existsErr = errors.New("storage unavailable")
	r, err = s.Health(ctx, false)
	if r.Status != Unknown || fault.ExitCode(err) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Health(cancelCtx, false)
	if fault.ExitCode(err) != 5 {
		t.Fatal(err)
	}
}

type catalogReadCounter struct {
	storage.Provider
	gets  map[string]int
	lists int
}

func (s *catalogReadCounter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	s.gets[key]++
	return s.Provider.Get(ctx, key)
}

func (s *catalogReadCounter) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	s.lists++
	return s.Provider.List(ctx, prefix)
}

func TestHealthSelectsScopeAndWatchCachesLegacyEvidence(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Now = func() time.Time { return healthNow.Add(-time.Hour) }
	full, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.Database.Options.IncludeTables = []string{"public.audit_log"}
	s.Now = func() time.Time { return healthNow.Add(-time.Minute) }
	partial, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.Database.Options.IncludeTables = nil
	metrics, err := s.Metrics(ctx)
	if err != nil || !strings.Contains(metrics, `dbvault_last_backup_timestamp_seconds{engine="postgres",database="fixture"} `+strconv.FormatInt(full.CreatedAt.Unix(), 10)+"\n") {
		t.Fatalf("whole-database metrics did not select the whole backup: %v\n%s", err, metrics)
	}
	snapshot, err := s.ReadMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const ageSeries = `dbvault_backup_age_seconds{engine="postgres",database="fixture"} `
	if !strings.Contains(snapshot.Text(full.CreatedAt.Add(time.Second)), ageSeries+"1\n") ||
		!strings.Contains(snapshot.Text(full.CreatedAt.Add(3*time.Second)), ageSeries+"3\n") {
		t.Fatal("backup age did not advance while reusing a metrics snapshot")
	}
	legacy := "verification_legacy.json"
	record, _, err := newVerificationRecord(full, VerificationResult{Status: "verified"}, s.now())
	if err != nil {
		t.Fatal(err)
	}
	record.RecordKey = legacy
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Put(ctx, legacy, bytes.NewReader(b), int64(len(b))); err != nil {
		t.Fatal(err)
	}
	unrelated := evidencePrefix("verification_", partial.ID) + "bad.json"
	if err := s.Store.Put(ctx, unrelated, strings.NewReader("bad"), 3); err != nil {
		t.Fatal(err)
	}
	store := &catalogReadCounter{Provider: s.Store, gets: make(map[string]int)}
	s.Store = store
	cache := &EvidenceCache{}
	for range 2 {
		h, err := s.HealthWithEvidenceCache(ctx, false, cache)
		if err != nil || h.Databases[0].BackupName != full.Name || h.Databases[0].Integrity != "verified" {
			t.Fatalf("health = %+v, error = %v", h, err)
		}
	}
	if store.gets[legacy] != 1 || store.gets[unrelated] != 0 {
		t.Fatalf("legacy reads=%d unrelated reads=%d", store.gets[legacy], store.gets[unrelated])
	}
	s.Config.Database.Options.IncludeTables = []string{"public.audit_log"}
	h, err := s.HealthWithEvidenceCache(ctx, false, cache)
	if err != nil || h.Databases[0].BackupName != partial.Name || store.gets[unrelated] != 1 {
		t.Fatalf("filtered health = %+v, error = %v", h, err)
	}
}

func TestCleanupDoesNotRescanForEachCandidate(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Config.Retention.KeepCount = 1
	var missing string
	for i := range 5 {
		s.Now = func() time.Time { return healthNow.Add(time.Duration(i) * time.Hour) }
		m, err := s.Backup(ctx, "full", false)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			missing = m.Name
		}
	}
	if err := s.Store.Delete(ctx, missing); err != nil {
		t.Fatal(err)
	}
	store := &catalogReadCounter{Provider: s.Store, gets: make(map[string]int)}
	s.Store = store
	deleted, err := s.Cleanup(ctx, s.now(), false)
	if err != nil || len(deleted) != 4 || store.lists != 2 {
		t.Fatalf("deleted=%d listings=%d err=%v", len(deleted), store.lists, err)
	}
	if _, err := s.ReadManifest(ctx, missing); err == nil {
		t.Fatal("missing archive manifest was retained")
	}
	remaining, err := s.List(ctx, "")
	if err != nil || len(remaining) != 1 || !remaining[0].CreatedAt.Equal(s.now()) {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
}

func TestCleanupRefusesMissingRetainedArchive(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Retention.KeepCount = 1
	s.Now = func() time.Time { return healthNow.Add(-time.Hour) }
	old, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return healthNow }
	latest, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Delete(ctx, latest.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(ctx, s.now(), false); err == nil {
		t.Fatal("cleanup accepted a missing retained archive")
	}
	if exists, err := s.Store.Exists(ctx, old.Name); err != nil || !exists {
		t.Fatalf("older archive was not protected: exists=%t err=%v", exists, err)
	}
}

func TestCleanupClearsVerifiedRetainedDeletionMarker(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Retention.KeepCount = 1
	m, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	marker := m.Name + ".meta.json.deleting"
	if err := s.Store.Put(ctx, marker, strings.NewReader(m.Name), int64(len(m.Name))); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.Cleanup(ctx, s.now(), false)
	if err != nil || len(deleted) != 0 {
		t.Fatalf("cleanup deleted=%d error=%v", len(deleted), err)
	}
	if _, err := s.Backup(ctx, "incremental", true); err != nil {
		t.Fatalf("base is still blocked: %v", err)
	}
}

func TestDeleteProtectsBaseWhenRegisteredChildArchiveIsMissing(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	base, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.Backup(ctx, "incremental", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Delete(ctx, child.Name); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		if err := s.Delete(ctx, base.Name, true, dry); err == nil || !strings.Contains(err.Error(), child.Name) {
			t.Fatalf("delete dry=%t failed to protect base: %v", dry, err)
		}
	}
}
