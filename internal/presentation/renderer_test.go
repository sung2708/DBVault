package presentation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/security"
)

func enableColorEnv(t *testing.T) { t.Helper(); t.Setenv("NO_COLOR", ""); os.Unsetenv("NO_COLOR") }
func tty(width int) *Capabilities {
	return &Capabilities{TTY: true, Color: true, Unicode: true, Width: width}
}
func TestTerminalPolicies(t *testing.T) {
	enableColorEnv(t)
	cases := []struct {
		name      string
		options   Options
		env       bool
		wantColor bool
	}{
		{"tty", Options{OutCaps: tty(100), ErrCaps: tty(100)}, false, true},
		{"no-color", Options{OutCaps: tty(100), ErrCaps: tty(100), NoColor: true}, false, false},
		{"environment", Options{OutCaps: tty(100), ErrCaps: tty(100)}, true, false},
		{"redirect-out", Options{OutCaps: &Capabilities{Width: 80}, ErrCaps: tty(100)}, false, false},
		{"redirect-err", Options{OutCaps: tty(100), ErrCaps: &Capabilities{Width: 80}}, false, false},
		{"quiet", Options{OutCaps: tty(100), ErrCaps: tty(100), Quiet: true}, false, false},
		{"json", Options{OutCaps: tty(100), ErrCaps: tty(100), JSON: true}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env {
				t.Setenv("NO_COLOR", "1")
			}
			var out, err bytes.Buffer
			r := New(&out, &err, tc.options)
			if r.outCaps.Color != tc.wantColor {
				t.Fatal("color policy")
			}
			r.Status("success", "Completed")
			if strings.Contains(err.String(), "\x1b[") != tc.wantColor && !tc.options.Quiet && !tc.options.JSON {
				t.Fatal(err.String())
			}
			if !tc.wantColor && r.animation() {
				t.Fatal("animation escaped policy")
			}
			if tc.options.JSON || tc.options.Quiet {
				if err.Len() != 0 {
					t.Fatal("decorative machine/quiet output")
				}
			}
		})
	}
}
func TestFormatters(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{{0, "0 B"}, {1023, "1023 B"}, {1024, "1.00 KiB"}, {2415919104, "2.25 GiB"}, {-1, "unknown"}} {
		if got := FormatBytes(tc.n); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	if FormatDuration(102*time.Second) != "1m42s" || FormatDuration(-1) != "0s" {
		t.Fatal("duration")
	}
	if ServerVersion("postgres", "160004") != "16.4" || ServerVersion("mongodb", "8.0.15") != "8.0.15" {
		t.Fatal("server version")
	}
	hash := strings.Repeat("a", 64)
	if FormatChecksum(hash, true) != "sha256:"+hash || len(FormatChecksum(hash, false)) >= len(hash) {
		t.Fatal("checksum")
	}
	if FormatTime(time.Time{}) != "unknown" {
		t.Fatal("zero time")
	}
}
func TestActualProgressAndLifecycle(t *testing.T) {
	enableColorEnv(t)
	now := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	var out, err bytes.Buffer
	r := New(&out, &err, Options{OutCaps: tty(100), ErrCaps: tty(100), Now: func() time.Time { return now }})
	r.Step("verify")
	r.tick(now.Add(100 * time.Millisecond))
	if err.Len() != 0 {
		t.Fatal("short operation animated")
	}
	r.Observe(app.Event{Stage: "verify", State: "progress", Bytes: 1024, Total: 2048})
	r.tick(now.Add(time.Second))
	if !strings.Contains(ansi.Strip(err.String()), "50%") || !strings.Contains(err.String(), "1.00 KiB / 2.00 KiB") {
		t.Fatal(err.String())
	}
	r.Done("verify")
	if r.visible || r.active || !strings.Contains(err.String(), "verified") {
		t.Fatal("unfinished line")
	}
	err.Reset()
	r.Step("backup.stream")
	r.Observe(app.Event{Stage: "backup.stream", State: "progress", Bytes: 2048})
	r.tick(now.Add(time.Second))
	if strings.Contains(err.String(), "%") || !strings.Contains(err.String(), "2.00 KiB") {
		t.Fatal("invented total", err.String())
	}
	stop := r.Start(context.Background())
	stop()
	stop()
	if r.visible {
		t.Fatal("cursor line left behind")
	}
}
func TestNarrowTablePreservesNamesAndRedacts(t *testing.T) {
	var out, err bytes.Buffer
	name := "database_20261001_020000_" + strings.Repeat("a", 32) + ".dump.gz"
	r := New(&out, &err, Options{NoColor: true, OutCaps: tty(32), ErrCaps: tty(32), Redactor: security.New("test-secret")})
	m := metadata.Manifest{Name: name, Database: metadata.Database{Engine: "postgres", Name: "test-secret"}, Pipeline: metadata.Pipeline{Stored: 1024}, CreatedAt: time.Now(), BackupType: "full"}
	if e := r.Result("list", []metadata.Manifest{m}); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "test-secret") || strings.Contains(out.String(), "\x1b") {
		t.Fatal(out.String())
	}
	if !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(out.String(), "\n", ""), " ", ""), name) {
		t.Fatal("backup name truncated", out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if ansi.StringWidth(line) > 32 && !strings.HasPrefix(line, "Use a backup") {
			t.Fatalf("wide line %q", line)
		}
	}
}
func TestTablesSummariesEmptyAndControlSafety(t *testing.T) {
	var out, err bytes.Buffer
	r := New(&out, &err, Options{OutCaps: &Capabilities{Width: 150}, Redactor: security.New("password-value")})
	m := metadata.Manifest{Name: "demo.dump.gz", ID: "abc", Database: metadata.Database{Engine: "postgres", Name: "password-value\x1b[31m\nFAKE"}, Pipeline: metadata.Pipeline{Stored: 1024, Raw: 2048, Compression: "gzip"}, Checksum: metadata.Checksum{Hash: strings.Repeat("a", 64)}, Duration: 1, BackupType: "full", Storage: "local"}
	if e := r.Result("backup", m); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "Backup completed") || strings.Contains(out.String(), "password-value") || strings.Contains(out.String(), "\x1b") {
		t.Fatal(out.String())
	}
	out.Reset()
	r.Result("list", []metadata.Manifest{m})
	if !strings.Contains(out.String(), "BACKUP NAME") || !strings.Contains(out.String(), "1.00 KiB") {
		t.Fatal(out.String())
	}
	out.Reset()
	r.Result("list", []metadata.Manifest{})
	if !strings.Contains(out.String(), "No backups found") || !strings.Contains(out.String(), "dbvault backup") {
		t.Fatal(out.String())
	}
}
func TestErrorsAndVerboseLogsAreSafe(t *testing.T) {
	for _, machine := range []bool{false, true} {
		var out, err bytes.Buffer
		r := New(&out, &err, Options{JSON: machine, Verbose: true, Redactor: security.New("test-password")})
		r.Error("backup", "dbvault backup [flags]", "dbvault backup --help", errors.New("https://user:test-password@host failure\x1b[31m"))
		if strings.Contains(err.String(), "test-password") || strings.Contains(err.String(), "\x1b") || out.Len() != 0 {
			t.Fatal(err.String())
		}
		if machine {
			var record map[string]any
			if e := json.Unmarshal(err.Bytes(), &record); e != nil {
				t.Fatal(e)
			}
		}
	}
	var out, err bytes.Buffer
	r := New(&out, &err, Options{Verbose: true, Redactor: security.New("test-password")})
	logger := slog.New(&HumanHandler{Renderer: r})
	logger.Warn("notification failed", "detail", "test-password")
	if strings.Contains(err.String(), "test-password") || !strings.Contains(err.String(), "[REDACTED]") {
		t.Fatal(err.String())
	}
}
