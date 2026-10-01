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
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/pipeline"
)

type VerificationResult struct {
	Requested       bool       `json:"requested"`
	Status          string     `json:"status"`
	EvidenceStatus  string     `json:"evidence_status"`
	Algorithm       string     `json:"algorithm,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Bytes           int64      `json:"bytes,omitempty"`
	RecordKey       string     `json:"record_key,omitempty"`
	FailureCategory string     `json:"failure_category,omitempty"`
}

type BackupResult struct {
	metadata.Manifest `json:",inline"`
	BackupID          string             `json:"backup_id,omitempty"`
	BackupStatus      string             `json:"backup_status"`
	Verification      VerificationResult `json:"verification"`
}

type PostBackupVerificationError struct {
	Verification VerificationResult
	Err          error
}

func (e *PostBackupVerificationError) Error() string {
	if e.Verification.EvidenceStatus == "failed" {
		return "backup artifact was created and verification status is " + e.Verification.Status + ", but verification evidence could not be recorded"
	}
	return "backup artifact was created, but required stored-artifact verification " + e.Verification.Status + ": " + e.Err.Error()
}
func (e *PostBackupVerificationError) Unwrap() error { return e.Err }

type VerificationRecord struct {
	Version         int       `json:"version"`
	ID              string    `json:"verification_id"`
	RecordKey       string    `json:"record_key"`
	BackupID        string    `json:"backup_id"`
	BackupName      string    `json:"backup_name"`
	Engine          string    `json:"engine"`
	Database        string    `json:"database_name"`
	Checksum        string    `json:"checksum"`
	Algorithm       string    `json:"algorithm"`
	Status          string    `json:"status"`
	FailureCategory string    `json:"failure_category,omitempty"`
	Bytes           int64     `json:"verified_bytes"`
	CompletedAt     time.Time `json:"completed_at"`
}

func DecodeVerification(r io.Reader) (VerificationRecord, error) {
	var record VerificationRecord
	data, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return record, fmt.Errorf("invalid verification evidence")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&record); err != nil {
		return record, fmt.Errorf("invalid verification evidence")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return record, fmt.Errorf("trailing verification evidence")
	}
	if record.Version != 1 || record.ID == "" || record.RecordKey == "" || record.BackupID == "" || record.BackupName == "" || record.Engine == "" || record.Database == "" || record.Algorithm != "sha256" || record.Checksum == "" || record.CompletedAt.IsZero() || record.Bytes < 0 || (record.Status != "verified" && record.Status != "failed" && record.Status != "cancelled") {
		return record, fmt.Errorf("invalid verification evidence fields")
	}
	checksum, err := hex.DecodeString(record.Checksum)
	if err != nil || len(checksum) != 32 {
		return record, fmt.Errorf("invalid verification evidence checksum")
	}
	if record.Status == "failed" || record.Status == "cancelled" {
		validCategory := (record.Status == "failed" && (record.FailureCategory == "integrity" || record.FailureCategory == "storage_read")) || (record.Status == "cancelled" && record.FailureCategory == "cancelled")
		if !validCategory {
			return record, fmt.Errorf("invalid verification failure category")
		}
	} else if record.FailureCategory != "" {
		return record, fmt.Errorf("unexpected verification failure category")
	}
	return record, nil
}

func (s *Service) verifyStored(ctx context.Context, key string) (metadata.Manifest, int64, error) {
	s.stage("manifest", "start")
	m, err := s.ReadManifest(ctx, key)
	if err != nil {
		return m, 0, err
	}
	s.emit(Event{Stage: "manifest", State: "complete", Manifest: m})
	r, err := s.Store.Get(ctx, key)
	if err != nil {
		return m, 0, fault.Wrap(fault.Integrity, "open artifact", err)
	}
	defer r.Close()
	s.stage("verify", "start")
	digest, n, err := pipeline.Hash(ctx, s.meter(r, "verify", m.Pipeline.Stored), io.Discard)
	if err != nil {
		return m, n, fault.Wrap(fault.Storage, "read artifact during verification", err)
	}
	if n != m.Pipeline.Stored || digest != m.Checksum.Hash {
		err = fmt.Errorf("stored size or SHA-256 differs from manifest")
	}
	if err != nil {
		return m, n, fault.Wrap(fault.Integrity, "verify stored artifact", err)
	}
	s.stage("verify", "complete")
	return m, n, nil
}

// verifyAndRecord reuses verifyStored and appends a small immutable record.
// A detached, bounded context lets cancellation evidence be saved without
// keeping a large artifact read alive.
func (s *Service) verifyAndRecord(ctx context.Context, key string, expected *metadata.Manifest) (metadata.Manifest, VerificationResult, error) {
	m, count, err := s.verifyStored(ctx, key)
	if m.Name == "" && expected != nil {
		m = *expected
	}
	result := VerificationResult{Requested: true, Status: "failed", EvidenceStatus: "pending", Algorithm: "sha256", Bytes: count}
	if err == nil {
		result.Status = "verified"
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.Status = "cancelled"
		result.FailureCategory = "cancelled"
	} else {
		var f *fault.Error
		if errors.As(err, &f) && f.Kind == fault.Storage {
			result.FailureCategory = "storage_read"
		} else {
			result.FailureCategory = "integrity"
		}
	}
	completed := s.now()
	result.CompletedAt = &completed
	if m.Name == "" {
		result.EvidenceStatus = "unavailable"
		return m, result, err
	}
	record, keyName, encodeErr := newVerificationRecord(m, result, completed)
	if encodeErr != nil {
		result.EvidenceStatus = "failed"
		result.FailureCategory = "evidence_write"
		return m, result, errors.Join(err, encodeErr)
	}
	data, encodeErr := json.Marshal(record)
	if encodeErr != nil {
		result.EvidenceStatus = "failed"
		result.FailureCategory = "evidence_write"
		return m, result, errors.Join(err, encodeErr)
	}
	evidenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if putErr := s.Store.Put(evidenceCtx, keyName, bytes.NewReader(data), int64(len(data))); putErr != nil {
		result.EvidenceStatus = "failed"
		result.FailureCategory = "evidence_write"
		return m, result, errors.Join(err, fault.Wrap(fault.Storage, "record verification evidence", putErr))
	}
	result.RecordKey = keyName
	result.EvidenceStatus = "recorded"
	return m, result, err
}

// Verify checks stored bytes and persists immutable evidence of the attempt.
func (s *Service) Verify(ctx context.Context, key string) (metadata.Manifest, error) {
	m, _, err := s.verifyAndRecord(ctx, key, nil)
	return m, err
}

func newVerificationRecord(m metadata.Manifest, result VerificationResult, at time.Time) (VerificationRecord, string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return VerificationRecord{}, "", err
	}
	id := hex.EncodeToString(b)
	key := "verification_" + id + ".json"
	category := result.FailureCategory
	record := VerificationRecord{Version: 1, ID: id, RecordKey: key, BackupID: m.ID, BackupName: m.Name, Engine: m.Database.Engine, Database: m.Database.Name, Checksum: m.Checksum.Hash, Algorithm: "sha256", Status: result.Status, FailureCategory: category, Bytes: result.Bytes, CompletedAt: at.UTC()}
	return record, key, nil
}
