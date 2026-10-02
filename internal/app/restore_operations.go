package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sung2708/DBVault/internal/compression"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/pipeline"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type RestoreRequest struct {
	NewDatabase  bool
	BackupBefore bool
	Options      database.RestoreOptions
}
type RestoreResult struct {
	Version        int               `json:"version"`
	ID             string            `json:"restore_id"`
	RecordKey      string            `json:"record_key,omitempty"`
	Backup         metadata.Manifest `json:"backup"`
	Engine         string            `json:"engine"`
	Host           string            `json:"host,omitempty"`
	Port           int               `json:"port,omitempty"`
	Database       string            `json:"database"`
	NewDatabase    bool              `json:"new_database"`
	DryRun         bool              `json:"dry_run"`
	Clean          bool              `json:"clean"`
	Tables         []string          `json:"tables,omitempty"`
	Schemas        []string          `json:"schemas,omitempty"`
	Collections    []string          `json:"collections,omitempty"`
	BackupBefore   bool              `json:"backup_before_restore"`
	SafetyBackup   string            `json:"safety_backup,omitempty"`
	Status         string            `json:"status"`
	Validation     string            `json:"validation"`
	EvidenceStatus string            `json:"evidence_status"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    time.Time         `json:"completed_at"`
	Duration       float64           `json:"duration_seconds"`
}

func NewDestination(engine, source string, now time.Time) (string, error) {
	id, _, err := metadata.Identity("restore", "", now)
	if err != nil {
		return "", err
	}
	suffix := "_restore_" + now.UTC().Format("20060102_150405") + "_" + id[:8]
	if engine == "sqlite" {
		ext := filepath.Ext(source)
		if ext == "" {
			ext = ".sqlite"
		}
		return strings.TrimSuffix(source, filepath.Ext(source)) + suffix + ext, nil
	}
	base := strings.ToLower(source)
	base = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(base, "_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "db_" + base
	}
	if len(base) > 63-len(suffix) {
		base = base[:63-len(suffix)]
	}
	return base + suffix, nil
}
func ValidateNewDestination(engine, name string) error {
	if engine == "sqlite" {
		if name == "" || strings.ContainsAny(name, "\x00\r\n") {
			return fmt.Errorf("invalid new SQLite path")
		}
		return nil
	}
	return database.ValidateNewName(name)
}

type newPreflight struct {
	database.Adapter
	info *database.Info
}

func (a newPreflight) Preflight(ctx context.Context) (database.Info, error) {
	return *a.info, nil
}

func (s *Service) RestoreWithResult(ctx context.Context, key string, confirm, dry bool, q RestoreRequest) (result RestoreResult, resultErr error) {
	result = RestoreResult{Version: 1, Engine: s.Config.Database.Type, Host: s.Config.Database.Host, Port: s.Config.Database.Port, Database: s.Config.Database.Database, NewDatabase: q.NewDatabase, DryRun: dry, Clean: q.Options.Clean, Tables: q.Options.Tables, Schemas: q.Options.Schemas, Collections: q.Options.Collections, BackupBefore: q.BackupBefore, StartedAt: s.now(), Status: "failed", Validation: "not_run", EvidenceStatus: "not_recorded"}
	if s.Config.Database.Type == "sqlite" {
		result.Host = ""
		result.Port = 0
	}
	if !confirm && !dry {
		return result, fmt.Errorf("--confirm is required for restore")
	}
	if q.NewDatabase && q.BackupBefore {
		return result, fmt.Errorf("--backup-before-restore requires an existing destination")
	}
	if q.NewDatabase && q.Options.Clean {
		return result, fmt.Errorf("--clean cannot be combined with --new-database; a new destination has no objects to drop")
	}
	if err := validateRestoreSelection(result.Engine, q.Options); err != nil {
		return result, err
	}
	if q.BackupBefore && result.Engine == "mongodb" && !s.Config.Database.Options.Quiesced {
		return result, fmt.Errorf("destination safety backup requires database.options.quiesced=true and stopped MongoDB writes")
	}
	if q.NewDatabase {
		if err := ValidateNewDestination(result.Engine, result.Database); err != nil {
			return result, err
		}
	}
	id, _, err := metadata.Identity("restore", "", s.now())
	if err != nil {
		return result, err
	}
	result.ID = id
	defer func() {
		if !dry && result.Backup.ID != "" {
			m := result.Backup
			m.Duration = result.Duration
			s.notify(ctx, "restore", m, resultErr)
		}
	}()
	defer func() {
		result.CompletedAt = s.now()
		result.Duration = result.CompletedAt.Sub(result.StartedAt).Seconds()
		if resultErr == nil {
			if dry {
				result.Status = "preview_passed"
			} else {
				result.Status = "completed"
			}
		} else if errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
			result.Status = "cancelled"
		}
		if dry || result.Backup.ID == "" {
			return
		}
		result.RecordKey = "restore_history_" + result.StartedAt.Format("20060102_150405") + "_" + result.ID + ".json"
		result.EvidenceStatus = "recorded"
		b, e := json.Marshal(result)
		if e == nil {
			x, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			e = s.Store.Put(x, result.RecordKey, bytes.NewReader(b), int64(len(b)))
		}
		if e != nil {
			result.EvidenceStatus = "failed"
			resultErr = errors.Join(resultErr, fmt.Errorf("restore status %s; saving restore history failed: %w", result.Status, e))
		}
	}()
	work := *s
	var newInfo database.Info
	work.Notifier = nil
	work.Observe = func(e Event) {
		if e.Stage == "manifest" && e.State == "complete" {
			result.Backup = e.Manifest
		}
		s.emit(e)
	}
	prepare := func(ctx context.Context, m metadata.Manifest) error {
		result.Backup = m
		if q.NewDatabase {
			n, ok := s.DB.(database.NewTarget)
			if !ok {
				return fmt.Errorf("new destination is unsupported by adapter")
			}
			s.stage("restore.create", "start")
			info, e := n.PreflightNew(ctx)
			if e != nil {
				return e
			}
			if e = s.DB.Compatible(info, m.Database.Version, m.ToolVersion); e != nil {
				return e
			}
			newInfo = info
			if !dry {
				if e = n.CreateNew(ctx); e != nil {
					return e
				}
			}
			s.stage("restore.create", "complete")
		}
		return nil
	}
	if q.NewDatabase && dry {
		_, ok := s.DB.(database.NewTarget)
		if !ok {
			return result, fmt.Errorf("new destination unsupported")
		}
		work.DB = newPreflight{s.DB, &newInfo}
	}
	before := func() error {
		if !q.BackupBefore {
			return nil
		}
		full, ok := s.DB.(database.FullBackupAdapter)
		if !ok {
			return fmt.Errorf("full destination backup adapter is required")
		}
		safety := *s
		safety.DB = full.ForFullBackup()
		safety.Notifier = nil
		safety.Config.Database.Options = config.Options{Quiesced: s.Config.Database.Options.Quiesced}
		safety.Config.Protection = &config.Protection{VerifyAfterBackup: true}
		s.stage("restore.safety", "start")
		b, e := safety.BackupWithResult(ctx, "full", false)
		result.SafetyBackup = b.Manifest.Name
		if e != nil {
			return fmt.Errorf("destination backup failed; restore aborted: %w", e)
		}
		s.stage("restore.safety", "complete")
		return nil
	}
	if err = work.restore(ctx, key, confirm, dry, q.Options, prepare, before); err != nil {
		return result, err
	}
	if dry {
		result.Validation = "compatibility_checked"
		return result, nil
	}
	s.stage("restore.validation", "start")
	if v, ok := s.DB.(database.RecoveryValidator); ok {
		_, err = v.ValidateRecovery(ctx)
		result.Validation = "structure_checked"
	} else {
		_, err = s.DB.Preflight(ctx)
		result.Validation = "connectivity_checked"
	}
	if err != nil {
		result.Validation = "failed"
		return result, fmt.Errorf("post-restore validation failed: %w", err)
	}
	s.stage("restore.validation", "complete")
	return result, nil
}

func (s *Service) RestoreHistory(ctx context.Context) ([]RestoreResult, error) {
	objects, err := s.Store.List(ctx, "restore_history_")
	if err != nil {
		return nil, err
	}
	results := []RestoreResult{}
	for _, o := range objects {
		if !strings.HasSuffix(o.Key, ".json") {
			continue
		}
		r, e := s.Store.Get(ctx, o.Key)
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		r.Close()
		if e != nil {
			return nil, e
		}
		if len(b) > 1<<20 {
			return nil, fmt.Errorf("restore history record exceeds size limit")
		}
		var v RestoreResult
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if e = d.Decode(&v); e != nil {
			return nil, e
		}
		var extra any
		if d.Decode(&extra) != io.EOF || v.Version != 1 || v.RecordKey != o.Key || v.ID == "" || v.Backup.ID == "" || v.StartedAt.IsZero() || v.CompletedAt.Before(v.StartedAt) || v.DryRun || (v.Status != "completed" && v.Status != "failed" && v.Status != "cancelled") {
			return nil, fmt.Errorf("invalid restore history record %s", o.Key)
		}
		if err := v.Backup.Validate(); err != nil {
			return nil, fmt.Errorf("invalid source manifest in restore history %s: %w", o.Key, err)
		}
		if v.Database == "" || v.Duration < 0 || v.EvidenceStatus != "recorded" || o.Key != "restore_history_"+v.StartedAt.UTC().Format("20060102_150405")+"_"+v.ID+".json" {
			return nil, fmt.Errorf("invalid restore history destination/evidence fields")
		}
		if v.Status == "completed" && v.Validation != "structure_checked" && v.Validation != "connectivity_checked" {
			return nil, fmt.Errorf("completed restore history lacks validation")
		}
		results = append(results, v)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].StartedAt.Equal(results[j].StartedAt) {
			return results[i].RecordKey < results[j].RecordKey
		}
		return results[i].StartedAt.After(results[j].StartedAt)
	})
	return results, nil
}

func validateRestoreSelection(engine string, o database.RestoreOptions) error {
	if (engine == "sqlite" || engine == "mysql") && (o.Clean || len(o.Tables) > 0 || len(o.Schemas) > 0 || len(o.Collections) > 0) {
		return fmt.Errorf("%s supports full restore only; clean and selectors are unsupported", engine)
	}
	if engine == "postgres" && len(o.Collections) > 0 {
		return fmt.Errorf("PostgreSQL uses --table/--schema rather than --collection")
	}
	if engine == "mongodb" && (len(o.Tables) > 0 || len(o.Schemas) > 0) {
		return fmt.Errorf("MongoDB uses --collection rather than --table/--schema")
	}
	for _, name := range o.Collections {
		if name == "" || strings.ContainsAny(name, "*?[]\x00\r\n") {
			return fmt.Errorf("collection selectors must be literal nonempty names")
		}
	}
	return nil
}

type ExportResult struct {
	Backup       string `json:"backup"`
	File         string `json:"file"`
	Bytes        int64  `json:"bytes"`
	Decompressed bool   `json:"decompressed"`
	Checksum     string `json:"stored_sha256"`
}

func (s *Service) Export(ctx context.Context, key, path string, decompress bool) (result ExportResult, err error) {
	if path == "" {
		return result, fmt.Errorf("--file is required")
	}
	m, f, err := s.snapshot(ctx, key)
	if err != nil {
		return result, err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	var src io.Reader = pipeline.Reader{Context: ctx, Source: f}
	expected := m.Pipeline.Stored
	if decompress {
		c, e := compression.New(m.Pipeline.Compression, m.Pipeline.Level)
		if e != nil {
			return result, e
		}
		r, e := c.Decompress(src)
		if e != nil {
			return result, e
		}
		defer r.Close()
		src = r
		expected = m.Pipeline.Raw
	}
	// Exclusive creation never overwrites a local backup, manifest or user file.
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	owned, _ := out.Stat()
	defer func() {
		out.Close()
		if err != nil {
			current, e := os.Lstat(path)
			if e == nil && owned != nil && os.SameFile(owned, current) {
				os.Remove(path)
			}
		}
	}()
	s.stage("export.write", "start")
	n, err := io.Copy(out, pipeline.Reader{Context: ctx, Source: src})
	if err == nil && n != expected {
		err = fmt.Errorf("export size mismatch")
	}
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		return result, err
	}
	s.stage("export.write", "complete")
	absolute, err := filepath.Abs(path)
	return ExportResult{m.Name, absolute, n, decompress, m.Checksum.Hash}, err
}
