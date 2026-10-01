package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
