package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/security"
)

func TestStatusCLIJSONAndMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	configPath, statePath := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "state.json")
	content := "version: \"1\"\ndatabase:\n  type: postgres\n  database: production\n  user: operator\n  password_env: STATUS_MISSING_SECRET\nstorage:\n  local:\n    path: " + filepath.ToSlash(dir) + "\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	data := []byte("fixture")
	m := metadata.Manifest{Version: "1.0", ID: "id-1", Name: "backup.dump", BackupType: "full", CreatedAt: now.Add(-18 * time.Minute), CompletedAt: now.Add(-17 * time.Minute), Status: "completed", Database: metadata.Database{Engine: "postgres", Name: "production", Format: "custom"}, Pipeline: metadata.Pipeline{Compression: "none", Level: 6, Stored: int64(len(data)), Raw: int64(len(data))}, Checksum: metadata.Checksum{Algorithm: "sha256", Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	encoded, err := metadata.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, m.Name), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, m.Name+".meta.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err = schedule.Update(statePath, func(s *schedule.State) error {
		s.Jobs = append(s.Jobs, schedule.Job{ID: "daily", Cron: "0 0 * * *", Config: configPath, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	for _, child := range root.Commands() {
		if child.Name() == "status" {
			root.RemoveCommand(child)
		}
	}
	root.AddCommand((&options{configPath: configPath, redactor: security.New()}).statusCommand(func() time.Time { return now }))
	root.SetArgs([]string{"status", "--config", configPath, "--state", statePath, "--output", "json"})
	command, err := root.ExecuteContextC(context.Background())
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	if stderr.Len() != 0 || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
	}
	var result app.StatusResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if result.Integrity != "unknown" || result.Protection != "not_configured" || len(result.RecentBackups) != 1 || result.RecentBackups[0].AgeSeconds != 18*60 || len(result.Schedules) != 1 || result.ScheduleNote == "" {
		t.Fatalf("%+v", result)
	}
	if strings.Contains(out.String(), "STATUS_MISSING_SECRET") || strings.Contains(out.String(), "password_env") {
		t.Fatal(out.String())
	}
	_ = command
}

func TestStatusMissingAndPartialScheduleState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	statePath := filepath.Join(dir, "bad-state.json")
	if err := os.WriteFile(configPath, []byte("version: \"1\"\ndatabase:\n  type: sqlite\n  database: "+filepath.ToSlash(filepath.Join(dir, "db.sqlite"))+"\nstorage:\n  local:\n    path: "+filepath.ToSlash(filepath.Join(dir, "backups"))+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"status", "--config", configPath, "--state", statePath, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result app.StatusResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if result.LastBackup != nil || result.BackupHealth != app.Critical || result.ScheduleNote == "" || result.Integrity != "unknown" {
		t.Fatalf("%+v", result)
	}
}

func TestStatusHelp(t *testing.T) {
	var out bytes.Buffer
	root := New(Build{}, &out, &out)
	root.SetArgs([]string{"status", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"existing", "recovery drill", "--output", "--no-color"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
