package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/schedule"
	"path/filepath"
	"time"
)

func (o *options) scheduleCommand() *cobra.Command {
	var statePath, expression, id string
	daemon := &cobra.Command{Use: "schedule", Short: "Manage schedules and run scheduled backups", GroupID: "operations", Args: noPositionalArgs,
		Long:    "Manage saved backup schedules with add, list, enable, disable and remove.\nRun schedule without a subcommand to start the foreground daemon for saved jobs;\n--cron instead runs one schedule using --config without saving it.\n\nCron has five fields (minute hour day month weekday), using UTC unless CRON_TZ\nis specified. Keep the daemon running; Ctrl+C stops it and cancels active work.\nJobs skip overlaps. Restart the daemon after changing saved definitions.",
		Example: "  dbvault schedule add --id nightly --cron \"0 2 * * *\" --config production.yaml\n  dbvault schedule list\n  dbvault schedule\n  dbvault schedule --cron \"CRON_TZ=Asia/Bangkok 0 2 * * *\" --config production.yaml"}
	daemon.SuggestionsMinimumDistance = 2
	daemon.PersistentFlags().StringVar(&statePath, "state", ".dbvault-schedules.json", "JSON file for saved schedules (relative to current directory)")
	daemon.Flags().StringVar(&expression, "cron", "", "Five-field cron expression; UTC by default, supports CRON_TZ")
	execute := func(c *cobra.Command, jobs []schedule.Job) error {
		if len(jobs) == 0 {
			return fmt.Errorf("no schedules configured; use schedule add or --cron")
		}
		for _, job := range jobs {
			if e := schedule.Validate(job); e != nil {
				return e
			}
		}
		enabled := 0
		for _, job := range jobs {
			if job.Enabled {
				enabled++
			}
		}
		display := o.display(c)
		display.Status("active", fmt.Sprintf("Starting foreground scheduler: %d enabled jobs. Ctrl+C stops active work.", enabled))
		return schedule.Run(c.Context(), jobs, func(ctx context.Context, j schedule.Job) error {
			return o.executeScheduledJob(c, ctx, j)
		}, func(r schedule.Result) {
			if jsonMode(c) && (r.Skipped || r.Err != nil) {
				status := "failed"
				if r.Skipped {
					status = "skipped"
				}
				json.NewEncoder(c.ErrOrStderr()).Encode(map[string]string{"operation": "schedule", "schedule_id": r.ID, "status": status})
				return
			}
			if r.Skipped {
				display.Warning(fmt.Sprintf("Schedule %s skipped: another backup is running", r.ID))
			} else if r.Err != nil {
				display.Error("schedule "+r.ID, c.UseLine(), "dbvault schedule --help", r.Err)
				display.Warning("Retry occurs at the next scheduled time")
			}
		})
	}
	daemon.RunE = func(c *cobra.Command, _ []string) error {
		if expression != "" {
			if _, e := o.load(c); e != nil {
				return e
			}
			abs, e := filepath.Abs(o.configPath)
			if e != nil {
				return e
			}
			return execute(c, []schedule.Job{{ID: "foreground", Cron: expression, Config: abs, Enabled: true}})
		}
		s, e := schedule.Load(statePath)
		if e != nil {
			return e
		}
		return execute(c, s.Jobs)
	}
	for _, action := range []string{"add", "list", "remove", "enable", "disable"} {
		a := action
		cmd := &cobra.Command{Use: a, Args: noPositionalArgs}
		applyScheduleHelp(cmd)
		if a != "list" {
			cmd.Flags().StringVar(&id, "id", "", "Required schedule ID (1-64 letters, digits, underscores or hyphens)")
			cmd.MarkFlagRequired("id")
		}
		var cronExpr, operation, backupType, recoveryDir string
		var confirmRecovery bool
		var baseEvery string
		var cleanup bool
		if a == "add" {
			cmd.Flags().StringVar(&cronExpr, "cron", "", "Required five-field cron expression (UTC; supports CRON_TZ)")
			cmd.MarkFlagRequired("cron")
			cmd.Flags().StringVar(&operation, "operation", "backup", "Schedule backup, pitr (native baseline/log capture), or recovery")
			cmd.Flags().StringVar(&baseEvery, "base-every", "", "PITR baseline refresh interval; default uses separate full jobs")
			cmd.Flags().BoolVar(&cleanup, "cleanup", false, "PITR: delete expired whole chains after each successful backup")
			cmd.Flags().StringVar(&backupType, "type", "", "Backup type: full or incremental")
			cmd.Flags().StringVar(&recoveryDir, "recovery-dir", "", "Existing private directory for NEW SQLite drill files")
			cmd.Flags().BoolVar(&confirmRecovery, "confirm", false, "Authorize recurring isolated recovery drills")
		}
		cmd.RunE = func(c *cobra.Command, _ []string) error {
			if a == "list" {
				s, e := schedule.Load(statePath)
				if e != nil {
					return e
				}
				return o.output(c, s)
			}
			var job schedule.Job
			if a == "add" {
				cfg, e := o.load(c)
				if e != nil {
					return e
				}
				if operation == "recovery" {
					if !confirmRecovery {
						return fmt.Errorf("--confirm is required to authorize recurring recovery drills")
					}
					if cfg.Database.Type == "sqlite" && recoveryDir == "" {
						return fmt.Errorf("SQLite recovery schedule requires --recovery-dir")
					}
					if recoveryDir != "" {
						recoveryDir, e = filepath.Abs(recoveryDir)
						if e != nil {
							return e
						}
					}
				}
				if operation == "pitr" && (cfg.PITR == nil || (cfg.Database.Type != "postgres" && cfg.Database.Type != "mysql" && cfg.Database.Type != "mongodb")) {
					return fmt.Errorf("native schedule requires pitr configuration and PostgreSQL, MySQL or MongoDB")
				}
				abs, e := filepath.Abs(o.configPath)
				if e != nil {
					return e
				}
				job = schedule.Job{ID: id, Cron: cronExpr, Config: abs, Enabled: true, Operation: operation, BackupType: backupType, RecoveryDirectory: recoveryDir, BaseEvery: baseEvery, Cleanup: cleanup}
				if e = schedule.Validate(job); e != nil {
					return e
				}
			}
			e := schedule.Update(statePath, func(s *schedule.State) error {
				found := -1
				for i, j := range s.Jobs {
					if j.ID == id {
						found = i
						break
					}
				}
				if a == "add" {
					if found >= 0 {
						return fmt.Errorf("schedule already exists")
					}
					s.Jobs = append(s.Jobs, job)
					return nil
				}
				if found < 0 {
					return fmt.Errorf("schedule does not exist")
				}
				if a == "remove" {
					s.Jobs = append(s.Jobs[:found], s.Jobs[found+1:]...)
				} else {
					s.Jobs[found].Enabled = a == "enable"
				}
				return nil
			})
			if e != nil {
				return e
			}
			return o.output(c, map[string]string{"schedule": id, "action": a})
		}
		daemon.AddCommand(cmd)
	}
	return daemon
}

func (o *options) executeScheduledJob(c *cobra.Command, ctx context.Context, j schedule.Job) error {
	jobCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	// Reuse the same command/service pipeline and reload credentials each run.
	command := New(o.build, c.OutOrStdout(), c.ErrOrStderr())
	command.Annotations = map[string]string{"dbvault.scheduled": "true"}
	args := []string{"backup", "--config", j.Config}
	if j.BackupType != "" {
		args = append(args, "--type", j.BackupType)
	}
	if j.Operation == "pitr" {
		args = []string{"pitr", "backup", "--config", j.Config}
		if j.BackupType != "" {
			args = append(args, "--type", j.BackupType)
		}
		if j.BaseEvery != "" {
			args = append(args, "--base-every", j.BaseEvery)
		}
		if j.Cleanup {
			args = append(args, "--cleanup")
		}
	}
	if j.Operation == "recovery" {
		cfg, e := config.Load(j.Config, config.Overrides{})
		if e != nil {
			return e
		}
		destination, e := app.NewDestination(cfg.Database.Type, cfg.Database.Database, time.Now())
		if e != nil {
			return e
		}
		if cfg.Database.Type == "sqlite" {
			if j.RecoveryDirectory == "" {
				return fmt.Errorf("SQLite recovery schedule requires --recovery-dir")
			}
			destination = filepath.Join(j.RecoveryDirectory, filepath.Base(destination))
		}
		args = []string{"recovery", "drill", "--config", j.Config, "--latest", "--recovery-database", destination, "--confirm", "--cleanup"}
	}
	if jsonMode(c) {
		args = append(args, "--output", "json")
	}
	if globalBool(c, "quiet") {
		args = append(args, "--quiet")
	}
	if globalBool(c, "no-color") {
		args = append(args, "--no-color")
	}
	if o.verbose {
		args = append(args, "--verbose")
	}
	command.SetArgs(args)
	return command.ExecuteContext(jobCtx)
}
