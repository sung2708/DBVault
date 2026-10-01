package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = `version: "1"
database:
  type: postgres
  user: operator
  database: original
  password_env: TEST_DB_PASSWORD
storage:
  local:
    path: backups
`

func TestHealthPolicy(t *testing.T) {
	for _, value := range []string{"12h", "168h", "1h30m", "", "0", "-1h", "24d", "invalid", "999999999999h"} {
		c, err := Load(write(t, example+"health:\n  max_backup_age: \""+value+"\"\n"), Overrides{})
		valid := value == "12h" || value == "168h" || value == "1h30m" || value == ""
		if (err == nil) != valid {
			t.Fatalf("%q: %+v %v", value, c, err)
		}
	}
}

func TestVerifyAfterBackupPolicy(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		c, err := Load(write(t, example+"protection:\n  verify_after_backup: "+value+"\n"), Overrides{})
		if err != nil || c.Protection == nil || c.Protection.VerifyAfterBackup != (value == "true") {
			t.Fatalf("%s: %+v %v", value, c.Protection, err)
		}
	}
	if _, err := Load(write(t, example+"backup:\n  verify: true\n"), Overrides{}); err == nil {
		t.Fatal("accepted duplicate/unrecognized backup verification setting")
	}
}

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestLoadPrecedence(t *testing.T) {
	t.Setenv("DBVAULT_DB_NAME", "environment")
	t.Setenv("DBVAULT_DB_PORT", "5555")
	v := "cli"
	c, err := Load(write(t, example), Overrides{Database: &v})
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Database != "cli" || c.Database.Port != 5555 || c.Compression.Type != "gzip" {
		t.Fatalf("unexpected config: %+v", c)
	}
	c, err = Load(write(t, example), Overrides{})
	if err != nil || c.Database.Database != "environment" {
		t.Fatal(c, err)
	}
}
func TestLoadFailures(t *testing.T) {
	for _, tt := range []struct{ name, text string }{{"unknown", example + "password: SUPER_SECRET_DB_PASSWORD_12345\n"}, {"multiple", example + "---\nversion: \"1\"\n"}, {"version", strings.Replace(example, `version: "1"`, `version: "2"`, 1)}, {"flags", strings.Replace(example, "  type: postgres", "  type: postgres\n  options:\n    extra_flags: [--file=evil]", 1)}, {"conninfo", strings.Replace(example, "database: original", "database: 'postgres://user:secret@evil/db'", 1)}} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(write(t, tt.text), Overrides{})
			if err == nil {
				t.Fatal("expected rejection")
			}
			if strings.Contains(err.Error(), "SUPER_SECRET") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestNativeToolPathsValidateAndRoundTrip(t *testing.T) {
	toolDir := filepath.Join(t.TempDir(), "Program Files", "PostgreSQL", "18", "bin")
	pgDump := filepath.Join(toolDir, "pg_dump")
	pgRestore := filepath.Join(toolDir, "pg_restore")
	psql := filepath.Join(toolDir, "psql")
	valid := strings.Replace(example, "  database: original", "  database: original\n  tools:\n    pg_dump: '"+pgDump+"'\n    pg_restore: '"+pgRestore+"'\n    psql: '"+psql+"'", 1)
	c, err := Load(write(t, valid), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Tools["pg_dump"] != pgDump {
		t.Fatalf("path changed: %q", c.Database.Tools["pg_dump"])
	}
	for _, tools := range []string{"    pg_dump: pg_dump.exe\n", "    mysql: /usr/bin/mysql\n", "    pg_dump: ' \\\\server\\share\\pg_dump.exe'\n"} {
		text := strings.Replace(example, "  database: original", "  database: original\n  tools:\n"+tools, 1)
		if _, err := Load(write(t, text), Overrides{}); err == nil {
			t.Fatalf("accepted invalid tool configuration: %s", tools)
		}
	}
}
func TestSecretResolution(t *testing.T) {
	c, err := Load(write(t, example), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DB_PASSWORD", "")
	if _, err := c.Password(); err == nil {
		t.Fatal("missing password accepted")
	}
	t.Setenv("TEST_DB_PASSWORD", "SUPER_SECRET_DB_PASSWORD_12345")
	if p, err := c.Password(); err != nil || p != "SUPER_SECRET_DB_PASSWORD_12345" {
		t.Fatal("resolution failed")
	}
}
func TestDocumentedExample(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "configs", "*.yaml"))
	if err != nil || len(files) < 6 {
		t.Fatal("missing documented examples", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			if _, err := Load(file, Overrides{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
