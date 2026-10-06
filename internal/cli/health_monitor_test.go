package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/fault"
)

func TestHealthUnavailableSharedStorageJSONAndTimeout(t *testing.T) {
	const privateDiagnostic = "provider-internal-sensitive-value"
	for _, tc := range []struct {
		name string
		code int
	}{{"provider error", 1}, {"deadline", 5}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.code == 5 {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("<Error><Code>AccessDenied</Code><Message>" + privateDiagnostic + "</Message></Error>"))
			}))
			defer server.Close()
			dir := t.TempDir()
			cfg := filepath.Join(dir, "monitor.yaml")
			t.Setenv("MONITOR_S3_ID", "test-access")
			t.Setenv("MONITOR_S3_KEY", "test-secret")
			text := "version: '1'\ndatabase:\n  type: postgres\n  database: production\n  user: unused\n  password_env: UNUSED_DB_PASSWORD\nstorage:\n  type: s3\n  s3:\n    bucket: backups\n    region: us-east-1\n    endpoint: " + server.URL + "\n    access_key_env: MONITOR_S3_ID\n    secret_key_env: MONITOR_S3_KEY\nhealth:\n  max_backup_age: 7h\n"
			if err := os.WriteFile(cfg, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			root := New(Build{}, &out, &stderr)
			root.SetArgs([]string{"health", "--config", cfg, "--state", "", "--json", "--timeout", "1s"})
			cmd, err := root.ExecuteContextC(context.Background())
			if err == nil || fault.ExitCode(err) != tc.code {
				t.Fatal(err, fault.ExitCode(err))
			}
			if tc.code == 1 {
				bounded, ok := err.(*healthOperationError)
				if !ok || !strings.Contains(bounded.cause.Error(), privateDiagnostic) {
					t.Fatal("provider diagnostic fixture did not reach error handler")
				}
			}
			WriteError(&stderr, cmd, err)
			var r app.HealthReport
			if e := json.Unmarshal(out.Bytes(), &r); e != nil || r.Status != app.Unknown {
				t.Fatal(r, e, out.String())
			}
			for _, s := range []string{privateDiagnostic, "test-secret", "test-access"} {
				if strings.Contains(out.String()+stderr.String(), s) {
					t.Fatal("sensitive diagnostics leaked")
				}
			}
		})
	}
}

func TestHealthRuntimeFailureJSONAndIndependentState(t *testing.T) {
	for _, kind := range []string{"storage", "config", "state", "skip-state"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "monitor.yaml")
			storage := filepath.Join(dir, "storage")
			state := filepath.Join(dir, "schedules.json")
			if err := os.WriteFile(state, []byte("broken state"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "storage" {
				if err := os.WriteFile(storage, []byte("file not directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MONITOR_UNUSED_PASSWORD", "credential-must-never-appear")
			text := "version: '1'\ndatabase:\n  type: postgres\n  database: production\n  user: unused\n  password_env: MONITOR_UNUSED_PASSWORD\nstorage:\n  local:\n    path: " + filepath.ToSlash(storage) + "\nhealth:\n  max_backup_age: 12h\n"
			if kind != "config" {
				if err := os.WriteFile(cfg, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stateArg := ""
			if kind == "state" {
				stateArg = state
			}
			var out, stderr bytes.Buffer
			root := New(Build{}, &out, &stderr)
			root.SetArgs([]string{"health", "--config", cfg, "--state", stateArg, "--output", "json"})
			cmd, err := root.ExecuteContextC(context.Background())
			if err == nil || fault.ExitCode(err) != 1 {
				t.Fatal(err)
			}
			WriteError(&stderr, cmd, err)
			var report app.HealthReport
			if e := json.Unmarshal(out.Bytes(), &report); e != nil {
				t.Fatal(e, out.String())
			}
			want := app.Unknown
			if kind == "skip-state" {
				want = app.Critical
			}
			if report.Status != want {
				t.Fatal(report)
			}
			if kind == "config" && len(report.Databases) != 0 {
				t.Fatal("invented database identity")
			}
			if strings.Contains(out.String()+stderr.String(), "credential-must-never-appear") {
				t.Fatal("credential leaked")
			}
		})
	}
}
