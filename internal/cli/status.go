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
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) statusCommand(now func() time.Time) *cobra.Command {
	var timeout, interval time.Duration
	var statePath string
	var watch bool
	c := &cobra.Command{
		Use: "status", GroupID: "operations", Short: "Show current backup protection evidence",
		Long:    "Summarize existing backup metadata, health and recovery evidence, storage, and saved schedules for the configured database. Status does not connect to the database, hash backup contents, perform a restore, or run a recovery drill. Integrity comes from recorded stored-artifact verification evidence or an immediate size check; a checksum in the manifest alone is not verification. Saved schedules do not prove that a scheduler process is running.\n\nWith --watch, status refreshes in place every --interval until Ctrl+C. Each refresh re-reads metadata and evidence from storage, so remote storage incurs list and read requests per refresh. Watch needs an interactive terminal and human output.",
		Example: "  dbvault status\n  dbvault status --output json\n  dbvault status --state .dbvault-schedules.json --timeout 1m\n  dbvault status --watch --interval 1m",
		Args:    noPositionalArgs,
		PreRunE: func(c *cobra.Command, _ []string) error {
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			if c.Flags().Changed("interval") && !watch {
				return fmt.Errorf("--interval requires --watch")
			}
			if watch && interval < 5*time.Second {
				return fmt.Errorf("--interval must be at least 5s")
			}
			if watch && !presentation.NewLiveView(c.OutOrStdout(), displayOptions(c, nil)).Supported() {
				return fmt.Errorf("--watch needs an interactive terminal with cursor controls and human output; omit --output json and --quiet, or run status without --watch")
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			clock := now
			if clock == nil {
				clock = time.Now
			}
			if watch {
				return o.watchStatus(c, statePath, timeout, interval, clock)
			}
			ctx, cancel := context.WithTimeout(c.Context(), timeout)
			defer cancel()
			result, healthErr, err := o.collectStatus(ctx, c, statePath, clock, nil)
			if err != nil {
				return err
			}
			if err := o.output(c, result); err != nil {
				return err
			}
			if healthErr != nil {
				return o.redactor.Error(healthErr)
			}
			return nil
		},
	}
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall status deadline (per refresh with --watch)")
	c.Flags().StringVar(&statePath, "state", ".dbvault-schedules.json", "Saved schedule definitions (does not check daemon liveness)")
	c.Flags().BoolVar(&watch, "watch", false, "Refresh the status in place until Ctrl+C (interactive terminals only)")
	c.Flags().DurationVar(&interval, "interval", 30*time.Second, "Refresh interval for --watch (minimum 5s)")
	return c
}

// collectStatus reads the configuration, saved schedules and storage evidence
// once. healthErr is a health evaluation failure reported with a partial result.
func (o *options) collectStatus(ctx context.Context, c *cobra.Command, statePath string, clock func() time.Time, cache *app.EvidenceCache) (result app.StatusResult, healthErr, err error) {
	cfg, err := o.load(c)
	if err != nil {
		return result, nil, err
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
		return result, nil, o.redactor.Error(fault.Wrap(fault.Storage, "initialize status storage", err))
	}
	defer closeStore()
	svc := &app.Service{Config: cfg, Store: store}
	// Both operations use the existing bounded metadata/health APIs. No
	// archive contents are opened by this read model.
	health, healthErr := svc.HealthWithEvidenceCache(ctx, false, cache)
	return svc.BuildStatus(health, scheduleDefs, scheduleNote, clock()), healthErr, nil
}

// watchStatus redraws the status in place. Refresh failures are shown in the
// footer and retried at the next interval; Ctrl+C ends the watch successfully.
func (o *options) watchStatus(c *cobra.Command, statePath string, timeout, interval time.Duration, clock func() time.Time) error {
	view := presentation.NewLiveView(c.OutOrStdout(), displayOptions(c, o.redactor))
	ctx := c.Context()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	cache := &app.EvidenceCache{}
	frame := ""
	for {
		refresh, cancel := context.WithTimeout(ctx, timeout)
		result, healthErr, err := o.collectStatus(refresh, c, statePath, clock, cache)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		footer := ""
		stamp := clock().UTC().Format("15:04:05") + " UTC"
		if err == nil && (healthErr == nil || frame == "") {
			var next string
			next, err = view.Render(c.Name(), result)
			if err == nil {
				frame = next
			}
		}
		switch {
		case err != nil:
			footer = "Refresh failed at " + stamp + ": " + err.Error() + " · retrying every " + interval.String() + " · Ctrl+C to stop"
		case healthErr != nil:
			footer = "Health check failed at " + stamp + ": " + healthErr.Error() + " · every " + interval.String() + " · Ctrl+C to stop"
		default:
			footer = "Updated " + stamp + " · every " + interval.String() + " · Ctrl+C to stop"
		}
		view.Draw(frame + view.Footer(footer))
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
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
