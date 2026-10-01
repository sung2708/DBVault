package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/metadata"
)

func uiFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "demo.sqlite")
	db, e := sql.Open("sqlite", source)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE demo(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO demo(value) VALUES ('original')"); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	cfg := filepath.Join(dir, "dbvault.yaml")
	text := fmt.Sprintf("version: \"1\"\ndatabase:\n  type: sqlite\n  database: %q\nstorage:\n  type: local\n  local:\n    path: %q\ncompression:\n  type: gzip\n  level: 6\n", source, filepath.Join(dir, "backups"))
	if e = os.WriteFile(cfg, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	return cfg, source
}
func uiRun(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	c := New(Build{Version: "test", Commit: "fixture", Date: "2026-10-01"}, &out, &errOut)
	c.SetArgs(args)
	resolved, e := c.ExecuteContextC(context.Background())
	if e != nil {
		if resolved == nil {
			resolved = c
		}
		WriteError(&errOut, resolved, e)
	}
	return out.String(), errOut.String(), e
}
func jsonLines(t *testing.T, s string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" && !json.Valid([]byte(line)) {
			t.Fatalf("non-JSON diagnostic: %q", line)
		}
	}
	if strings.Contains(s, "\x1b") {
		t.Fatal("ANSI in JSON")
	}
}
func TestPresentationSQLiteMachineAndHuman(t *testing.T) {
	cfg, _ := uiFixture(t)
	out, diag, e := uiRun("backup", "--config", cfg, "--output", "json")
	if e != nil {
		t.Fatal(e, diag)
	}
	var m metadata.Manifest
	dec := json.NewDecoder(strings.NewReader(out))
	if e = dec.Decode(&m); e != nil {
		t.Fatal(e, out)
	}
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		t.Fatal("extra stdout", out)
	}
	if m.Name == "" || m.Pipeline.Stored <= 0 {
		t.Fatal(m)
	}
	jsonLines(t, diag)
	out, diag, e = uiRun("list", "--config", cfg)
	if e != nil || !strings.Contains(out, m.Name) || strings.Contains(out+diag, "\x1b") {
		t.Fatal(e, out, diag)
	}
	if !strings.Contains(diag, "Backup listing loaded") {
		t.Fatal(diag)
	}
	out, diag, e = uiRun("backup", "--config", cfg, "--no-color")
	if e != nil || !strings.Contains(out, "Backup completed") || strings.Contains(out+diag, "\x1b") || strings.Contains(diag, "%") {
		t.Fatal(e, out, diag)
	}
	out, diag, e = uiRun("backup", "--config", cfg, "--quiet", "--output", "json")
	if e != nil || !json.Valid([]byte(out)) || diag != "" {
		t.Fatal(e, out, diag)
	}
	out, diag, e = uiRun("backup", "--config", cfg, "--quiet")
	if e != nil || diag != "" || len(strings.Split(strings.TrimSpace(out), "\n")) != 1 {
		t.Fatal(e, out, diag)
	}
	legacy, _, e := uiRun("version", "--json")
	if e != nil {
		t.Fatal(e)
	}
	alias, _, e := uiRun("version", "--output", "json")
	if e != nil || legacy != alias {
		t.Fatal("alias changes schema", legacy, alias, e)
	}
}
func TestPresentationFailuresAndConfirmations(t *testing.T) {
	cfg, source := uiFixture(t)
	out, diag, e := uiRun("backup", "--config", cfg, "--database", source+".missing", "--output", "json")
	if e == nil || out != "" {
		t.Fatal("false success", out, diag)
	}
	jsonLines(t, diag)
	out, diag, e = uiRun("backup", "--config", cfg, "--database", source+".missing", "--no-color")
	if e == nil || out != "" || !strings.Contains(diag, "Error:") || strings.Contains(diag, "Backup completed") || strings.Contains(diag, "\x1b") {
		t.Fatal(e, out, diag)
	}
	out, diag, e = uiRun("backup", "--config", cfg, "--output", "json", "--quiet")
	if e != nil {
		t.Fatal(e, diag)
	}
	var m metadata.Manifest
	if e = json.Unmarshal([]byte(out), &m); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"restore", "delete"} {
		out, diag, e = uiRun(op, "--config", cfg, "--target", m.Name)
		if e == nil || out != "" || !strings.Contains(diag, "--confirm") {
			t.Fatal("confirmation bypass", op, e, out, diag)
		}
	}
	for _, op := range []string{"restore", "delete"} {
		out, diag, e = uiRun(op, "--config", cfg, "--target", m.Name, "--dry-run")
		if e != nil || !strings.Contains(out, "preview") {
			t.Fatal(op, e, out, diag)
		}
	}
	out, diag, e = uiRun("list", "--config", cfg, "--output", "json")
	if e != nil || !strings.Contains(out, m.Name) {
		t.Fatal("dry run deleted", e, out, diag)
	}
	out, diag, e = uiRun("version", "--output", "xml")
	if e == nil || !strings.Contains(diag, "text or json") {
		t.Fatal(e, out, diag)
	}
	out, diag, e = uiRun("version", "--json", "--output", "text")
	if e == nil {
		t.Fatal("conflicting formats accepted", out, diag)
	}
	jsonLines(t, diag)
}
