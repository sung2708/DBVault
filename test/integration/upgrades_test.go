//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
)

func assertServerRecoveryUpgrade(t *testing.T, ctx context.Context, s *app.Service, m metadata.Manifest) metadata.Manifest {
	t.Helper()
	s.Config.Encryption = integrationEncryption(t)
	s.Config.Metrics.RecordOperations = true
	incremental, err := s.Backup(ctx, "incremental", false)
	if err != nil {
		t.Fatal("encrypted incremental", err)
	}
	if incremental.Delta == nil || incremental.Delta.BaseID != m.ID {
		t.Fatal("wrong base")
	}
	options := app.DrillOptions{Target: incremental.Name, RecoveryDatabase: "dbvault_isolated_recovery", Confirm: true, Cleanup: true}
	result, err := s.RecoveryDrill(ctx, options)
	if err != nil {
		t.Fatal("isolated recovery", err)
	}
	if result.Status != "passed" || result.TargetState != "removed" || result.BackupID != incremental.ID || result.Validation == nil || result.Validation.Objects < 1 {
		t.Fatalf("invalid drill result %+v", result)
	}
	record, err := s.Store.Get(ctx, result.RecordKey)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Close()
	if _, err = app.DecodeDrill(record); err != nil {
		t.Fatal("recovery evidence", err)
	}
	if _, err = s.Metrics(ctx); err != nil {
		t.Fatal("metrics", err)
	}
	return incremental
}

func integrationEncryption(t *testing.T) *config.Encryption {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBVAULT_INTEGRATION_KEY", base64.StdEncoding.EncodeToString(secret))
	return &config.Encryption{KeyID: "integration", Keys: map[string]string{"integration": "DBVAULT_INTEGRATION_KEY"}}
}
