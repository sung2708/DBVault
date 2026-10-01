package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sung2708/DBVault/internal/doctor"
	"github.com/sung2708/DBVault/internal/fault"
)

func TestDoctorSQLiteJSONReport(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE sample(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "dbvault.yaml")
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"init", "--non-interactive", "--database", "sqlite", "--database-name", dbPath, "--storage", "local", "--output-dir", filepath.Join(dir, "backups"), "--config", configPath})
	if err = root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	root = New(Build{}, &out, &stderr)
	root.SetArgs([]string{"doctor", "--config", configPath, "--json"})
	if err = root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report doctor.Report
	if err = json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, out.String())
	}
	if !report.Ready || report.Summary.Failed != 0 || len(report.Checks) < 5 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
	}
}

func TestDoctorConfigurationFailureReport(t *testing.T) {
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"doctor", "--config", filepath.Join(t.TempDir(), "missing.yaml"), "--json"})
	err := root.ExecuteContext(context.Background())
	if fault.ExitCode(err) != 1 {
		t.Fatalf("expected readiness exit code 1, got %v", err)
	}
	WriteError(&stderr, root, err)
	var report doctor.Report
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatal(decodeErr, out.String())
	}
	if report.Ready || report.Summary.Failed != 1 || report.Summary.Skipped == 0 {
		t.Fatalf("unexpected failure report: %+v", report)
	}
	if !bytes.Contains(stderr.Bytes(), []byte(`"exit_code":1`)) {
		t.Fatalf("expected structured error diagnostic: %s", stderr.String())
	}
}
