package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/config"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/pitr"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) pitrCommand() *cobra.Command {
	root := &cobra.Command{Use: "pitr", Short: "Native log backups and point-in-time recovery", Long: "Create instance-wide native baselines, capture only WAL/binlog/oplog changes, and replay a verified continuous chain to an exclusive UTC timestamp. Native archives use a separate namespace from logical dump backups. Configure pitr and the engine prerequisites first; see docs/pitr.md.", Example: "  dbvault pitr base\n  dbvault pitr capture --parent pitr_ID.tar.enc\n  dbvault pitr list", GroupID: "operations", Args: noPositionalArgs}
	for _, op := range []string{"base", "backup", "capture", "list", "cleanup", "restore"} {
		command := &cobra.Command{Use: op, Short: map[string]string{"base": "Create a full native baseline", "backup": "Create a baseline or capture logs from the latest source chain", "capture": "Capture new engine logs since a parent", "list": "List native baselines and captured ranges", "cleanup": "Remove expired whole native chains using retention policies", "restore": "Restore a verified chain to an exclusive timestamp"}[op], Long: "Use native engine data and log coordinates. Capture requires the previous cursor to remain available; gaps, rollback and identity changes fail closed. Restore authenticates all archives first and requires a fresh target. PostgreSQL prepares an offline data directory; MySQL/MongoDB replay into an independently configured empty instance.", Example: "  dbvault pitr " + op + " --help", Args: noPositionalArgs}
		var parent, target, at, directory, targetConfig string
		var confirm bool
		var timeout time.Duration
		var backupType string
		var baseEvery time.Duration
		var cleanup, dry bool
		if op == "backup" {
			command.Flags().StringVar(&backupType, "type", "incremental", "full creates a baseline; incremental starts a baseline if needed, otherwise captures logs")
			command.Flags().DurationVar(&baseEvery, "base-every", 0, "Refresh the baseline after this interval; 0 requires a separate full backup job")
			command.Flags().BoolVar(&cleanup, "cleanup", false, "After successful backup, delete expired whole chains using retention policies")
		}
		if op == "cleanup" {
			command.Flags().BoolVar(&dry, "dry-run", false, "Preview expired chains without deleting archives")
		}
		command.Flags().DurationVar(&timeout, "timeout", 2*time.Hour, "Operation deadline")
		if op == "capture" {
			command.Flags().StringVar(&parent, "parent", "", "Previous native base or captured range")
		}
		if op == "restore" {
			command.Flags().StringVar(&target, "target", "", "Last captured native range")
			command.Flags().StringVar(&at, "time", "", "Exclusive target timestamp, RFC3339, whole seconds")
			command.Flags().StringVar(&directory, "directory", "", "New offline PostgreSQL data directory")
			command.Flags().StringVar(&targetConfig, "target-config", "", "Configuration for a fresh MySQL/MongoDB target instance")
			command.Flags().BoolVar(&confirm, "confirm", false, "Authorize mutation of the fresh recovery target")
		}
		command.RunE = func(c *cobra.Command, _ []string) (err error) {
			defer func() { err = o.redactor.Error(err) }()
			if timeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			cfg, err := o.load(c)
			if err != nil {
				return err
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return err
			}
			defer closeStore()
			svc := &pitr.Service{Config: cfg, Store: store, Runner: runner.Native{Redactor: o.redactor}}
			if op == "base" || op == "capture" || op == "backup" {
				svc.Password, err = cfg.Password()
				if err != nil {
					return err
				}
			}
			switch op {
			case "backup":
				m, e := svc.Backup(ctx, backupType, baseEvery)
				if e != nil {
					return e
				}
				if e = o.output(c, m); e != nil {
					return e
				}
				if cleanup {
					_, e = svc.Cleanup(ctx, time.Now(), false)
					if e != nil {
						return fmt.Errorf("native backup published, but cleanup failed: %w", e)
					}
				}
				return nil
			case "cleanup":
				m, e := svc.Cleanup(ctx, time.Now(), dry)
				if e != nil {
					return e
				}
				return o.output(c, m)
			case "base":
				m, e := svc.Base(ctx)
				if e != nil {
					return e
				}
				return o.output(c, m)
			case "capture":
				if parent == "" {
					return fmt.Errorf("--parent is required")
				}
				m, e := svc.Capture(ctx, parent)
				if e != nil {
					return e
				}
				return o.output(c, m)
			case "list":
				m, e := svc.List(ctx)
				if e != nil {
					return e
				}
				return o.output(c, m)
			case "restore":
				if target == "" || at == "" {
					return fmt.Errorf("--target and --time are required")
				}
				when, e := time.Parse(time.RFC3339, at)
				if e != nil {
					return fmt.Errorf("invalid RFC3339 target time")
				}
				var destination *config.Config
				if targetConfig != "" {
					dest, e := config.Load(targetConfig, config.Overrides{})
					if e != nil {
						return e
					}
					destination = &dest
					o.redactor = o.redactor.With(os.Getenv(dest.Database.PasswordEnv))
					svc.Runner = runner.Native{Redactor: o.redactor}
				}
				m, e := svc.Restore(ctx, target, when, directory, destination, confirm)
				if e != nil {
					return e
				}
				return o.output(c, m)
			}
			return fmt.Errorf("unknown PITR operation")
		}
		root.AddCommand(command)
	}
	return root
}
