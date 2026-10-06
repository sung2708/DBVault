package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

// Preserve error categories/exit codes without echoing provider diagnostics or
// invalid configuration values into an unattended monitor's logs.
type healthOperationError struct {
	cause  error
	reason string
}

func (e *healthOperationError) Error() string { return e.reason }
func (e *healthOperationError) Unwrap() error { return e.cause }

func (o *options) healthCommand(now func() time.Time) *cobra.Command {
	var verify bool
	var timeout time.Duration
	var statePath string
	c := &cobra.Command{Use: "health", GroupID: "operations", Short: "Check backup freshness and available integrity evidence",
		Long:    "Check the latest completed backup using storage, without contacting the database.\nSet health.max_backup_age in YAML to define freshness. Default scope is logical;\nhealth.backup_scope: pitr requires health.source_identity from pitr list.\nNative freshness measures archived coverage and checks all chain artifacts.\nDefault checks read metadata and availability/size; unknown checksum evidence\nreturns warning. --verify reads stored bytes (the entire native chain for PITR);\nlogical verification writes evidence, native verification is read-only.\nExit 0 means healthy; warning, critical and unknown return 1. Integrity errors\nreturn 4; timeout/cancellation returns 5. JSON runtime failures also emit a report.\nFor a monitor on another host, use shared storage and --state \"\" to skip local\nschedule definitions. Definitions never prove scheduler liveness.\nNo database connection or Slack delivery occurs.",
		Example: "  dbvault health\n  dbvault health --verify\n  dbvault health --config monitor.yaml --state \"\" --output json\n  dbvault health --quiet", Args: noPositionalArgs,
		PreRunE: func(*cobra.Command, []string) error {
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			failure := func(reason string, checkErr error, cfg *config.Config) error {
				report := app.HealthReport{Status: app.Unknown, Reason: reason, Databases: []app.DatabaseHealth{}}
				if cfg != nil {
					report.Databases = append(report.Databases, app.DatabaseHealth{Name: cfg.Database.Database, Engine: cfg.Database.Type, BackupScope: cfg.Health.BackupScope, SourceIdentity: cfg.Health.SourceIdentity, Status: app.Unknown, Reason: reason, Integrity: "unknown", RestoreTest: "unknown"})
				}
				if err := o.output(c, report); err != nil {
					return err
				}
				return &healthOperationError{cause: checkErr, reason: reason}
			}
			cfg, err := o.load(c)
			if err != nil {
				return failure("Health configuration unavailable or invalid", err, nil)
			}
			state := schedule.State{}
			if statePath != "" {
				state, err = schedule.Load(statePath)
				if err != nil {
					return failure("Saved schedule state unavailable or invalid", fault.Wrap(fault.Configuration, "health schedule state", err), &cfg)
				}
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return failure("Health storage unavailable", fault.Wrap(fault.Storage, "initialize health storage", err), &cfg)
			}
			defer closeStore()
			svc := &app.Service{Config: cfg, Store: store, Now: now}
			report, checkErr := svc.Health(ctx, verify)
			if err := healthSchedules(&report, state, o.configPath); err != nil {
				return failure("Saved schedule definitions could not be evaluated", err, &cfg)
			}
			if err := o.output(c, report); err != nil {
				return err
			}
			if checkErr != nil {
				return &healthOperationError{cause: checkErr, reason: report.Databases[0].Reason}
			}
			if report.Status != app.Healthy {
				return &app.HealthFailure{Status: report.Status}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&verify, "verify", false, "Read and verify the latest artifact's size and SHA-256")
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall health-check deadline; increase for active verification")
	c.Flags().StringVar(&statePath, "state", ".dbvault-schedules.json", "Advisory schedule definitions; empty skips them (never checks daemon liveness)")
	return c
}

func healthSchedules(report *app.HealthReport, state schedule.State, configPath string) error {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	h := &report.Databases[0]
	anyEnabled := false
	anyBackup := false
	for _, job := range state.Jobs {
		path, err := filepath.Abs(job.Config)
		if err != nil {
			return err
		}
		matches := path == abs
		if runtime.GOOS == "windows" {
			matches = strings.EqualFold(path, abs)
		}
		if !matches {
			continue
		}
		h.Schedules = append(h.Schedules, app.HealthSchedule{ID: job.ID, Cron: job.Cron, Enabled: job.Enabled, Operation: job.Operation})
		if (h.BackupScope == "pitr" && job.Operation == "pitr") || (h.BackupScope != "pitr" && (job.Operation == "" || job.Operation == "backup")) {
			anyBackup = true
			anyEnabled = anyEnabled || job.Enabled
		}
	}
	sort.Slice(h.Schedules, func(i, j int) bool { return h.Schedules[i].ID < h.Schedules[j].ID })
	if len(h.Schedules) > 0 {
		h.ScheduleNote = "Saved schedules do not prove the foreground daemon is running; missed runs are not replayed"
		if anyBackup && !anyEnabled && h.MaxAgeSeconds != nil && h.Status == app.Healthy {
			h.Status = app.Warning
			h.Reason = "Backup is fresh and verified, but all matching saved schedules are disabled"
			report.Status = h.Status
		}
	}
	return nil
}
