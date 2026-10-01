package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) recoveryCommand() *cobra.Command {
	root := &cobra.Command{Use: "recovery", GroupID: "operations", Short: "Run controlled recovery tests in isolated targets", Long: "Recovery drills perform a real isolated restore followed by built-in validation.\nV1 supports SQLite only, into an explicitly selected NEW file. Server-engine\nisolation is not implemented and fails closed. No production-target bypass exists.", Args: noPositionalArgs, Example: "  dbvault recovery drill --help"}
	var drill app.DrillOptions
	var timeout time.Duration
	c := &cobra.Command{Use: "drill --target <backup-name> --recovery-database <new-sqlite-path>", Short: "Restore a SQLite backup into a new isolated file and validate it",
		Long:    "Restore the backup name returned by list into a NEW SQLite file.\nThe parent directory must exist and must be private to the operator. Existing\ntargets, links, source/production aliases and SQLite sidecars are refused.\n\n--confirm authorizes creating and restoring the isolated target. --dry-run\nverifies/preflights without creating it and is not a successful recovery drill.\nTargets are preserved by default and on failure/cancellation. --cleanup removes\nonly the file created by this run after successful validation and ownership checks.\nResults are saved as separate .recovery.json objects in the backup storage.\nChecksum integrity, restore completion and post-restore validation stay distinct.\nOnly SQLite is supported; PostgreSQL/MySQL/MongoDB fail before target mutation.",
		Example: "  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/new.sqlite --dry-run\n  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/new.sqlite --confirm\n  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/another.sqlite --confirm --cleanup --output json",
		Args:    noPositionalArgs, PreRunE: func(*cobra.Command, []string) error {
			if drill.Target == "" || drill.RecoveryDatabase == "" {
				return fmt.Errorf("--target and --recovery-database are required; use dbvault list for backup names")
			}
			if !drill.DryRun && !drill.Confirm {
				return fmt.Errorf("--confirm is required; use --dry-run to preview")
			}
			if drill.DryRun && drill.Cleanup {
				return fmt.Errorf("--cleanup cannot be combined with --dry-run")
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			return nil
		}, RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			cfg, err := o.load(c)
			if err != nil {
				return err
			}
			if cfg.Database.Type != "sqlite" {
				return fault.Wrap(fault.Unsupported, "recovery drill", fmt.Errorf("safe recovery drills currently support SQLite only; server target isolation is not implemented"))
			}
			adapter, err := databaseAdapter(cfg, "", o.redactor)
			if err != nil {
				return err
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return o.redactor.Error(fault.Wrap(fault.Storage, "initialize recovery storage", err))
			}
			defer closeStore()
			key, err := providers.Key(store, drill.Target)
			if err != nil {
				return err
			}
			drill.Target = key
			drill.CheckSpace = checkRecoverySpace
			drill.Redact = o.redactor.Text
			display := o.display(c)
			display.Configure(presentation.Details{Operation: "recovery drill", Config: cfg, ConfigPath: o.configPath, Target: key, DryRun: drill.DryRun}, o.redactor)
			stop := display.Start(ctx)
			svc := &app.Service{Config: cfg, DB: adapter, Store: store, Observe: display.Observe}
			result, checkErr := svc.RecoveryDrill(ctx, drill)
			stop()
			if err := o.output(c, result); err != nil {
				return err
			}
			return o.redactor.Error(checkErr)
		},
	}
	c.Flags().StringVarP(&drill.Target, "target", "t", "", targetHelp)
	c.Flags().StringVar(&drill.RecoveryDatabase, "recovery-database", "", "NEW isolated SQLite file; existing parent directory required")
	c.Flags().BoolVar(&drill.Confirm, "confirm", false, "Authorize creating and restoring the isolated target")
	c.Flags().BoolVar(&drill.DryRun, "dry-run", false, "Verify and preflight without creating or restoring the target")
	c.Flags().BoolVar(&drill.Cleanup, "cleanup", false, "Remove this run's owned file only after successful validation")
	c.Flags().DurationVar(&timeout, "timeout", 2*time.Hour, "Overall recovery-drill deadline")
	root.AddCommand(c)
	return root
}

func checkRecoverySpace(m metadata.Manifest, target string) error {
	// The verified compressed snapshot already occupies temp disk. Budget the
	// additional SQLite image plus target conservatively (including shared disks).
	if free, err := availableBytes(os.TempDir()); err == nil && free < uint64(m.Pipeline.Raw)*2 {
		return fmt.Errorf("insufficient temporary disk space for SQLite recovery image and target")
	}
	if free, err := availableBytes(filepath.Dir(target)); err == nil && free < uint64(m.Pipeline.Raw) {
		return fmt.Errorf("insufficient disk space for isolated recovery target")
	}
	return nil
}
