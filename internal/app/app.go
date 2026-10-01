package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/compression"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/notify"
	"github.com/sung2708/DBVault/internal/pipeline"
	"github.com/sung2708/DBVault/internal/storage"
)

type Service struct {
	Config   config.Config
	DB       database.Adapter
	Store    storage.Provider
	Logger   *slog.Logger
	Version  string
	Now      func() time.Time
	Notifier notify.Notifier
	Observe  func(Event)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) log(message string, args ...any) {
	if s.Logger != nil {
		s.Logger.Info(message, args...)
	}
}
func (s *Service) Test(ctx context.Context) (database.Info, error) {
	if s.DB == nil {
		return database.Info{}, fault.Wrap(fault.Unsupported, "database adapter", fmt.Errorf("not implemented"))
	}
	s.stage("preflight", "start")
	info, err := s.DB.Preflight(ctx)
	if err == nil {
		s.emit(Event{Stage: "preflight", State: "complete", Info: info})
	}
	return info, err
}
func (s *Service) Backup(ctx context.Context, kind string, dry bool) (metadata.Manifest, error) {
	result, err := s.BackupWithResult(ctx, kind, dry)
	return result.Manifest, err
}

func (s *Service) BackupWithResult(ctx context.Context, kind string, dry bool) (operation BackupResult, resultErr error) {
	operation.BackupStatus = "failed"
	operation.Verification = VerificationResult{Requested: s.Config.Protection != nil && s.Config.Protection.VerifyAfterBackup, Status: "not_requested", EvidenceStatus: "not_requested"}
	notificationStart := s.now()
	defer func() {
		if !dry {
			eventManifest := operation.Manifest
			eventManifest.Duration = s.now().Sub(notificationStart).Seconds()
			s.notifyBackup(ctx, eventManifest, resultErr, operation.BackupStatus, operation.Verification)
		}
	}()
	var m metadata.Manifest
	if s.DB == nil {
		return operation, fault.Wrap(fault.Unsupported, "database adapter", fmt.Errorf("not implemented"))
	}
	if err := database.RequireBackup(s.DB, kind); err != nil {
		return operation, err
	}
	info, err := s.Test(ctx)
	if err != nil {
		return operation, err
	}
	if dry {
		operation.BackupStatus = "not_created"
		return operation, nil
	}
	c, err := compression.New(s.Config.Compression.Type, s.Config.Compression.Level)
	if err != nil {
		return operation, err
	}
	start := s.now()
	id, key, err := metadata.Identity(s.Config.Database.Database, s.DB.Extension()+c.Extension(), start)
	if err != nil {
		return operation, err
	}
	s.log("backup started", "backup_id", id, "operation", "backup", "database_type", s.DB.Name(), "database_name", s.Config.Database.Database, "storage", s.Config.Storage.Type, "compression", s.Config.Compression.Type)
	s.stage("backup.stream", "start")
	pipeR, pipeW := io.Pipe()
	defer pipeR.Close()
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	var digest string
	var stored, raw int64
	done := make(chan error, 1)
	go func() {
		// The checksum writer sits after compression; native output is counted first.
		h := newHashWriter(pipeW)
		cw, e := c.Compress(h)
		if e == nil {
			count := &pipeline.Counter{Writer: cw}
			e = s.DB.Dump(child, count)
			raw = count.N
			closeErr := cw.Close()
			if e == nil {
				e = closeErr
			}
		}
		digest = h.digest()
		stored = h.N
		if e == nil && raw == 0 {
			e = fmt.Errorf("native dump produced no data")
		}
		pipeW.CloseWithError(e)
		done <- e
	}()
	err = s.Store.Put(child, key, s.meter(pipeR, "backup.stream", 0), -1)
	storedSuccessfully := err == nil
	if err != nil {
		cancel()
		pipeR.CloseWithError(err)
	}
	producerErr := <-done
	if err == nil {
		err = producerErr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if storedSuccessfully {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cleanupCancel()
			err = errors.Join(err, s.Store.Delete(cleanupCtx, key))
		}
		return operation, fault.Wrap(fault.Backup, "stream dump", err)

	}
	s.stage("backup.stream", "complete")
	complete := s.now()
	m = metadata.Manifest{Version: "1.0", ID: id, Name: key, BackupType: kind, CreatedAt: start, CompletedAt: complete, Database: metadata.Database{Engine: s.DB.Name(), Version: info.ServerVersion, Name: s.Config.Database.Database, Format: s.DB.Format()}, Pipeline: metadata.Pipeline{Compression: s.Config.Compression.Type, Level: s.Config.Compression.Level, Raw: raw, Stored: stored}, Checksum: metadata.Checksum{Algorithm: "sha256", Hash: digest}, Duration: complete.Sub(start).Seconds(), Status: "completed", ApplicationVersion: s.Version, ToolVersion: info.ToolVersion, Storage: s.Config.Storage.Type}
	m.Database.IncludeTables = append([]string(nil), s.Config.Database.Options.IncludeTables...)
	m.Database.ExcludeTables = append([]string(nil), s.Config.Database.Options.ExcludeTables...)
	m.Database.IncludeCollections = append([]string(nil), s.Config.Database.Options.IncludeCollections...)
	m.Database.ExcludeCollections = append([]string(nil), s.Config.Database.Options.ExcludeCollections...)
	s.stage("backup.register", "start")
	b, err := metadata.Encode(m)
	if err == nil {
		err = s.Store.Put(ctx, key+".meta.json", bytes.NewReader(b), int64(len(b)))
	}
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		cleanupErr := s.Store.Delete(cleanupCtx, key)
		return operation, fault.Wrap(fault.Storage, "register manifest", errors.Join(err, cleanupErr))
	}
	operation.Manifest = m
	operation.BackupID = m.ID
	operation.BackupStatus = "success"
	s.emit(Event{Stage: "backup.register", State: "complete", Manifest: m})
	s.log("backup completed", "backup_id", id, "status", "completed", "duration", m.Duration, "size", stored)
	if operation.Verification.Requested {
		verifiedManifest, verification, verifyErr := s.verifyAndRecord(ctx, key, &m)
		operation.Manifest = verifiedManifest
		operation.Verification = verification
		if verifyErr != nil {
			wrapped := &PostBackupVerificationError{Verification: verification, Err: verifyErr}
			s.log("backup created but post-backup verification failed", "backup_id", m.ID, "verification_status", verification.Status, "failure_category", verification.FailureCategory)
			return operation, wrapped
		}
		s.log("backup post-verification completed", "backup_id", m.ID, "verification_status", verification.Status, "verified_bytes", verification.Bytes)
	}
	return operation, nil
}
func (s *Service) ReadManifest(ctx context.Context, key string) (metadata.Manifest, error) {
	r, err := s.Store.Get(ctx, key+".meta.json")
	if err != nil {
		return metadata.Manifest{}, fault.Wrap(fault.Integrity, "open manifest", err)
	}
	defer r.Close()
	m, err := metadata.Decode(r)
	if err == nil && m.Name != key {
		err = fmt.Errorf("manifest backup_name does not match target")
	}
	return m, fault.Wrap(fault.Integrity, "validate manifest", err)
}

// snapshot verifies the exact stored bytes into an operator-private temporary
// file. Restore never reopens a mutable source after verification.
func (s *Service) snapshot(ctx context.Context, key string) (metadata.Manifest, *os.File, error) {
	s.stage("manifest", "start")
	m, err := s.ReadManifest(ctx, key)
	if err != nil {
		return m, nil, err
	}
	s.emit(Event{Stage: "manifest", State: "complete", Manifest: m})
	r, err := s.Store.Get(ctx, key)
	if err != nil {
		return m, nil, fault.Wrap(fault.Integrity, "open artifact", err)
	}
	defer r.Close()
	f, err := os.CreateTemp("", "dbvault-verified-*")
	if err != nil {
		return m, nil, err
	}
	fail := func(e error) (metadata.Manifest, *os.File, error) {
		f.Close()
		os.Remove(f.Name())
		return m, nil, fault.Wrap(fault.Integrity, "verify stored artifact", e)
	}
	s.stage("restore.verify", "start")
	digest, n, err := pipeline.Hash(ctx, s.meter(r, "restore.verify", m.Pipeline.Stored), f)
	if err != nil {
		return fail(err)
	}
	if digest != m.Checksum.Hash || n != m.Pipeline.Stored {
		return fail(fmt.Errorf("checksum or stored size mismatch; restore aborted"))
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	s.stage("restore.verify", "complete")
	return m, f, nil
}
func (s *Service) Restore(ctx context.Context, key string, confirm, dry bool, o database.RestoreOptions) (resultErr error) {
	return s.restore(ctx, key, confirm, dry, o, nil, nil)
}

// prepare is reserved for the recovery drill's exclusively owned target. It
// runs only after the same immutable snapshot has passed stored-byte verification.
func (s *Service) restore(ctx context.Context, key string, confirm, dry bool, o database.RestoreOptions, prepare func(context.Context, metadata.Manifest) error, beforeWrite func() error) (resultErr error) {
	var notificationManifest metadata.Manifest
	notificationStart := s.now()
	defer func() {
		if !dry {
			notificationManifest.Duration = s.now().Sub(notificationStart).Seconds()
			s.notify(ctx, "restore", notificationManifest, resultErr)
		}
	}()
	if !confirm && !dry {
		return fault.Wrap(fault.Restore, "confirmation", fmt.Errorf("must specify --confirm for destructive restore"))
	}
	if s.DB == nil {
		return fault.Wrap(fault.Unsupported, "database adapter", fmt.Errorf("not implemented"))
	}
	if (len(o.Tables) > 0 || len(o.Schemas) > 0 || len(o.Collections) > 0) && !s.DB.Capabilities().SelectiveRestore {
		return fault.Wrap(fault.Unsupported, "selective restore", fmt.Errorf("not supported by the configured adapter"))
	}
	m, f, err := s.snapshot(ctx, key)
	if err != nil {
		return err
	}
	notificationManifest = m
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if m.Database.Engine != s.DB.Name() || m.Database.Format != s.DB.Format() {
		return fault.Wrap(fault.Unsupported, "restore compatibility", fmt.Errorf("backup engine or format differs from configured adapter"))
	}
	if prepare != nil {
		if err := prepare(ctx, m); err != nil {
			return err
		}
	}
	info, err := s.Test(ctx)
	if err != nil {
		return err
	}
	if err = s.DB.Compatible(info, m.Database.Version, m.ToolVersion); err != nil {
		return fault.Wrap(fault.Unsupported, "restore compatibility", err)
	}
	s.stage("restore.compatibility", "complete")
	if dry {
		return nil
	}
	if beforeWrite != nil {
		if err := beforeWrite(); err != nil {
			return err
		}
	}
	c, err := compression.New(m.Pipeline.Compression, m.Pipeline.Level)
	if err != nil {
		return err
	}
	r, err := c.Decompress(pipeline.Reader{Context: ctx, Source: f})
	if err != nil {
		return fault.Wrap(fault.Integrity, "decompress artifact", err)
	}
	defer r.Close()
	// Feed a pipe so all decompression errors and trailer checks propagate even
	// when a native process exits early. Cancel and close both ends on failure.
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	defer pr.Close()
	done := make(chan error, 1)
	go func() {
		n, e := io.Copy(pw, pipeline.Reader{Context: child, Source: r})
		if e == nil && n != m.Pipeline.Raw {
			e = fmt.Errorf("uncompressed size mismatch")
		}
		pw.CloseWithError(e)
		done <- e
	}()
	o.SourceDatabase = m.Database.Name
	s.stage("restore.write", "start")
	err = s.DB.Restore(child, s.meter(pr, "restore.write", m.Pipeline.Raw), o)
	cancel()
	pr.CloseWithError(fmt.Errorf("restore process finished"))
	copyErr := <-done
	// Cancellation used to stop the producer after a native restore failure is
	// internal teardown, not evidence that the operator cancelled the drill.
	if err != nil && ctx.Err() == nil && errors.Is(copyErr, context.Canceled) {
		copyErr = nil
	}
	if err != nil || copyErr != nil {
		return fault.Wrap(fault.Restore, "native restore", errors.Join(err, copyErr))
	}
	s.stage("restore.write", "complete")
	s.log("restore completed", "backup_id", m.ID, "operation", "restore", "status", "completed")
	return nil
}

func (s *Service) notify(ctx context.Context, operation string, m metadata.Manifest, err error) {
	if s.Notifier == nil {
		return
	}
	status := "completed"
	message := ""
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	event := notify.Event{Operation: operation, Status: status, BackupID: m.ID, DatabaseType: s.Config.Database.Type, DatabaseName: s.Config.Database.Database, Size: m.Pipeline.Stored, Duration: m.Duration, Error: message}
	nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e := s.Notifier.Send(nctx, event); e != nil && s.Logger != nil {
		s.Logger.Warn("notification failed", "operation", operation, "error_category", "notification", "error", e)
	}
}

func (s *Service) notifyBackup(ctx context.Context, m metadata.Manifest, err error, backupStatus string, verification VerificationResult) {
	if s.Notifier == nil {
		return
	}
	status := "completed"
	message := ""
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	var postVerification *PostBackupVerificationError
	if errors.As(err, &postVerification) {
		message = "stored-artifact verification " + postVerification.Verification.Status + " (" + postVerification.Verification.FailureCategory + ")"
	}
	event := notify.Event{Operation: "backup", Status: status, BackupStatus: backupStatus, VerificationRequested: verification.Requested, VerificationStatus: verification.Status, VerificationEvidenceStatus: verification.EvidenceStatus, BackupID: m.ID, DatabaseType: s.Config.Database.Type, DatabaseName: s.Config.Database.Database, Size: m.Pipeline.Stored, Duration: m.Duration, Error: message}
	nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e := s.Notifier.Send(nctx, event); e != nil && s.Logger != nil {
		s.Logger.Warn("notification failed", "operation", "backup", "error_category", "notification", "error", e)
	}
}
func (s *Service) List(ctx context.Context, prefix string) ([]metadata.Manifest, error) {
	objects, err := s.Store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	result := []metadata.Manifest{}
	for _, o := range objects {
		if !strings.HasSuffix(o.Key, ".meta.json") {
			continue
		}
		key := strings.TrimSuffix(o.Key, ".meta.json")
		m, err := s.ReadManifest(ctx, key)
		if err != nil {
			return nil, err
		}
		exists, err := s.Store.Exists(ctx, key)
		if err != nil {
			return nil, err
		}
		if exists {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].Name < result[j].Name
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
func (s *Service) Delete(ctx context.Context, key string, confirm, dry bool) error {
	if !confirm && !dry {
		return fmt.Errorf("must specify --confirm for deletion")
	}
	s.stage("manifest", "start")
	m, err := s.ReadManifest(ctx, key)
	if err != nil {
		return err
	}
	s.emit(Event{Stage: "manifest", State: "complete", Manifest: m})
	if dry {
		s.stage("delete.preview", "complete")
		return nil
	}
	// Unregister before removing bytes; an interrupted delete cannot expose a
	// manifest pointing to missing data as a valid backup.
	s.stage("delete", "start")
	if err := s.Store.Delete(ctx, key+".meta.json"); err != nil {
		return err
	}
	err = s.Store.Delete(ctx, key)
	if err == nil {
		s.stage("delete", "complete")
	}
	return err
}
