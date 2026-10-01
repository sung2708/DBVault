package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/sqlite"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/storage"
	"github.com/sung2708/DBVault/internal/storage/local"
)

type DrillOptions struct {
	Target                   string
	RecoveryDatabase         string
	Confirm, DryRun, Cleanup bool
	CheckSpace               func(metadata.Manifest, string) error
	Redact                   func(string) string
}
type DrillStage struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type DrillResult struct {
	Version         int                          `json:"version"`
	Status          string                       `json:"status"`
	BackupName      string                       `json:"backup_name"`
	BackupID        string                       `json:"backup_id,omitempty"`
	Checksum        string                       `json:"checksum,omitempty"`
	SourceDatabase  string                       `json:"source_database,omitempty"`
	Engine          string                       `json:"engine"`
	StartedAt       time.Time                    `json:"started_at"`
	CompletedAt     time.Time                    `json:"completed_at"`
	DurationSeconds float64                      `json:"duration_seconds"`
	RecoveryTarget  string                       `json:"recovery_target,omitempty"`
	TargetState     string                       `json:"target_state"`
	DryRun          bool                         `json:"dry_run"`
	Stages          []DrillStage                 `json:"stages"`
	Validation      *database.RecoveryValidation `json:"validation,omitempty"`
	RecordKey       string                       `json:"record_key,omitempty"`
}

// RecoveryDrill deliberately supports only the adapter whose isolation can be
// enforced locally. Native server restores require a separate security design.
func (s *Service) RecoveryDrill(ctx context.Context, o DrillOptions) (DrillResult, error) {
	cfg := s.Config.Database
	cfg.Database = o.RecoveryDatabase
	return s.recoveryDrill(ctx, o, &sqlite.Adapter{Config: cfg})
}

func (s *Service) recoveryDrill(ctx context.Context, o DrillOptions, target database.Adapter) (r DrillResult, resultErr error) {
	r = DrillResult{Version: 1, Status: "failed", BackupName: o.Target, Engine: s.Config.Database.Type, StartedAt: s.now(), TargetState: "not_created", DryRun: o.DryRun,
		Stages: []DrillStage{{"isolation", "pending"}, {"metadata", "pending"}, {"integrity", "pending"}, {"compatibility", "pending"}, {"restore", "pending"}, {"validation", "pending"}, {"cleanup", "not_requested"}, {"record", "pending"}}}
	stage := func(name, status string) {
		for i := range r.Stages {
			if r.Stages[i].Name == name {
				r.Stages[i].Status = status
				break
			}
		}
	}
	var owned *drillTarget
	defer func() {
		r.CompletedAt = s.now()
		r.DurationSeconds = max(0, r.CompletedAt.Sub(r.StartedAt).Seconds())
		if resultErr != nil {
			if errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
				r.Status = "cancelled"
			} else {
				r.Status = "failed"
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
		if owned != nil {
			owned.root.Close()
		}
		// Dry runs never create success evidence. Failed runs with a known backup
		// identity are useful evidence too; preserve the target for inspection.
		if !o.DryRun && r.BackupID != "" {
			stage("record", "passed")
			if err := s.recordDrill(context.WithoutCancel(ctx), &r, o.Redact); err != nil {
				stage("record", "failed")
				if r.Status == "passed" {
					r.Status = "failed"
				}
				r.RecordKey = ""
				resultErr = errors.Join(resultErr, fault.Wrap(fault.Storage, "record recovery drill", err))
			}
		} else {
			stage("record", "skipped")
		}
	}()
	stage("isolation", "running")
	if s.Config.Database.Type != "sqlite" || target.Name() != "sqlite" || !target.Capabilities().RecoveryDrill {
		return r, fault.Wrap(fault.Unsupported, "recovery drill", fmt.Errorf("safe recovery drills currently support SQLite only; server target isolation is not implemented"))
	}
	validator, ok := target.(database.RecoveryValidator)
	if !ok {
		return r, fault.Wrap(fault.Unsupported, "recovery validation", fmt.Errorf("adapter lacks post-restore validation"))
	}
	if o.Target == "" || o.RecoveryDatabase == "" {
		return r, fmt.Errorf("--target and --recovery-database are required")
	}
	if !o.DryRun && !o.Confirm {
		return r, fmt.Errorf("--confirm is required to create and restore the isolated recovery target")
	}
	if o.DryRun && o.Cleanup {
		return r, fmt.Errorf("--cleanup cannot be combined with --dry-run")
	}
	path, err := isolatedSQLitePath(s.Config.Database.Database, o.RecoveryDatabase)
	if err != nil {
		return r, err
	}
	r.RecoveryTarget = path
	stage("isolation", "passed")
	// The real adapter must use the normalized target that was checked.
	if a, ok := target.(*sqlite.Adapter); ok {
		a.Config.Database = path
	}
	work := *s
	work.Notifier = nil // Drill has its own lifecycle; never send misleading restore notifications.
	work.Config.Database.Database = path
	work.DB = target
	var mu sync.Mutex
	observe := s.Observe
	work.Observe = func(e Event) {
		mu.Lock()
		name := map[string]string{"manifest": "metadata", "restore.verify": "integrity", "preflight": "compatibility", "restore.compatibility": "compatibility", "restore.write": "restore"}[e.Stage]
		if name != "" {
			if e.State == "start" {
				stage(name, "running")
			}
			if e.State == "complete" && e.Stage != "preflight" {
				stage(name, "passed")
			}
		}
		if e.Stage == "manifest" && e.State == "complete" {
			r.BackupID = e.Manifest.ID
			r.Checksum = e.Manifest.Checksum.Hash
			r.SourceDatabase = e.Manifest.Database.Name
		}
		mu.Unlock()
		if observe != nil {
			observe(e)
		}
	}
	if o.DryRun {
		// New targets use the embedded engine; preflight an in-memory connection
		// so a drill also works when the production file has been lost.
		work.DB = recoveryPreflightAdapter{Adapter: target, preflight: validator.PreflightRecovery}
		err = work.restore(ctx, o.Target, false, true, database.RestoreOptions{}, func(_ context.Context, m metadata.Manifest) error {
			if _, err := isolatedSQLitePath(m.Database.Name, path); err != nil {
				stage("isolation", "failed")
				return err
			}
			if o.CheckSpace != nil {
				return o.CheckSpace(m, path)
			}
			return nil
		}, nil)
		stage("restore", "skipped")
		stage("validation", "skipped")
		if err != nil {
			return r, err
		}
		r.Status = "preflight_passed"
		return r, nil
	}
	err = work.restore(ctx, o.Target, true, false, database.RestoreOptions{}, func(ctx context.Context, m metadata.Manifest) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Protect the manifest source as well as the configured production file.
		if _, err := isolatedSQLitePath(m.Database.Name, path); err != nil {
			stage("isolation", "failed")
			return err
		}
		stage("compatibility", "running")
		info, err := validator.PreflightRecovery(ctx)
		if err != nil {
			return err
		}
		if err := target.Compatible(info, m.Database.Version, m.ToolVersion); err != nil {
			return fault.Wrap(fault.Unsupported, "recovery compatibility", err)
		}
		if o.CheckSpace != nil {
			if err := o.CheckSpace(m, path); err != nil {
				return err
			}
		}
		owned, err = createDrillTarget(path)
		if err != nil {
			stage("isolation", "failed")
			return err
		}
		r.TargetState = "preserved"
		if err := owned.check(); err != nil {
			r.TargetState = "ownership_changed"
			stage("isolation", "failed")
			return err
		}
		return nil
	}, func() error {
		if owned == nil {
			return fmt.Errorf("recovery target was not prepared")
		}
		if err := owned.check(); err != nil {
			r.TargetState = "ownership_changed"
			stage("isolation", "failed")
			return err
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	stage("validation", "running")
	if err := owned.check(); err != nil {
		r.TargetState = "ownership_changed"
		stage("isolation", "failed")
		return r, err
	}
	s.stage("recovery.validation", "start")
	validation, err := validator.ValidateRecovery(ctx)
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
		if err := owned.cleanup(); err != nil {
			if owned.check() != nil {
				r.TargetState = "ownership_changed"
				stage("isolation", "failed")
			}
			return r, err
		}
		r.TargetState = "removed"
		stage("cleanup", "passed")
	}
	r.Status = "passed"
	return r, nil
}

// Existing targets are always refused, including symlinks and hardlinks. The
// parent is resolved before exclusive creation. No equality bypass is offered.
func isolatedSQLitePath(source, target string) (string, error) {
	if source == "" || target == "" || strings.ContainsAny(target, "\x00\r\n") {
		return "", fmt.Errorf("source and recovery paths must be explicit")
	}
	// Windows absolute-path resolution may normalize away trailing dots/spaces.
	// Reject unsafe raw basenames before that normalization can hide an alias.
	if err := local.ValidKey(filepath.Base(target)); err != nil {
		return "", fmt.Errorf("recovery filename must be filesystem-safe: %w", err)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err := local.ValidKey(filepath.Base(abs)); err != nil {
		return "", fmt.Errorf("recovery filename must be filesystem-safe: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("recovery parent directory must already exist: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	src, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(src); err == nil {
		src = real
	} else {
		if !os.IsNotExist(err) {
			return "", err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(src))
		if err != nil {
			return "", fmt.Errorf("source parent cannot be resolved; isolation is ambiguous: %w", err)
		}
		src = filepath.Join(parent, filepath.Base(src))
	}
	equal := filepath.Clean(src) == filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		equal = strings.EqualFold(filepath.Clean(src), filepath.Clean(abs))
	}
	if equal {
		return "", fmt.Errorf("recovery drill target matches the configured production/source target; aborted")
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(abs + suffix); err == nil {
			return "", fmt.Errorf("recovery target or SQLite sidecar already exists; choose a new isolated path")
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return abs, nil
}

type drillTarget struct {
	root     *os.Root
	name     string
	path     string
	identity os.FileInfo
}

func createDrillTarget(path string) (*drillTarget, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		root.Close()
		return nil, err
	}
	identity, err := f.Stat()
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		root.Close()
		return nil, errors.Join(err, closeErr)
	}
	return &drillTarget{root: root, name: name, path: path, identity: identity}, nil
}
func (t *drillTarget) check() error {
	info, err := t.root.Lstat(t.name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(t.identity, info) {
		return fmt.Errorf("recovery target ownership changed; operation aborted")
	}
	current, err := os.Lstat(t.path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(t.identity, current) {
		return fmt.Errorf("recovery path no longer resolves to this run's owned target")
	}
	return nil
}

type recoveryPreflightAdapter struct {
	database.Adapter
	preflight func(context.Context) (database.Info, error)
}

func (a recoveryPreflightAdapter) Preflight(ctx context.Context) (database.Info, error) {
	return a.preflight(ctx)
}
func (t *drillTarget) cleanup() error {
	if err := t.check(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := t.root.Lstat(t.name + suffix); err == nil {
			return fmt.Errorf("SQLite sidecar remains; target preserved for inspection")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return t.root.Remove(t.name)
}

func (s *Service) recordDrill(ctx context.Context, r *DrillResult, redact func(string) string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	r.RecordKey = "recovery_" + hex.EncodeToString(id) + ".recovery.json"
	persisted := *r
	if redact != nil {
		persisted.SourceDatabase = redact(persisted.SourceDatabase)
		persisted.RecoveryTarget = redact(persisted.RecoveryTarget)
		persisted.BackupName = redact(persisted.BackupName)
	}
	b, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	return s.Store.Put(ctx, r.RecordKey, bytes.NewReader(b), int64(len(b)))
}

// DecodeDrill validates evidence without accepting arbitrary/legacy JSON as a
// passed recovery drill. Evidence is operator-controlled, not a signed attestation.
func DecodeDrill(reader io.Reader) (DrillResult, error) {
	var r DrillResult
	b, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return r, fmt.Errorf("unreadable or oversized recovery record")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, fmt.Errorf("invalid recovery record")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return r, fmt.Errorf("trailing or oversized recovery record")
	}
	if r.Version != 1 || r.DryRun || r.BackupID == "" || r.BackupName == "" || r.SourceDatabase == "" || r.Engine != "sqlite" || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.DurationSeconds < 0 {
		return r, fmt.Errorf("invalid recovery evidence")
	}
	hash, err := hex.DecodeString(r.Checksum)
	if err != nil || len(hash) != 32 {
		return r, fmt.Errorf("invalid recovery checksum")
	}
	if r.Status != "passed" && r.Status != "failed" && r.Status != "cancelled" {
		return r, fmt.Errorf("invalid recovery result")
	}
	if err := storage.FlatKey(r.RecordKey); err != nil || !strings.HasSuffix(r.RecordKey, ".recovery.json") {
		return r, fmt.Errorf("invalid recovery record key")
	}
	if r.Status == "passed" {
		if r.TargetState != "preserved" && r.TargetState != "removed" {
			return r, fmt.Errorf("invalid passed target state")
		}
		seen := map[string]bool{}
		cleanupOK := false
		for _, stage := range r.Stages {
			if seen[stage.Name] {
				return r, fmt.Errorf("duplicate recovery stage")
			}
			seen[stage.Name] = true
			if stage.Name == "cleanup" {
				cleanupOK = (r.TargetState == "removed" && stage.Status == "passed") || (r.TargetState == "preserved" && stage.Status == "not_requested")
			}
		}
		if !cleanupOK {
			return r, fmt.Errorf("invalid passed cleanup evidence")
		}
		if r.Validation == nil || r.Validation.Method != "SQLite PRAGMA integrity_check and sqlite_schema query" || r.Validation.Objects < 0 {
			return r, fmt.Errorf("missing recovery validation evidence")
		}
		for _, name := range []string{"isolation", "metadata", "integrity", "compatibility", "restore", "validation", "record"} {
			found := false
			for _, stage := range r.Stages {
				if stage.Name == name && stage.Status == "passed" {
					found = true
				}
			}
			if !found {
				return r, fmt.Errorf("incomplete passed recovery stages")
			}
		}
	}
	return r, nil
}
