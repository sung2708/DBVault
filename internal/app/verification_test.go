package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/storage"
)

type verificationStore struct {
	storage.Provider
	corruptArtifact   bool
	failArtifactRead  bool
	failEvidenceWrite bool
	shortArtifactRead bool
}

func (s verificationStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if s.failEvidenceWrite && strings.HasPrefix(key, "verification_") {
		return errors.New("injected evidence write failure")
	}
	if s.corruptArtifact && !strings.HasSuffix(key, ".meta.json") && !strings.HasPrefix(key, "verification_") {
		r = &flipFirstByte{Reader: r}
	}
	return s.Provider.Put(ctx, key, r, size)
}
func (s verificationStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.failArtifactRead && !strings.HasSuffix(key, ".meta.json") && !strings.HasPrefix(key, "verification_") {
		return nil, errors.New("injected artifact read failure")
	}
	r, err := s.Provider.Get(ctx, key)
	if err == nil && s.shortArtifactRead && !strings.HasSuffix(key, ".meta.json") && !strings.HasPrefix(key, "verification_") {
		return &shortReadCloser{Reader: io.LimitReader(r, 8), Closer: r}, nil
	}
	return r, err
}

type shortReadCloser struct {
	io.Reader
	io.Closer
}

type flipFirstByte struct {
	Reader io.Reader
	done   bool
}

func (r *flipFirstByte) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	if n > 0 && !r.done {
		p[0] ^= 0xff
		r.done = true
	}
	return n, e
}

func TestVerifyAfterBackupRecordsEvidenceAndHealth(t *testing.T) {
	s, _, _ := setup(t, "gzip")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	s.Config.Protection = &config.Protection{VerifyAfterBackup: true}
	result, err := s.BackupWithResult(context.Background(), "full", false)
	if err != nil || result.BackupStatus != "success" || result.Verification.Status != "verified" || result.Verification.Bytes != result.Manifest.Pipeline.Stored || result.Verification.RecordKey == "" {
		t.Fatal(result, err)
	}
	report, err := s.Health(context.Background(), false)
	if err != nil || report.Databases[0].Integrity != "verified" || report.Databases[0].LastVerifiedAt == nil {
		t.Fatal(report, err)
	}
	status := s.BuildStatus(report, nil, "", time.Now())
	if status.Integrity != "verified" || status.LastVerifiedAt == nil {
		t.Fatal(status)
	}
	if _, err = s.Verify(context.Background(), result.Manifest.Name); err != nil {
		t.Fatal(err)
	}
	report, err = s.Health(context.Background(), false)
	if err != nil || report.Databases[0].Integrity != "verified" {
		t.Fatal(report, err)
	}
}

func TestVerifyAfterBackupFailurePreservesArtifactAndRecordsFailure(t *testing.T) {
	for _, mode := range []string{"corrupt", "missing", "short", "evidence"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := setup(t, "gzip")
			s.Config.Database.Type = "postgres"
			s.Config.Health.MaxBackupAge = "12h"
			s.Config.Protection = &config.Protection{VerifyAfterBackup: true}
			notifier := &recordedNotifier{}
			s.Notifier = notifier
			base := s.Store
			injected := verificationStore{Provider: base, corruptArtifact: mode == "corrupt", failArtifactRead: mode == "missing", shortArtifactRead: mode == "short", failEvidenceWrite: mode == "evidence"}
			s.Store = injected
			result, err := s.BackupWithResult(context.Background(), "full", false)
			if err == nil || result.Manifest.Name == "" || result.BackupStatus != "success" {
				t.Fatalf("artifact state lost: %+v %v", result, err)
			}
			if exists, e := base.Exists(context.Background(), result.Manifest.Name); e != nil || !exists {
				t.Fatalf("artifact removed: %v %v", exists, e)
			}
			s.Store = base
			if mode == "evidence" {
				if result.Verification.Status != "verified" || result.Verification.EvidenceStatus != "failed" {
					t.Fatal(result.Verification)
				}
				return
			}
			if result.Verification.Status != "failed" || result.Verification.RecordKey == "" {
				t.Fatal(result.Verification)
			}
			wantVerification := "failed"
			if mode == "evidence" {
				wantVerification = "verified"
			}
			if len(notifier.events) != 1 || notifier.events[0].Status != "failed" || notifier.events[0].BackupStatus != "success" || notifier.events[0].VerificationStatus != wantVerification || strings.Contains(notifier.events[0].Error, "injected") {
				t.Fatal("notification did not preserve verification outcome safely", notifier.events)
			}
			report, e := s.Health(context.Background(), false)
			if e != nil || report.Databases[0].Integrity != "failed" {
				t.Fatal(report, e)
			}
		})
	}
}

func TestVerifyCancellationRecordsAndDoesNotReadAll(t *testing.T) {
	s, db, _ := setup(t, "none")
	db.data = bytes.Repeat([]byte("large-stream"), 1<<18)
	s.Config.Protection = &config.Protection{VerifyAfterBackup: true}
	ctx, cancel := context.WithCancel(context.Background())
	s.Observe = func(event Event) {
		if event.Stage == "verify" && event.State == "progress" {
			cancel()
		}
	}
	result, err := s.BackupWithResult(ctx, "full", false)
	if err == nil || fault.ExitCode(err) != 5 || result.Manifest.Name == "" || result.Verification.Status != "cancelled" {
		t.Fatal(result, err)
	}
	if exists, e := s.Store.Exists(context.Background(), result.Manifest.Name); e != nil || !exists {
		t.Fatal("cancelled verification removed stored artifact", e)
	}
	if result.Verification.RecordKey == "" {
		t.Fatal("cancelled verification evidence not persisted", result)
	}
}

func TestVerificationRecordRejectsBrokenIdentity(t *testing.T) {
	_, _, dir := setup(t, "none")
	path := filepath.Join(dir, "bad-record.json")
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := DecodeVerification(f); err == nil {
		t.Fatal("accepted incomplete verification record")
	}
}

func TestVerifyDetectsValidButWrongManifestChecksum(t *testing.T) {
	s, _, dir := setup(t, "gzip")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	m, err := s.Backup(context.Background(), "full", false)
	if err != nil {
		t.Fatal(err)
	}
	m.Checksum.Hash = strings.Repeat("0", 64)
	encoded, err := metadata.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, m.Name+".meta.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(context.Background(), m.Name); err == nil || fault.ExitCode(err) != 4 {
		t.Fatal("wrong manifest checksum accepted", err)
	}
	report, err := s.Health(context.Background(), false)
	if err != nil || report.Databases[0].Integrity != "failed" {
		t.Fatal(report, err)
	}
}

func TestHealthVerifyRetriesFailedEvidence(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	m, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	store := s.Store
	s.Store = verificationStore{Provider: store, failArtifactRead: true}
	if _, err := s.Verify(ctx, m.Name); err == nil {
		t.Fatal("expected storage read failure")
	}
	s.Store = store
	passive, err := s.Health(ctx, false)
	if err != nil || passive.Status != Critical {
		t.Fatalf("passive health: %+v, %v", passive, err)
	}
	active, err := s.Health(ctx, true)
	if err != nil || active.Status != Healthy || active.Databases[0].Integrity != "verified" || active.Databases[0].VerificationNote != "" {
		t.Fatalf("active health: %+v, %v", active, err)
	}
}

type healthReadOrder struct {
	storage.Provider
	keys []string
}

func (s *healthReadOrder) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	s.keys = append(s.keys, key)
	return s.Provider.Get(ctx, key)
}

func TestActiveHealthVerifiesBeforeReadingRecoveryHistory(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	s.Config.Database.Type = "postgres"
	s.Config.Health.MaxBackupAge = "12h"
	m, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	const verificationKey = "verification_legacy.json"
	const recoveryKey = "recovery_legacy.recovery.json"
	for _, key := range []string{verificationKey, recoveryKey} {
		if err := s.Store.Put(ctx, key, strings.NewReader("{}"), 2); err != nil {
			t.Fatal(err)
		}
	}
	store := &healthReadOrder{Provider: s.Store}
	s.Store = store
	report, err := s.Health(ctx, true)
	if err != nil || report.Status != Healthy || report.Databases[0].Integrity != "verified" {
		t.Fatalf("health = %+v, error = %v", report, err)
	}
	archive := slices.Index(store.keys, m.Name)
	recovery := slices.Index(store.keys, recoveryKey)
	if archive < 0 || recovery <= archive || slices.Contains(store.keys, verificationKey) {
		t.Fatalf("storage read order = %v", store.keys)
	}
}

func TestVerifyStopsAtFirstByteBeyondStoredSize(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t, "none")
	m, err := s.Backup(ctx, "full", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Delete(ctx, m.Name); err != nil {
		t.Fatal(err)
	}
	size := m.Pipeline.Stored + 4096
	if err := s.Store.Put(ctx, m.Name, bytes.NewReader(make([]byte, size)), size); err != nil {
		t.Fatal(err)
	}
	_, read, err := s.verifyStored(ctx, m.Name)
	if err == nil || read != m.Pipeline.Stored+1 {
		t.Fatalf("read %d bytes, expected %d and a size mismatch: %v", read, m.Pipeline.Stored+1, err)
	}
}
