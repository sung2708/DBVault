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
	"path/filepath"
	"time"
)

func (o *options) selectRestore(c *cobra.Command) error {
	target, _ := c.Flags().GetString("target")
	interactive, _ := c.Flags().GetBool("interactive")
	nonInteractive, _ := c.Flags().GetBool("non-interactive")
	allowed := presentation.CanPrompt(c.InOrStdin(), c.ErrOrStderr(), nonInteractive, jsonMode(c), globalBool(c, "quiet"))
	if interactive && !allowed {
		return fmt.Errorf("--interactive requires terminal input/output and text mode; provide --target and destination flags instead")
	}
	if !interactive && (target != "" || !allowed) {
		return nil
	}
	cfg, err := o.load(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	store, closeStore, err := providers.Open(ctx, cfg.Storage)
	if err != nil {
		cancel()
		return err
	}
	svc := &app.Service{Config: cfg, Store: store}
	items, err := svc.List(ctx, "")
	closeErr := closeStore()
	cancel()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return o.promptRestore(c, &presentation.TerminalPrompter{In: c.InOrStdin(), Out: c.ErrOrStderr(), NoColor: o.noColor, Redactor: o.redactor}, cfg, items)
}

func (o *options) promptRestore(c *cobra.Command, p onboarding.Prompter, cfg config.Config, items []metadata.Manifest) error {
	target, _ := c.Flags().GetString("target")
	if target == "" {
		if len(items) == 0 {
			return fmt.Errorf("no completed backups found in configured storage")
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
			selected, err = list.SelectList(c.Context(), title+" (/ to filter)", picker.Header, picker.Choices, items[0].Name, min(len(picker.Choices), pickerRows)+pickerChrome)
		} else {
			selected, err = p.Select(c.Context(), title, picker.Choices, items[0].Name)
		}
		if err != nil {
			return err
		}
		if err = c.Flags().Set("target", selected); err != nil {
			return err
		}
		target = selected
	}
	if !c.Flags().Changed("database") && !c.Flags().Changed("new-database") {
		choices := []onboarding.Choice{{Label: "New database with UTC date/time", Value: "new"}, {Label: "New database with chosen name/path", Value: "custom-new"}, {Label: "Configured existing destination", Value: "existing"}, {Label: "Existing database with chosen name/path", Value: "custom-existing"}}
		fallback := "new"
		clean, _ := c.Flags().GetBool("clean")
		safety, _ := c.Flags().GetBool("backup-before-restore")
		if clean || safety {
			choices = choices[2:]
			fallback = "existing"
		}
		mode, err := p.Select(c.Context(), "Restore destination", choices, fallback)
		if err != nil {
			return err
		}
		if mode == "new" {
			name, err := app.NewDestination(cfg.Database.Type, cfg.Database.Database, time.Now())
			if err != nil {
				return err
			}
			c.Flags().Set("new-database", "true")
			c.Flags().Set("database", name)
		}
		if mode == "custom-new" || mode == "custom-existing" {
			fallback := cfg.Database.Database
			if mode == "custom-new" {
				fallback, err = app.NewDestination(cfg.Database.Type, cfg.Database.Database, time.Now())
				if err != nil {
					return err
				}
			}
			name, err := p.Input(c.Context(), "Destination database name (SQLite: file path)", fallback)
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("destination cannot be empty")
			}
			if mode == "custom-new" {
				if err = app.ValidateNewDestination(cfg.Database.Type, name); err != nil {
					return err
				}
				c.Flags().Set("new-database", "true")
			}
			c.Flags().Set("database", name)
		}
	}
	name, _ := c.Flags().GetString("database")
	if name == "" {
		name = cfg.Database.Database
	}
	newDB, _ := c.Flags().GetBool("new-database")
	if newDB && !c.Flags().Changed("database") {
		var err error
		name, err = app.NewDestination(cfg.Database.Type, cfg.Database.Database, time.Now())
		if err != nil {
			return err
		}
		c.Flags().Set("database", name)
	}
	dry, _ := c.Flags().GetBool("dry-run")
	if dry {
		return nil
	}
	if !newDB && !c.Flags().Changed("backup-before-restore") {
		yes, err := p.Confirm(c.Context(), "Create and verify a full destination backup before restoring?", true)
		if err != nil {
			return err
		}
		c.Flags().Set("backup-before-restore", fmt.Sprint(yes))
	}
	clean, _ := c.Flags().GetBool("clean")
	safety, _ := c.Flags().GetBool("backup-before-restore")
	tables, _ := c.Flags().GetStringSlice("table")
	schemas, _ := c.Flags().GetStringSlice("schema")
	collections, _ := c.Flags().GetStringSlice("collection")
	plan := presentation.RestorePlan{Manifest: metadata.Manifest{Name: target}, Destination: cfg.Database, NewDatabase: newDB, BackupBefore: safety, Clean: clean, Tables: tables, Schemas: schemas, Collections: collections}
	plan.Destination.Database = name
	for _, m := range items {
		if m.Name == target {
			plan.Manifest = m
			break
		}
	}
	p.Message("Restore plan\n" + presentation.FormatFields(plan.Fields(), presentation.Detect(c.ErrOrStderr()).Width))
	for _, w := range plan.Warnings() {
		if warner, ok := p.(onboarding.Warner); ok {
			warner.Warning(w)
		} else {
			p.Message("Warning: " + w)
		}
	}
	confirmed, _ := c.Flags().GetBool("confirm")
	if confirmed {
		return nil
	}
	if !newDB {
		// Writing into an existing destination needs the operator to name it;
		// --confirm remains the non-interactive authorization.
		want := destinationConfirmation(cfg.Database.Type, name)
		typed, err := p.Input(c.Context(), fmt.Sprintf("Type %q to restore into this existing destination. Stop application writes first.", want), "")
		if err != nil {
			return err
		}
		if typed != want {
			p.Message("Confirmation did not match; nothing was restored.")
			return context.Canceled
		}
		return c.Flags().Set("confirm", "true")
	}
	yes, err := p.Confirm(c.Context(), "Restore into this new destination?", false)
	if err != nil {
		return err
	}
	if !yes {
		return context.Canceled
	}
	return c.Flags().Set("confirm", "true")
}

const (
	// pickerRows bounds visible backups; pickerChrome covers the title,
	// filter and column header lines drawn above them.
	pickerRows   = 10
	pickerChrome = 3
)

// destinationConfirmation is the text an operator types to authorize a restore
// into an existing destination: the database name, or the SQLite file name.
func destinationConfirmation(engine, name string) string {
	if engine == "sqlite" {
		return filepath.Base(name)
	}
	return name
}
