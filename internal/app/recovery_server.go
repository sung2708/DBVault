package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/recoveryserver"
)

func (s *Service) serverRecoveryDrill(ctx context.Context, o DrillOptions, native runner.Runner) (r DrillResult, resultErr error) {
	r = DrillResult{Version: 1, Status: "failed", BackupName: o.Target, Engine: s.Config.Database.Type, StartedAt: s.now(), TargetState: "not_created", DryRun: o.DryRun,
		Stages: []DrillStage{{"isolation", "pending"}, {"metadata", "pending"}, {"integrity", "pending"}, {"compatibility", "pending"}, {"restore", "pending"}, {"validation", "pending"}, {"cleanup", "not_requested"}, {"record", "pending"}}}
	stage := func(name, status string) {
		for i := range r.Stages {
			if r.Stages[i].Name == name {
				r.Stages[i].Status = status
			}
		}
	}
	var target *recoveryserver.Target
	defer func() {
		if target != nil && target.ID != "" && r.TargetState != "removed" {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			stopErr := target.Stop(stopCtx)
			cancel()
			if stopErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("stop retained recovery container: %w", stopErr))
			}
		}
		r.CompletedAt = s.now()
		r.DurationSeconds = max(0, r.CompletedAt.Sub(r.StartedAt).Seconds())
		if target != nil && target.ID != "" {
			r.RecoveryTarget = target.ID
			if r.TargetState == "not_created" {
				r.TargetState = "preserved"
			}
		}
		if resultErr != nil {
			r.Status = "failed"
			if errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
				r.Status = "cancelled"
			}
			for i := range r.Stages {
				if r.Stages[i].Status == "running" {
					r.Stages[i].Status = "failed"
				}
				if r.Stages[i].Status == "pending" {
					r.Stages[i].Status = "skipped"
				}
			}
		}
		if !o.DryRun && r.BackupID != "" {
			stage("record", "passed")
			if err := s.recordDrill(context.WithoutCancel(ctx), &r, o.Redact); err != nil {
				stage("record", "failed")
				r.RecordKey = ""
				if r.Status == "passed" {
					r.Status = "failed"
				}
				resultErr = errors.Join(resultErr, fault.Wrap(fault.Storage, "record recovery drill", err))
			}
		} else {
			stage("record", "skipped")
		}
	}()
	stage("isolation", "running")
	if o.Target == "" || o.RecoveryDatabase == "" {
		return r, fmt.Errorf("--target and --recovery-database are required")
	}
	if !o.DryRun && !o.Confirm {
		return r, fmt.Errorf("--confirm is required to create and restore the isolated recovery target")
	}
	if o.DryRun && o.Cleanup {
		return r, fmt.Errorf("--cleanup cannot be combined with --dry-run")
	}
	var err error
	target, err = recoveryserver.New(s.Config.Database.Type, o.RecoveryDatabase, s.Config.Database.Database, native)
	if err != nil {
		return r, err
	}
	work := *s
	work.Notifier = nil
	work.DB = target
	work.Config.Database = target.Config
	work.Observe = func(e Event) {
		name := map[string]string{"manifest": "metadata", "restore.verify": "integrity", "restore.compatibility": "compatibility", "restore.write": "restore"}[e.Stage]
		if name != "" {
			if e.State == "start" {
				stage(name, "running")
			}
			if e.State == "complete" {
				stage(name, "passed")
			}
		}
		if e.Stage == "manifest" && e.State == "complete" {
			r.BackupID, r.Checksum, r.SourceDatabase = e.Manifest.ID, e.Manifest.Checksum.Hash, e.Manifest.Database.Name
		}
		if s.Observe != nil {
			s.Observe(e)
		}
	}
	prepare := func(ctx context.Context, m metadata.Manifest) error {
		if m.Database.Name == o.RecoveryDatabase {
			return fmt.Errorf("recovery database matches the manifest source")
		}
		stage("compatibility", "running")
		if err := target.CheckImage(ctx, m.Database.Version, m.ToolVersion); err != nil {
			return fault.Wrap(fault.Dependency, "server recovery image", err)
		}
		if !o.DryRun {
			if err := target.Start(ctx); err != nil {
				return err
			}
			r.RecoveryTarget, r.TargetState = target.ID, "preserved"
		}
		stage("isolation", "passed")
		return nil
	}
	if o.DryRun {
		m, snapshot, err := work.snapshot(ctx, o.Target)
		if err != nil {
			return r, err
		}
		defer func() { snapshot.Close(); os.Remove(snapshot.Name()) }()
		if m.Database.Engine != s.Config.Database.Type || m.Database.Format != target.Format() {
			return r, fault.Wrap(fault.Unsupported, "recovery compatibility", fmt.Errorf("backup engine or format differs from server"))
		}
		payload, e := work.materializeSnapshot(ctx, m, snapshot, 0)
		if e != nil {
			return r, e
		}
		removeTemp(payload)
		if err := prepare(ctx, m); err != nil {
			return r, err
		}
		stage("compatibility", "skipped") // No server is created/probed in a dry-run.
		stage("restore", "skipped")
		stage("validation", "skipped")
		r.Status = "preflight_passed"
		return r, nil
	}
	if err := work.restore(ctx, o.Target, true, false, database.RestoreOptions{}, prepare, func() error { return target.Check(ctx) }); err != nil {
		return r, err
	}
	stage("validation", "running")
	s.stage("recovery.validation", "start")
	validation, err := target.ValidateRecovery(ctx)
	if err != nil {
		return r, err
	}
	r.Validation = &validation
	stage("validation", "passed")
	s.stage("recovery.validation", "complete")
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if o.Cleanup {
		stage("cleanup", "running")
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := target.Cleanup(cleanupCtx); err != nil {
			return r, err
		}
		r.TargetState = "removed"
		stage("cleanup", "passed")
	}
	r.Status = "passed"
	return r, nil
}
