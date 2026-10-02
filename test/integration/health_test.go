//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
)

// assertBackupHealth runs against actual registered backups/providers, with
// injected time so freshness checks never depend on real current time.
func assertBackupHealth(t *testing.T, ctx context.Context, s *app.Service, m metadata.Manifest) {
	t.Helper()
	previousNow, previousConfig := s.Now, s.Config
	defer func() { s.Now = previousNow; s.Config = previousConfig }()
	s.Config.Health.MaxBackupAge = "12h"
	s.Now = func() time.Time { return m.CompletedAt.Add(time.Hour) }
	wantIntegrity, wantFreshStatus := "unknown", app.Warning
	if s.Config.Protection != nil && s.Config.Protection.VerifyAfterBackup {
		wantIntegrity, wantFreshStatus = "verified", app.Healthy
	}
	r, err := s.Health(ctx, false)
	if err != nil || r.Status != wantFreshStatus || r.Databases[0].BackupName != m.Name || r.Databases[0].Integrity != wantIntegrity {
		t.Fatalf("fresh default health: %+v %v", r, err)
	}
	r, err = s.Health(ctx, true)
	if err != nil || r.Status != app.Healthy || r.Databases[0].Integrity != "verified" {
		t.Fatalf("active verified health: %+v %v", r, err)
	}
	s.Now = func() time.Time { return m.CreatedAt.Add(12 * time.Hour) }
	r, err = s.Health(ctx, false)
	// The explicit verification above persists immutable evidence, so all later
	// fresh health evaluations in this helper must include verified integrity.
	if err != nil || r.Status != app.Healthy || r.Databases[0].Stale == nil || *r.Databases[0].Stale {
		t.Fatalf("threshold boundary: %+v %v", r, err)
	}
	s.Now = func() time.Time { return m.CreatedAt.Add(12*time.Hour + time.Nanosecond) }
	r, err = s.Health(ctx, false)
	if err != nil || r.Status != app.Critical || !*r.Databases[0].Stale {
		t.Fatalf("stale health: %+v %v", r, err)
	}
	s.Config.Health.MaxBackupAge = ""
	r, err = s.Health(ctx, false)
	if err != nil || r.Status != app.Unknown {
		t.Fatalf("missing policy: %+v %v", r, err)
	}
	s.Config.Database.Database = "no-such-registered-database"
	r, err = s.Health(ctx, false)
	if err != nil || r.Status != app.Critical || r.Databases[0].BackupName != "" {
		t.Fatalf("missing backup: %+v %v", r, err)
	}
	s.Config = previousConfig
	if s.Config.Database.Type == "mysql" || s.Config.Database.Type == "mongodb" {
		r, err := s.RecoveryDrill(ctx, app.DrillOptions{Target: m.Name, RecoveryDatabase: filepath.Join(t.TempDir(), "isolated.sqlite"), Confirm: true})
		var typed *fault.Error
		if !errors.As(err, &typed) || typed.Kind != fault.Unsupported || r.TargetState != "not_created" || r.RecordKey != "" {
			t.Fatalf("native drill did not fail closed: %+v %v", r, err)
		}
	}
	t.Logf("health fresh/verified/boundary/stale/no-policy/no-backup passed: engine=%s storage=%s codec=%s", m.Database.Engine, s.Config.Storage.Type, m.Pipeline.Compression)
}
