package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func commandFor(t *testing.T, name string, flags ...string) *cobra.Command {
	t.Helper()
	root := New(Build{}, os.Stdout, os.Stderr)
	c, _, err := root.Find([]string{name})
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

func TestPickerSelectsTargetForReadOnlyCommands(t *testing.T) {
	for _, name := range []string{"export", "inspect", "verify"} {
		c := commandFor(t, name)
		p := &listPrompt{}
		if err := promptTarget(c, p, name, promptBackups); err != nil {
			t.Fatal(name, err)
		}
		if target, _ := c.Flags().GetString("target"); target != promptBackups[0].Name {
			t.Fatal(name, "target not set", target)
		}
		if len(p.messages) != 0 || p.height == 0 {
			t.Fatal(name, "unexpected confirmation or missing picker", p.messages)
		}
	}
}

func TestPickedDeleteStillRequiresConfirmation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		c := commandFor(t, "delete")
		p := &listPrompt{restorePrompt: restorePrompt{cancel: cancel}}
		err := promptTarget(c, p, "delete", promptBackups)
		confirmed, _ := c.Flags().GetBool("confirm")
		if confirmed == cancel || (cancel && !errors.Is(err, context.Canceled)) || (!cancel && err != nil) {
			t.Fatal(cancel, confirmed, err)
		}
		all := strings.Join(p.messages, "\n")
		if !strings.Contains(all, "Delete plan") || !strings.Contains(all, "Backup        "+promptBackups[0].Name) || !strings.Contains(all, "Warning: Deletion permanently removes") {
			t.Fatal(all)
		}
	}
}

func TestPickedDeleteKeepsConfirmAndDryRunFlags(t *testing.T) {
	for _, flags := range [][]string{{"confirm", "true"}, {"dry-run", "true"}} {
		c := commandFor(t, "delete", flags...)
		p := &listPrompt{restorePrompt: restorePrompt{cancel: true}}
		if err := promptTarget(c, p, "delete", promptBackups); err != nil {
			t.Fatal(flags, err)
		}
		if len(p.messages) != 0 {
			t.Fatal(flags, "prompted for confirmation", p.messages)
		}
	}
}

func TestTargetPickerNeverRunsForMachineOutput(t *testing.T) {
	for _, args := range [][]string{{"inspect", "--json"}, {"verify", "--non-interactive"}, {"export", "--quiet"}, {"delete", "--dry-run", "--output", "json"}} {
		if _, _, err := uiRun(args...); err == nil || !strings.Contains(err.Error(), "--target is required") {
			t.Fatal(args, err)
		}
	}
}
