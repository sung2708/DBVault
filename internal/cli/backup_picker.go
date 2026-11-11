package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/onboarding"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

const (
	// pickerRows bounds visible backups; pickerChrome covers the title,
	// filter and column header lines drawn above them.
	pickerRows   = 10
	pickerChrome = 3
)

// selectTarget offers the backup picker to export, inspect, verify and delete
// when --target is omitted, under the same prompt policy as restore. Machine
// output, quiet mode, non-terminals and --non-interactive keep the existing
// "--target is required" error.
func (o *options) selectTarget(c *cobra.Command, name string) (*config.Config, error) {
	target, _ := c.Flags().GetString("target")
	nonInteractive, _ := c.Flags().GetBool("non-interactive")
	if target != "" || !presentation.CanPrompt(c.InOrStdin(), c.ErrOrStderr(), nonInteractive, jsonMode(c), globalBool(c, "quiet")) {
		return nil, nil
	}
	cfg, items, err := o.listForPicker(c)
	if err != nil {
		return nil, err
	}
	return &cfg, promptTarget(c, o.terminalPrompter(c), name, items)
}

func promptTarget(c *cobra.Command, p onboarding.Prompter, name string, items []metadata.Manifest) error {
	target, err := pickBackup(c, p, items)
	if err != nil {
		return err
	}
	if name != "delete" {
		return nil
	}
	confirmed, _ := c.Flags().GetBool("confirm")
	dry, _ := c.Flags().GetBool("dry-run")
	if confirmed || dry {
		return nil
	}
	for _, m := range items {
		if m.Name == target {
			p.Message("Delete plan\n" + presentation.FormatFields(presentation.RestorePlan{Manifest: m}.SourceFields(), presentation.Detect(c.ErrOrStderr()).Width))
			break
		}
	}
	warning := "Deletion permanently removes this backup archive and its metadata."
	if warner, ok := p.(onboarding.Warner); ok {
		warner.Warning(warning)
	} else {
		p.Message("Warning: " + warning)
	}
	yes, err := p.Confirm(c.Context(), "Delete this backup?", false)
	if err != nil {
		return err
	}
	if !yes {
		return context.Canceled
	}
	return c.Flags().Set("confirm", "true")
}

// pickBackup shows the aligned, filterable backup list and stores the choice
// in --target.
func pickBackup(c *cobra.Command, p onboarding.Prompter, items []metadata.Manifest) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no completed backups found in configured storage")
	}
	caps := presentation.Detect(c.ErrOrStderr())
	picker := presentation.BackupChoices(items, caps.Width, caps.Unicode)
	title := "Choose backup"
	if picker.Source != "" {
		title += " of " + picker.Source
	}
	var selected string
	var err error
	if list, ok := p.(onboarding.ListSelector); ok {
		selected, err = list.SelectList(c.Context(), title, picker.Header, picker.Choices, items[0].Name, min(len(picker.Choices), pickerRows)+pickerChrome)
	} else {
		selected, err = p.Select(c.Context(), title, picker.Choices, items[0].Name)
	}
	if err != nil {
		return "", err
	}
	return selected, c.Flags().Set("target", selected)
}

func (o *options) listForPicker(c *cobra.Command) (config.Config, []metadata.Manifest, error) {
	cfg, err := o.load(c)
	if err != nil {
		return cfg, nil, err
	}
	ctx, cancel, err := operationContext(c)
	if err != nil {
		return cfg, nil, err
	}
	o.terminalPrompter(c).Message("Loading backups from configured storage...")
	defer cancel()
	store, closeStore, err := providers.Open(ctx, cfg.Storage)
	if err != nil {
		return cfg, nil, err
	}
	svc := &app.Service{Config: cfg, Store: store}
	items, err := svc.List(ctx, "")
	if closeErr := closeStore(); err == nil {
		err = closeErr
	}
	return cfg, items, err
}

func (o *options) terminalPrompter(c *cobra.Command) *presentation.TerminalPrompter {
	return &presentation.TerminalPrompter{In: c.InOrStdin(), Out: c.ErrOrStderr(), NoColor: o.noColor, Redactor: o.redactor}
}
