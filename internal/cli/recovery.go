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
	"github.com/sung2708/DBVault/internal/notify"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) recoveryCommand() *cobra.Command {
	root := &cobra.Command{Use: "recovery", GroupID: "operations", Short: "Run controlled recovery tests in isolated targets", Long: "Recovery drills perform a real isolated restore followed by built-in validation.\nSQLite uses a NEW file; PostgreSQL uses a newly created network-isolated Docker\nserver with fresh credentials. MySQL and MongoDB also use newly created isolated Docker servers.", Args: noPositionalArgs, Example: "  dbvault recovery drill --help"}
	var drill app.DrillOptions
	var timeout time.Duration
	var latest bool
	c := &cobra.Command{Use: "drill --target <backup-name> --recovery-database <new-target>", Short: "Restore a backup into an isolated target",
		Long:    "Restore the backup name returned by list into a NEW SQLite file or a NEW\nPostgreSQL Docker server. SQLite requires an existing private parent directory;\nexisting targets, links, source aliases and sidecars are refused. PostgreSQL\nrequires Docker and a preloaded postgres:<source-major>-bookworm image. It uses\nno external network, published ports, host binds or source credentials.\n\n--confirm authorizes creating and restoring the isolated target. --dry-run\nverifies/preflights without creating it and is not successful recovery evidence;\nPostgreSQL dry-run checks the image only, not a live server. Targets are preserved\nby default and on failure/cancellation. --cleanup removes only this run's target\nand its anonymous Docker volumes after successful validation and ownership checks.\nResults are saved as separate .recovery.json objects in backup storage.\nMySQL requires mysql:<source-series>; MongoDB requires mongo:<source-major.minor>.",
		Example: "  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/new.sqlite --dry-run\n  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/new.sqlite --confirm\n  dbvault recovery drill --target backup.sqlite.gz --recovery-database ./recovery/another.sqlite --confirm --cleanup --output json",
		Args:    noPositionalArgs, PreRunE: func(*cobra.Command, []string) error {
			if (drill.Target == "" && !latest) || (drill.Target != "" && latest) || drill.RecoveryDatabase == "" {
				return fmt.Errorf("specify --target or --latest, and --recovery-database")
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

			adapter, err := databaseAdapter(cfg, "", o.redactor)
			if err != nil {
				return err
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return o.redactor.Error(fault.Wrap(fault.Storage, "initialize recovery storage", err))
			}
			defer closeStore()
			if latest {
				svc := &app.Service{Config: cfg, Store: store}
				items, e := svc.List(ctx, "")
				if e != nil {
					return e
				}
				for _, m := range items {
					if m.Database.Engine == cfg.Database.Type && m.Database.Name == cfg.Database.Database {
						drill.Target = m.Name
						break
					}
				}
				if drill.Target == "" {
					return fmt.Errorf("no matching completed backup")
				}
			}
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
			if cfg.Notifications.Slack.Enabled {
				hook := os.Getenv(cfg.Notifications.Slack.WebhookEnv)
				if hook == "" {
					return fmt.Errorf("Slack webhook environment variable is missing")
				}
				svc.Notifier = &notify.Slack{URL: hook, Channel: cfg.Notifications.Slack.Channel, Redactor: o.redactor}
			}
			result, checkErr := svc.RecoveryDrill(ctx, drill)
			stop()
			if err := o.output(c, result); err != nil {
				return err
			}
			return o.redactor.Error(checkErr)
		},
	}
	c.Flags().BoolVar(&latest, "latest", false, "Select the latest completed backup matching configured engine/database")
	c.Flags().StringVarP(&drill.Target, "target", "t", "", targetHelp)
	c.Flags().StringVar(&drill.RecoveryDatabase, "recovery-database", "", "NEW SQLite file or lowercase database name in a new isolated server container")
	c.Flags().BoolVar(&drill.Confirm, "confirm", false, "Authorize creating and restoring the isolated target")
	c.Flags().BoolVar(&drill.DryRun, "dry-run", false, "Verify and preflight without creating or restoring the target")
	c.Flags().BoolVar(&drill.Cleanup, "cleanup", false, "Remove this run's owned target only after successful validation")
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
