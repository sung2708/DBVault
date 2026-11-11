package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/onboarding"
)

func restoreCommand(t *testing.T, flags ...string) *cobra.Command {
	t.Helper()
	root := New(Build{}, os.Stdout, os.Stderr)
	c, _, err := root.Find([]string{"restore"})
	if err != nil {
		t.Fatal(err)
	}
	c.SetContext(context.Background())
	for i := 0; i+1 < len(flags); i += 2 {
		if err := c.Flags().Set(flags[i], flags[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

var promptBackups = []metadata.Manifest{{Name: "app_20261101_020000.dump.gz", BackupType: "full", CreatedAt: time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), Database: metadata.Database{Engine: "postgres", Name: "app"}, Pipeline: metadata.Pipeline{Stored: 2048}}}

func TestExistingRestoreRequiresTypedDestination(t *testing.T) {
	cfg := config.Config{Database: config.Database{Type: "postgres", Host: "db", Port: 5432, Database: "app"}}
	for _, tc := range []struct {
		typed string
		want  bool
	}{{"app", true}, {"App", false}, {"", false}, {"yes", false}} {
		c := restoreCommand(t, "database", "app", "backup-before-restore", "true")
		p := &restorePrompt{typed: tc.typed}
		err := (&options{}).promptRestore(c, p, cfg, promptBackups)
		confirmed, _ := c.Flags().GetBool("confirm")
		if confirmed != tc.want || (tc.want && err != nil) || (!tc.want && !errors.Is(err, context.Canceled)) {
			t.Fatalf("typed %q: confirmed=%t err=%v", tc.typed, confirmed, err)
		}
		if len(p.inputs) != 1 || !strings.Contains(p.inputs[0], `"app"`) {
			t.Fatal("missing typed confirmation prompt", p.inputs)
		}
	}
}

func TestSQLiteTypedConfirmationUsesFileName(t *testing.T) {
	cfg := config.Config{Database: config.Database{Type: "sqlite", Database: "/srv/data/app.db"}}
	c := restoreCommand(t, "database", "/srv/data/app.db", "backup-before-restore", "true")
	p := &restorePrompt{typed: "app.db"}
	if err := (&options{}).promptRestore(c, p, cfg, promptBackups); err != nil {
		t.Fatal(err)
	}
	if confirmed, _ := c.Flags().GetBool("confirm"); !confirmed {
		t.Fatal("file name not accepted")
	}
}

func TestConfirmFlagSkipsTypedConfirmation(t *testing.T) {
	cfg := config.Config{Database: config.Database{Type: "postgres", Database: "app"}}
	c := restoreCommand(t, "database", "app", "backup-before-restore", "false", "confirm", "true")
	p := &restorePrompt{}
	if err := (&options{}).promptRestore(c, p, cfg, promptBackups); err != nil {
		t.Fatal(err)
	}
	if len(p.inputs) != 0 {
		t.Fatal("--confirm still prompted", p.inputs)
	}
}

func TestRestoreSummaryLabelsFieldsAndWarnsOnClean(t *testing.T) {
	cfg := config.Config{Database: config.Database{Type: "postgres", Host: "db", Port: 5432, Database: "app"}}
	c := restoreCommand(t, "database", "app", "backup-before-restore", "true", "clean", "true", "table", "public.orders")
	p := &restorePrompt{typed: "app"}
	if err := (&options{}).promptRestore(c, p, cfg, promptBackups); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(p.messages, "\n")
	for _, want := range []string{"Restore plan", "Backup        app_20261101_020000.dump.gz", "Source        PostgreSQL / app", "Server        db:5432", "Mode          Restore into existing database", "Tables        public.orders", "Warning: --clean drops"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in\n%s", want, all)
		}
	}
	for _, unwanted := range []string{"[]", "true", "false", "Schemas", "Collections"} {
		if strings.Contains(all, unwanted) {
			t.Fatalf("raw or empty value %q in\n%s", unwanted, all)
		}
	}
}

type listPrompt struct {
	restorePrompt
	header string
	height int
	labels []string
}

func (p *listPrompt) SelectList(_ context.Context, _, header string, c []onboarding.Choice, _ string, height int) (string, error) {
	p.header, p.height = header, height
	for _, choice := range c {
		p.labels = append(p.labels, choice.Label)
	}
	return c[len(c)-1].Value, nil
}

func TestBackupPickerUsesFilterableColumns(t *testing.T) {
	items := make([]metadata.Manifest, 15)
	for i := range items {
		items[i] = promptBackups[0]
		items[i].Name = strings.Repeat("x", i) + ".dump.gz"
	}
	items[3].Encryption = &metadata.Encryption{Algorithm: "aes256-gcm-stream-v2", KeyID: "k"}
	cfg := config.Config{Database: config.Database{Type: "postgres", Database: "app"}}
	c := restoreCommand(t, "new-database", "true", "confirm", "true")
	p := &listPrompt{}
	if err := (&options{}).promptRestore(c, p, cfg, items); err != nil {
		t.Fatal(err)
	}
	if target, _ := c.Flags().GetString("target"); target != items[14].Name {
		t.Fatal("selection not applied", target)
	}
	if p.height != pickerRows+pickerChrome || !strings.HasPrefix(p.header, "CREATED (UTC)") || !strings.Contains(p.header, "ENC") {
		t.Fatal(p.height, p.header)
	}
	if !strings.Contains(p.labels[3], "  yes  ") || !strings.Contains(p.labels[0], "  full  ") || strings.Contains(p.labels[0], "|") {
		t.Fatal(p.labels[3], p.labels[0])
	}
}
