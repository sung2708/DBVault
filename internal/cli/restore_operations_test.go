package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/onboarding"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteRestoreNewSafetyHistoryExport(t *testing.T) {
	cfg, source := uiFixture(t)
	out, diag, e := uiRun("backup", "--config", cfg, "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	var m metadata.Manifest
	if e = json.Unmarshal([]byte(out), &m); e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(t.TempDir(), "restore.sqlite")
	out, diag, e = uiRun("restore", "--config", cfg, "--target", m.Name, "--new-database", "--database", destination, "--dry-run", "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	jsonLines(t, diag)
	if _, e = os.Stat(destination); !os.IsNotExist(e) {
		t.Fatal("dry-run created file")
	}
	out, diag, e = uiRun("restore", "--config", cfg, "--target", m.Name, "--new-database", "--database", destination, "--confirm", "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	var result app.RestoreResult
	if e = json.Unmarshal([]byte(out), &result); e != nil || result.Status != "completed" || result.Validation != "structure_checked" {
		t.Fatal(result, e, out)
	}
	db, e := sql.Open("sqlite", destination)
	if e != nil {
		t.Fatal(e)
	}
	var value string
	e = db.QueryRow("SELECT value FROM demo").Scan(&value)
	db.Close()
	if e != nil || value != "original" {
		t.Fatal(value, e)
	}
	if _, _, e = uiRun("restore", "--config", cfg, "--target", m.Name, "--new-database", "--database", destination, "--confirm", "--json"); e == nil {
		t.Fatal("existing target accepted")
	}
	db, e = sql.Open("sqlite", source)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("UPDATE demo SET value='before overwrite'")
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	out, diag, e = uiRun("restore", "--config", cfg, "--target", m.Name, "--backup-before-restore", "--confirm", "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	if e = json.Unmarshal([]byte(out), &result); e != nil || result.SafetyBackup == "" {
		t.Fatal(result, e)
	}
	raw := filepath.Join(t.TempDir(), "safety.sqlite")
	out, diag, e = uiRun("export", "--config", cfg, "--target", result.SafetyBackup, "--file", raw, "--decompress", "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	db, e = sql.Open("sqlite", raw)
	if e != nil {
		t.Fatal(e)
	}
	e = db.QueryRow("SELECT value FROM demo").Scan(&value)
	db.Close()
	if e != nil || value != "before overwrite" {
		t.Fatal(value, e)
	}
	out, diag, e = uiRun("history", "--config", cfg, "--json")
	if e != nil {
		t.Fatal(e, diag)
	}
	var history []app.RestoreResult
	if e = json.Unmarshal([]byte(out), &history); e != nil || len(history) != 3 {
		t.Fatal(history, e)
	}
	jsonLines(t, diag)
	out, diag, e = uiRun("history", "--config", cfg, "--no-color")
	if e != nil || !strings.Contains(out, "Restore history") || strings.Contains(out, "\x1b") {
		t.Fatal(out, diag, e)
	}
}

type restorePrompt struct {
	cancel   bool
	messages []string
}

func (p *restorePrompt) Select(_ context.Context, _ string, c []onboarding.Choice, _ string) (string, error) {
	return c[0].Value, nil
}
func (*restorePrompt) Input(context.Context, string, string) (string, error) { return "", nil }
func (*restorePrompt) Secret(context.Context, string) (string, error)        { return "", nil }
func (p *restorePrompt) Confirm(context.Context, string, bool) (bool, error) { return !p.cancel, nil }
func (p *restorePrompt) Message(s string)                                    { p.messages = append(p.messages, s) }
func TestRestorePromptDefaultsToNewAndCancelDoesNotAuthorize(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		root := New(Build{}, os.Stdout, os.Stderr)
		c, _, e := root.Find([]string{"restore"})
		if e != nil {
			t.Fatal(e)
		}
		c.SetContext(context.Background())
		p := &restorePrompt{cancel: cancel}
		o := &options{}
		e = o.promptRestore(c, p, config.Config{Database: config.Database{Type: "postgres", Database: "app"}}, []metadata.Manifest{{Name: "fixture.dump"}})
		confirmed, _ := c.Flags().GetBool("confirm")
		newDB, _ := c.Flags().GetBool("new-database")
		name, _ := c.Flags().GetString("database")
		if confirmed == cancel || !newDB || !strings.Contains(name, "app_restore_") {
			t.Fatal(confirmed, newDB, name, e)
		}
		if cancel && e == nil {
			t.Fatal("cancel ignored")
		}
	}
}
func TestRestoreMachineNeverPrompts(t *testing.T) {
	for _, args := range [][]string{{"restore", "--json"}, {"restore", "--interactive", "--json"}, {"restore", "--non-interactive"}} {
		_, _, e := uiRun(args...)
		if e == nil {
			t.Fatal("missing flags accepted", args)
		}
	}
}

func TestInteractiveSafetyFlagSelectsExistingDestination(t *testing.T) {
	root := New(Build{}, os.Stdout, os.Stderr)
	c, _, e := root.Find([]string{"restore"})
	if e != nil {
		t.Fatal(e)
	}
	c.SetContext(context.Background())
	c.Flags().Set("backup-before-restore", "true")
	p := &restorePrompt{}
	o := &options{}
	if e = o.promptRestore(c, p, config.Config{Database: config.Database{Type: "postgres", Database: "app"}}, []metadata.Manifest{{Name: "fixture.dump"}}); e != nil {
		t.Fatal(e)
	}
	newDB, _ := c.Flags().GetBool("new-database")
	if newDB {
		t.Fatal("explicit safety backup accidentally selected a new database")
	}
}
