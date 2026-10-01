package presentation

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/metadata"
)

func TestStatusPresentationWidthAndEvidence(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	passed := now.Add(-72 * time.Hour)
	result := app.StatusResult{Database: app.StatusDatabase{Name: "production", Engine: "postgres"}, Protection: "not_configured", BackupHealth: app.Warning,
		LastBackup: &app.StatusBackup{Name: "db.dump", AgeSeconds: 18 * 60, Type: "full", StoredBytes: 5 << 30, Status: "completed"}, Integrity: "unknown",
		RecoveryDrill: app.StatusRecovery{Status: "passed", CompletedAt: &passed}, Storage: app.StatusStorage{Type: "local"},
		Schedules: []app.StatusSchedule{{ID: "six-hourly", Cron: "0 */6 * * *", Enabled: true}}, ScheduleNote: "Saved definitions do not prove the scheduler process is running",
		RecentBackups: []app.StatusBackup{{AgeSeconds: 18 * 60, Type: "full", StoredBytes: 5 << 30, Status: "completed"}}, GeneratedAt: now}
	for _, width := range []int{32, 100} {
		var out bytes.Buffer
		r := New(&out, &bytes.Buffer{}, Options{OutCaps: &Capabilities{TTY: true, Width: width, Unicode: true}, ErrCaps: &Capabilities{TTY: true, Width: width}, Now: func() time.Time { return now }})
		if err := r.Result("status", result); err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(strings.ReplaceAll(out.String(), "\n", " "), " ", "")
		for _, expected := range []string{"DBVaultStatus", "production", "Notconfigured", "unknown", "passed(72h0m0sago)", "5.00GiB", "RecentBackups", "livenessunknown"} {
			if !strings.Contains(text, expected) {
				t.Errorf("width %d missing %q:\n%s", width, expected, text)
			}
		}
		if strings.Contains(text, "Scheduler running") || strings.Contains(text, "Next Backup") {
			t.Fatal(text)
		}
	}
}

func TestStatusNonTTYAndQuiet(t *testing.T) {
	result := app.StatusResult{Database: app.StatusDatabase{Name: "db", Engine: "sqlite"}, BackupHealth: app.Critical, Integrity: "unknown", RecoveryDrill: app.StatusRecovery{Status: "never_tested"}, Storage: app.StatusStorage{Type: "s3"}, RecentBackups: []app.StatusBackup{}}
	for _, opts := range []Options{{}, {Quiet: true}, {NoColor: true}} {
		var out bytes.Buffer
		r := New(&out, &bytes.Buffer{}, opts)
		if err := r.Result("status", result); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "DBVault Status") {
			t.Fatalf("%+v: %q", opts, out.String())
		}
	}
}

func TestBackupResultMakesVerificationFailureExplicit(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, &bytes.Buffer{}, Options{})
	result := app.BackupResult{Manifest: metadata.Manifest{ID: "id", Name: "backup.dump", Database: metadata.Database{Name: "production", Engine: "postgres"}, Pipeline: metadata.Pipeline{Stored: 4096, Compression: "gzip"}, Storage: "s3"}, BackupID: "id", BackupStatus: "success", Verification: app.VerificationResult{Requested: true, Status: "failed", Algorithm: "sha256", Bytes: 4096, FailureCategory: "integrity"}}
	if err := r.Result("backup", result); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{"Backup artifact created; verification failed", "Backup ID", "Integrity", "failed", "artifact remains stored"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q: %s", expected, text)
		}
	}
}
