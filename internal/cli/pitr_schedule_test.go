package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/schedule"
)

func TestNativeScheduleCLIAndExecutionRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.yaml")
	state := filepath.Join(dir, "jobs.json")
	text := "version: '1'\ndatabase:\n  type: mysql\n  host: localhost\n  port: 3306\n  user: root\n  database: app\n  password_env: NATIVE_SCHEDULE_PASS\nstorage:\n  local:\n    path: " + filepath.ToSlash(filepath.Join(dir, "backups")) + "\npitr: {}\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATIVE_SCHEDULE_PASS", "secret")
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"schedule", "add", "--id", "native", "--operation", "pitr", "--type", "incremental", "--base-every", "24h", "--cleanup", "--cron", "*/5 * * * *", "--config", path, "--state", state})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s, err := schedule.Load(state)
	if err != nil || len(s.Jobs) != 1 || s.Jobs[0].Operation != "pitr" || s.Jobs[0].BaseEvery != "24h" || !s.Jobs[0].Cleanup {
		t.Fatal(s, err)
	}
	// An invalid interval reaches native backup validation, proving scheduled
	// jobs forward the native flags through the same CLI command pipeline.
	job := s.Jobs[0]
	job.BaseEvery = "-1h"
	root = New(Build{}, &out, &stderr)
	o := &options{}
	if err = o.executeScheduledJob(root, context.Background(), job); err == nil || !strings.Contains(err.Error(), "base interval must be nonnegative") {
		t.Fatal("native command not dispatched", err)
	}
	if strings.Contains(out.String(), "secret") || strings.Contains(stderr.String(), "secret") {
		t.Fatal("credential exposed")
	}
}

func TestProtectedNativeBackupRejectsCleanupBeforeBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.yaml")
	text := `version: "1"
database:
  type: postgres
  database: app
  user: operator
  password_env: NATIVE_MISSING_PASSWORD
storage:
  type: s3
  s3:
    bucket: example
    region: us-east-1
    protect_deletes: true
pitr: {}
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"pitr", "backup", "--cleanup", "--config", path})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--cleanup: S3 deletion disabled") {
		t.Fatalf("backup error = %v", err)
	}
	state := filepath.Join(t.TempDir(), "jobs.json")
	root = New(Build{}, &out, &stderr)
	root.SetArgs([]string{"schedule", "add", "--id", "native", "--operation", "pitr", "--cleanup", "--cron", "*/5 * * * *", "--config", path, "--state", state})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--cleanup: S3 deletion disabled") {
		t.Fatalf("schedule error = %v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("invalid job wrote state: %v", err)
	}
}
