package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/fault"
)

func TestInitSQLiteEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE example(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetIn(strings.NewReader("must not read stdin"))
	root.SetArgs([]string{"init", "--non-interactive", "--database", "sqlite", "--database-name", dbPath, "--storage", "local", "--config", path, "--test", "--json"})
	if err = root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if result["connection_verified"] != true || stderr.Len() != 0 {
		t.Fatal(result, stderr.String())
	}
	if _, err = config.Load(path, config.Overrides{}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	root = New(Build{}, &out, &stderr)
	root.SetArgs([]string{"config", "--config", path})
	if err = root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestInitMissingNativeDependency(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("DBVAULT_INIT_TEST_PASSWORD", "temporary-test-fixture-secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"init", "--non-interactive", "--database", "postgres", "--database-name", "prod", "--user", "backup", "--storage", "local", "--password-env", "DBVAULT_INIT_TEST_PASSWORD", "--config", path, "--test"})
	err := root.Execute()
	if fault.ExitCode(err) != 3 {
		t.Fatal("expected dependency exit code", err)
	}
	if strings.Contains(err.Error(), "temporary-test-fixture-secret") || out.Len() != 0 {
		t.Fatal("test failure leaked secret/output", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed test wrote config")
	}
}

func TestInitNonTTYAndSecrets(t *testing.T) {
	t.Setenv("DBVAULT_DB_PASSWORD", "must-not-leak-password")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "must-not-leak-cloud-secret")
	t.Setenv("AWS_SESSION_TOKEN", "must-not-leak-session-token")
	t.Setenv("AZURE_CLIENT_SECRET", "must-not-leak-azure-secret")
	t.Setenv("SLACK_WEBHOOK_URL", "must-not-leak-webhook")
	t.Setenv("NO_COLOR", "1")
	for _, extra := range [][]string{nil, {"--non-interactive"}, {"--json"}, {"--quiet"}, {"--no-color"}} {
		var out, stderr bytes.Buffer
		root := New(Build{}, &out, &stderr)
		root.SetIn(strings.NewReader(""))
		root.SetArgs(append([]string{"init", "--config", filepath.Join(t.TempDir(), "missing.yaml")}, extra...))
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "--database") {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatal("failure polluted stdout")
		}
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"init", "--database", "postgres", "--database-name", "prod", "--user", "backup", "--storage", "local", "--config", path, "--no-color"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, secret := range []string{"must-not-leak-password", "must-not-leak-cloud-secret", "must-not-leak-session-token", "must-not-leak-azure-secret", "must-not-leak-webhook", "\x1b["} {
		if strings.Contains(out.String()+stderr.String()+string(data), secret) {
			t.Fatal("leaked output", secret)
		}
	}
}
