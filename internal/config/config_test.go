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
