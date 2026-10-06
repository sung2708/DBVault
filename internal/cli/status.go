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

func (o *options) statusCommand(now func() time.Time) *cobra.Command {
	var timeout time.Duration
	var statePath string
	c := &cobra.Command{
		Use: "status", GroupID: "operations", Short: "Show current backup protection evidence",
		Long:    "Summarize existing backup metadata, health and recovery evidence, storage, and saved schedules for the configured database. Status does not connect to the database, hash backup contents, perform a restore, or run a recovery drill. Integrity comes from recorded stored-artifact verification evidence or an immediate size check; a checksum in the manifest alone is not verification. Saved schedules do not prove that a scheduler process is running.",
		Example: "  dbvault status\n  dbvault status --output json\n  dbvault status --state .dbvault-schedules.json --timeout 1m",
		Args:    noPositionalArgs,
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
			state, stateErr := schedule.Load(statePath)
			var scheduleDefs []app.HealthSchedule
			scheduleNote := ""
			if stateErr != nil {
				scheduleNote = "Saved schedule state unavailable"
			} else {
				scheduleDefs, scheduleNote = matchingStatusSchedules(state, o.configPath)
			}
			store, closeStore, err := providers.Open(ctx, cfg.Storage)
			if err != nil {
				return o.redactor.Error(fault.Wrap(fault.Storage, "initialize status storage", err))
			}
			defer closeStore()
			svc := &app.Service{Config: cfg, Store: store}
			// Both operations use the existing bounded metadata/health APIs. No
			// archive contents are opened by this read model.
			health, healthErr := svc.Health(ctx, false)
			clock := now
			if clock == nil {
				clock = time.Now
			}
			result := svc.BuildStatus(health, scheduleDefs, scheduleNote, clock())
			if err := o.output(c, result); err != nil {
				return err
			}
			if healthErr != nil {
				return o.redactor.Error(healthErr)
			}
			return nil
		},
	}
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall status deadline")
	c.Flags().StringVar(&statePath, "state", ".dbvault-schedules.json", "Saved schedule definitions (does not check daemon liveness)")
	return c
}

func matchingStatusSchedules(state schedule.State, configPath string) ([]app.HealthSchedule, string) {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return nil, "Saved schedule state unavailable"
	}
	var result []app.HealthSchedule
	for _, job := range state.Jobs {
		path, err := filepath.Abs(job.Config)
		if err != nil {
			return nil, "Saved schedule state unavailable"
		}
		matches := path == abs
		if runtime.GOOS == "windows" {
			matches = strings.EqualFold(path, abs)
		}
		if matches {
			result = append(result, app.HealthSchedule{ID: job.ID, Cron: job.Cron, Enabled: job.Enabled, Operation: job.Operation})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	note := ""
	if len(result) > 0 {
		note = "Saved definitions do not prove the scheduler process is running"
	}
	return result, note
}
