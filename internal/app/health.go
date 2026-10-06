package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
)

type HealthStatus string

const (
	Healthy  HealthStatus = "healthy"
	Warning  HealthStatus = "warning"
	Critical HealthStatus = "critical"
	Unknown  HealthStatus = "unknown"
)

// HealthReport uses the existing completed manifests as its only registry.
type HealthReport struct {
	Reason    string           `json:"reason,omitempty"`
	Status    HealthStatus     `json:"status"`
	Databases []DatabaseHealth `json:"databases"`
}
type DatabaseHealth struct {
	BackupScope         string           `json:"backup_scope,omitempty"`
	SourceIdentity      string           `json:"source_identity,omitempty"`
	Name                string           `json:"name"`
	Engine              string           `json:"engine"`
	Status              HealthStatus     `json:"status"`
	Reason              string           `json:"reason"`
	LastBackupAt        *time.Time       `json:"last_backup_at,omitempty"`
	AgeSeconds          *float64         `json:"age_seconds,omitempty"`
	MaxAgeSeconds       *float64         `json:"max_age_seconds,omitempty"`
	BackupName          string           `json:"backup_name,omitempty"`
	StoredBytes         int64            `json:"stored_bytes,omitempty"`
	ArtifactExists      *bool            `json:"artifact_exists,omitempty"`
	Stale               *bool            `json:"stale,omitempty"`
	Integrity           string           `json:"integrity"`
	LastVerifiedAt      *time.Time       `json:"last_verified_at,omitempty"`
	VerificationNote    string           `json:"verification_note,omitempty"`
	RestoreTest         string           `json:"restore_test"`
	LastRecoveryDrillAt *time.Time       `json:"last_recovery_drill_at,omitempty"`
	RecoveryRecord      string           `json:"recovery_record,omitempty"`
	RecoveryNote        string           `json:"recovery_note,omitempty"`
	Schedules           []HealthSchedule `json:"schedules,omitempty"`
	ScheduleNote        string           `json:"schedule_note,omitempty"`
	RecentBackups       []HealthBackup   `json:"-"`
}
type HealthBackup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	Type        string    `json:"type"`
	StoredBytes int64     `json:"stored_bytes"`
	Status      string    `json:"status"`
}
type HealthSchedule struct {
	Operation string `json:"operation,omitempty"`
	ID        string `json:"id"`
	Cron      string `json:"cron"`
	Enabled   bool   `json:"enabled"`
}

// HealthFailure is a completed check with a non-healthy monitoring result.
// Operational failures instead retain their existing typed exit codes.
type HealthFailure struct{ Status HealthStatus }

func (e *HealthFailure) Error() string { return "backup health: " + string(e.Status) }

// Health reads sidecars and checks only the latest matching artifact. The
// provider API lists the configured namespace (no server-side latest query).
// Default mode never opens archive contents. Explicit --verify uses the shared
// verifier and persists a small immutable record for the selected backup.
func (s *Service) Health(ctx context.Context, verify bool) (HealthReport, error) {
	if s.Config.Health.BackupScope == "pitr" {
		return s.nativeHealth(ctx, verify)
	}
	h := DatabaseHealth{Name: s.Config.Database.Database, Engine: s.Config.Database.Type,
		Status: Unknown, Integrity: "unknown", RestoreTest: "unknown"}
	finish := func(err error) (HealthReport, error) {
		return HealthReport{Status: h.Status, Databases: []DatabaseHealth{h}}, err
	}
	var maxAge time.Duration
	if s.Config.Health.MaxBackupAge != "" {
		var err error
		maxAge, err = time.ParseDuration(s.Config.Health.MaxBackupAge)
		if err != nil || maxAge <= 0 {
			return finish(fault.Wrap(fault.Configuration, "health.max_backup_age", fmt.Errorf("must be a positive duration")))
		}
		seconds := maxAge.Seconds()
		h.MaxAgeSeconds = &seconds
	}
	now := s.now()
	objects, err := s.Store.List(ctx, "")
	if err != nil {
		h.Reason = "Storage listing unavailable"
		return finish(fault.Wrap(fault.Storage, "health list", err))
	}
	// Stable order makes malformed-sidecar handling independent of provider order.
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	var latest *metadata.Manifest
	matching := make([]metadata.Manifest, 0, 5)
	artifactKeys := make(map[string]bool, len(objects))
	for _, object := range objects {
		artifactKeys[object.Key] = true
	}
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			h.Reason = "Health check cancelled"
			return finish(err)
		}
		if !strings.HasSuffix(object.Key, ".meta.json") {
			continue
		}
		m, err := s.ReadManifest(ctx, strings.TrimSuffix(object.Key, ".meta.json"))
		if err != nil {
			h.Reason = "Registered metadata could not be validated; latest backup cannot be determined"
			return finish(err)
		}
		if m.Database.Engine != h.Engine || m.Database.Name != h.Name {
			continue
		}
		matching = append(matching, m)
		// CreatedAt reflects snapshot/dump start, rather than publication time.
		if latest == nil || m.CreatedAt.After(latest.CreatedAt) || (m.CreatedAt.Equal(latest.CreatedAt) && m.Name < latest.Name) {
			copy := m
			latest = &copy
		}
	}
	sort.Slice(matching, func(i, j int) bool {
		if matching[i].CreatedAt.Equal(matching[j].CreatedAt) {
			return matching[i].Name < matching[j].Name
		}
		return matching[i].CreatedAt.After(matching[j].CreatedAt)
	})
	for _, m := range matching {
		if len(h.RecentBackups) == 5 {
			break
		}
		status := m.Status
		if !artifactKeys[m.Name] {
			status = "artifact_missing"
		}
		for _, object := range objects {
			if object.Key == m.Name && object.Size != m.Pipeline.Stored {
				status = "size_mismatch"
				break
			}
		}
		h.RecentBackups = append(h.RecentBackups, HealthBackup{ID: m.ID, Name: m.Name, CreatedAt: m.CreatedAt.UTC(), Type: m.BackupType, StoredBytes: m.Pipeline.Stored, Status: status})
	}
	if latest == nil {
		h.Status = Critical
		h.Reason = "No registered completed backup; run dbvault backup"
		return finish(nil)
	}
	h.BackupName = latest.Name
	h.StoredBytes = latest.Pipeline.Stored
	at := latest.CreatedAt.UTC()
	h.LastBackupAt = &at
	// Immutable drill records must match this exact backup identity and checksum.
	// A damaged/unreadable history object never becomes positive recovery evidence.
	var drill *DrillResult
	for _, object := range objects {
		if !strings.HasSuffix(object.Key, ".recovery.json") {
			continue
		}
		reader, err := s.Store.Get(ctx, object.Key)
		if err != nil {
			h.RecoveryNote = "Some recovery evidence is unavailable"
			continue
		}
		record, err := DecodeDrill(reader)
		reader.Close()
		if err != nil || record.RecordKey != object.Key {
			h.RecoveryNote = "Some recovery evidence is invalid"
			continue
		}
		if record.BackupID != latest.ID || record.BackupName != latest.Name || record.Checksum != latest.Checksum.Hash || record.Engine != latest.Database.Engine || record.SourceDatabase != latest.Database.Name {
			continue
		}
		if record.CompletedAt.After(now) {
			h.RecoveryNote = "Recovery evidence has future timestamps"
			continue
		}
		if drill == nil || record.CompletedAt.After(drill.CompletedAt) || (record.CompletedAt.Equal(drill.CompletedAt) && record.RecordKey < drill.RecordKey) {
			copy := record
			drill = &copy
		}
	}
	if h.RecoveryNote == "" && drill != nil {
		h.RestoreTest = drill.Status
		at := drill.CompletedAt.UTC()
		h.LastRecoveryDrillAt = &at
		h.RecoveryRecord = drill.RecordKey
	}
	var verification *VerificationRecord
	for _, object := range objects {
		if !strings.HasPrefix(object.Key, "verification_") || !strings.HasSuffix(object.Key, ".json") {
			continue
		}
		reader, err := s.Store.Get(ctx, object.Key)
		if err != nil {
			h.VerificationNote = "Some verification evidence is unavailable"
			continue
		}
		record, err := DecodeVerification(reader)
		reader.Close()
		if err != nil || record.RecordKey != object.Key {
			h.VerificationNote = "Some verification evidence is invalid"
			continue
		}
		if record.BackupID != latest.ID || record.BackupName != latest.Name || record.Checksum != latest.Checksum.Hash || record.Engine != latest.Database.Engine || record.Database != latest.Database.Name {
			continue
		}
		if record.CompletedAt.After(now) {
			h.VerificationNote = "Verification evidence has a future timestamp"
			continue
		}
		if verification == nil || record.CompletedAt.After(verification.CompletedAt) || (record.CompletedAt.Equal(verification.CompletedAt) && record.RecordKey < verification.RecordKey) {
			copy := record
			verification = &copy
		}
	}
	if verification != nil {
		if verification.Status == "verified" {
			h.Integrity = "verified"
			at := verification.CompletedAt.UTC()
			h.LastVerifiedAt = &at
		} else if verification.Status == "failed" {
			h.Integrity = "failed"
			at := verification.CompletedAt.UTC()
			h.LastVerifiedAt = &at
			h.VerificationNote = "Latest stored-artifact verification failed (" + verification.FailureCategory + ")"
		} else {
			h.VerificationNote = "Latest stored-artifact verification was cancelled"
		}
	}
	if latest.CreatedAt.After(now) || latest.CompletedAt.After(now) {
		h.Reason = "Backup timestamps are in the future; check clock synchronization"
		return finish(nil)
	}
	age := now.Sub(at)
	seconds := age.Seconds()
	h.AgeSeconds = &seconds
	if maxAge > 0 {
		stale := age > maxAge
		h.Stale = &stale
	}
	if h.Integrity == "failed" {
		h.Status = Critical
		h.Reason = "Latest stored-artifact verification failed"
		return finish(nil)
	}
	exists, err := s.Store.Exists(ctx, latest.Name)
	if err != nil {
		h.Reason = "Artifact availability could not be checked"
		return finish(fault.Wrap(fault.Storage, "health artifact", err))
	}
	h.ArtifactExists = &exists
	if !exists {
		h.Status = Critical
		h.Reason = "Latest registered backup artifact is missing"
		return finish(nil)
	}
	// Listed size is cheap evidence, not a checksum verification.
	for _, object := range objects {
		if object.Key == latest.Name && object.Size != latest.Pipeline.Stored {
			h.Status = Critical
			h.Integrity = "failed"
			h.Reason = "Stored size differs from registered metadata"
			return finish(nil)
		}
	}
	if verify {
		if err := s.dependencyChain(ctx, *latest, true); err != nil {
			h.Status = Critical
			h.Integrity = "failed"
			h.Reason = "Incremental dependency verification failed"
			return finish(err)
		}
		if _, err := s.Verify(ctx, latest.Name); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				h.Status = Unknown
				h.Integrity = "unknown"
				h.VerificationNote = "Active stored-artifact verification was cancelled"
				h.Reason = "Active integrity verification was cancelled"
			} else {
				h.Status = Critical
				h.Integrity = "failed"
				h.Reason = "Active integrity verification did not pass"
			}
			return finish(err)
		}
		h.Integrity = "verified"
		at := s.now()
		h.LastVerifiedAt = &at
	}
	if !verify {
		if err := s.dependencyChain(ctx, *latest, false); err != nil {
			h.Status = Critical
			h.Reason = "Incremental dependency is missing or invalid"
			return finish(err)
		}
	}
	// A slow scan or active verification can cross the configured age limit.
	// Freshness reflects evaluation completion, rather than the start of I/O.
	now = s.now()
	if latest.CreatedAt.After(now) || latest.CompletedAt.After(now) {
		h.Reason = "Backup timestamps are in the future; check clock synchronization"
		h.AgeSeconds = nil
		h.Stale = nil
		return finish(nil)
	}
	age = now.Sub(at)
	seconds = age.Seconds()
	if h.Stale != nil {
		*h.Stale = age > maxAge
	}
	if maxAge == 0 {
		h.Reason = "No backup freshness policy configured"
		return finish(nil)
	}
	if *h.Stale {
		h.Status = Critical
		h.Reason = fmt.Sprintf("Backup is %s beyond the configured freshness limit", age-maxAge)
		return finish(nil)
	}
	if h.Integrity == "unknown" {
		h.Status = Warning
		h.Reason = "Backup is within freshness policy; stored checksum verification history is unavailable"
	} else if h.LastVerifiedAt != nil && !verify {
		h.Status = Healthy
		h.Reason = "Backup is within freshness policy and stored-artifact verification evidence passed"
	} else {
		h.Status = Healthy
		h.Reason = "Backup is within freshness policy and stored integrity passed this check"
	}
	return finish(nil)
}
