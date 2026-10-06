package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/security"
)

func TestHealthCLI(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		flags  []string
		policy string
		age    time.Duration
		backup bool
		code   int
		status app.HealthStatus
	}{
		{"fresh JSON", []string{"--verify", "--output", "json"}, "12h", time.Hour, true, 0, app.Healthy},
		{"warning JSON", []string{"--json"}, "12h", time.Hour, true, 1, app.Warning},
		{"stale", nil, "12h", 13 * time.Hour, true, 1, app.Critical},
		{"stale JSON", []string{"--output", "json"}, "12h", 13 * time.Hour, true, 1, app.Critical},
		{"no backup", nil, "12h", 0, false, 1, app.Critical},
		{"no backup JSON", []string{"--json"}, "12h", 0, false, 1, app.Critical},
		{"no policy", []string{"--verify", "--json"}, "", time.Hour, true, 1, app.Unknown},
		{"quiet", []string{"--quiet"}, "12h", time.Hour, true, 1, app.Warning},
		{"no color", []string{"--no-color"}, "12h", time.Hour, true, 1, app.Warning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			statePath := filepath.Join(dir, "state.json")
			yaml := "version: \"1\"\ndatabase:\n  type: postgres\n  database: production\n  user: operator\n  password_env: HEALTH_TEST_MISSING\nstorage:\n  local:\n    path: " + filepath.ToSlash(dir) + "\nnotifications:\n  slack:\n    enabled: true\n    webhook_url_env: HEALTH_TEST_MISSING_WEBHOOK\n"
			if tc.policy != "" {
				yaml += "health:\n  max_backup_age: " + tc.policy + "\n"
			}
			if err := os.WriteFile(configPath, []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.backup {
				data := []byte("fixture")
				hash := sha256.Sum256(data)
				m := metadata.Manifest{Version: "1.0", ID: "fixture", Name: "fixture.dump", BackupType: "full", CreatedAt: now.Add(-tc.age), CompletedAt: now.Add(-tc.age), Status: "completed",
					Database: metadata.Database{Engine: "postgres", Name: "production", Format: "custom"}, Pipeline: metadata.Pipeline{Compression: "none", Level: 6, Stored: int64(len(data)), Raw: int64(len(data))}, Checksum: metadata.Checksum{Algorithm: "sha256", Hash: hex.EncodeToString(hash[:])}}
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
			}
			var out, stderr bytes.Buffer
			root := New(Build{}, &out, &stderr)
			for _, c := range root.Commands() {
				if c.Name() == "health" {
					root.RemoveCommand(c)
				}
			}
			o := &options{configPath: configPath, redactor: security.New()}
			root.AddCommand(o.healthCommand(func() time.Time { return now }))
			root.SetArgs(append([]string{"health", "--config", configPath, "--state", statePath}, tc.flags...))
			c, err := root.ExecuteContextC(context.Background())
			if fault.ExitCode(err) != tc.code {
				t.Fatalf("exit %d: %v", fault.ExitCode(err), err)
			}
			if err != nil {
				WriteError(&stderr, c, err)
			}
			if stderr.Len() != 0 || strings.Contains(out.String(), "\x1b") {
				t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
			}
			if jsonMode(c) {
				var report app.HealthReport
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal(err, out.String())
				}
				if report.Status != tc.status {
					t.Fatal(report)
				}
			} else if !strings.Contains(out.String(), string(tc.status)) || !strings.Contains(out.String(), "Restore test") {
				t.Fatal(out.String())
			}
			if tc.name == "quiet" && (strings.Contains(out.String(), "[WARN]") || strings.Contains(out.String(), "→")) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestHealthSchedules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, enabled := range []bool{false, true} {
		maxAge := float64(3600)
		r := app.HealthReport{Status: app.Healthy, Databases: []app.DatabaseHealth{{Status: app.Healthy, MaxAgeSeconds: &maxAge}}}
		state := schedule.State{Jobs: []schedule.Job{{ID: "matching", Config: path, Cron: "0 */6 * * *", Enabled: enabled}, {ID: "other", Config: path + ".other", Cron: "0 * * * *", Enabled: false}}}
		if err := healthSchedules(&r, state, path); err != nil {
			t.Fatal(err)
		}
		if len(r.Databases[0].Schedules) != 1 || !strings.Contains(r.Databases[0].ScheduleNote, "do not prove") {
			t.Fatal(r)
		}
		want := app.Warning
		if enabled {
			want = app.Healthy
		}
		if r.Status != want {
			t.Fatal(r)
		}
	}
}

func TestNativeScheduleDefinitionsRemainAdvisory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.yaml")
	for _, enabled := range []bool{false, true} {
		maxAge := float64(3600)
		r := app.HealthReport{Status: app.Healthy, Databases: []app.DatabaseHealth{{BackupScope: "pitr", Status: app.Healthy, MaxAgeSeconds: &maxAge}}}
		state := schedule.State{Jobs: []schedule.Job{{ID: "logical", Config: path, Operation: "backup", Enabled: true}, {ID: "native", Config: path, Operation: "pitr", Enabled: enabled}}}
		if err := healthSchedules(&r, state, path); err != nil {
			t.Fatal(err)
		}
		want := app.Warning
		if enabled {
			want = app.Healthy
		}
		if r.Status != want || !strings.Contains(r.Databases[0].ScheduleNote, "do not prove") {
			t.Fatal(r)
		}
		// No definition is allowed to upgrade stale evidence to healthy.
		r.Status = app.Critical
		r.Databases[0].Status = app.Critical
		if err := healthSchedules(&r, state, path); err != nil || r.Status != app.Critical {
			t.Fatal(r, err)
		}
	}
}

func TestHealthHelpAndInvalidFlags(t *testing.T) {
	for _, flags := range [][]string{{"--help"}, {"--timeout", "0"}, {"--timeout", "-1s"}, {"positional"}} {
		var out bytes.Buffer
		root := New(Build{}, &out, &out)
		root.SetArgs(append([]string{"health"}, flags...))
		err := root.Execute()
		if flags[0] == "--help" {
			if err != nil || !strings.Contains(out.String(), "--verify") {
				t.Fatal(err, out.String())
			}
		} else if err == nil {
			t.Fatal("invalid flags accepted")
		}
	}
}
