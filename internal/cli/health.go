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
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) healthCommand(now func() time.Time) *cobra.Command {
	var verify bool
	var timeout time.Duration
	var statePath string
	c := &cobra.Command{Use: "health", GroupID: "operations", Short: "Check backup freshness and available integrity evidence",
		Long:    "Check the latest registered full backup for the configured database.\nSet health.max_backup_age in YAML to define freshness. Default mode reads\nmetadata and artifact availability/size only; verification history is unknown.\n--verify streams the latest archive through the existing size/SHA-256 check\n(and may incur substantial cloud I/O). Integrity does not prove recovery.\nExit 0 means healthy; warning, critical and unknown return 1.\nNo database connection or Slack delivery occurs.",
		Example: "  dbvault health\n  dbvault health --verify\n  dbvault health --output json\n  dbvault health --quiet", Args: noPositionalArgs,
		PreRunE: func(*cobra.Command, []string) error {
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			cfg, err := o.load(c)
			if err != nil {
				return err
			}
			state, err := schedule.Load(statePath)
			if err != nil {
				return o.redactor.Error(fault.Wrap(fault.Configuration, "health schedule state", err))
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return o.redactor.Error(fault.Wrap(fault.Storage, "initialize health storage", err))
			}
			defer closeStore()
			svc := &app.Service{Config: cfg, Store: store, Now: now}
			report, checkErr := svc.Health(ctx, verify)
			if err := healthSchedules(&report, state, o.configPath); err != nil {
				return err
			}
			if err := o.output(c, report); err != nil {
				return err
			}
			if checkErr != nil {
				return o.redactor.Error(checkErr)
			}
			if report.Status != app.Healthy {
				return &app.HealthFailure{Status: report.Status}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&verify, "verify", false, "Read and verify the latest artifact's size and SHA-256")
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall health-check deadline; increase for active verification")
	c.Flags().StringVar(&statePath, "state", ".dbvault-schedules.json", "Saved schedule definitions (does not check daemon liveness)")
	return c
}

func healthSchedules(report *app.HealthReport, state schedule.State, configPath string) error {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	h := &report.Databases[0]
	anyEnabled := false
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
		h.Schedules = append(h.Schedules, app.HealthSchedule{ID: job.ID, Cron: job.Cron, Enabled: job.Enabled})
		anyEnabled = anyEnabled || job.Enabled
	}
	sort.Slice(h.Schedules, func(i, j int) bool { return h.Schedules[i].ID < h.Schedules[j].ID })
	if len(h.Schedules) > 0 {
		h.ScheduleNote = "Saved schedules do not prove the foreground daemon is running; missed runs are not replayed"
		if !anyEnabled && h.MaxAgeSeconds != nil && h.Status == app.Healthy {
			h.Status = app.Warning
			h.Reason = "Backup is fresh and verified, but all matching saved schedules are disabled"
			report.Status = h.Status
		}
	}
	return nil
}
