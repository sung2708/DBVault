package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/pitr"
)

func (s *Service) nativeHealth(ctx context.Context, verify bool) (HealthReport, error) {
	h := DatabaseHealth{Name: s.Config.Database.Database, Engine: s.Config.Database.Type, BackupScope: "pitr", SourceIdentity: s.Config.Health.SourceIdentity, Status: Unknown, Integrity: "unknown", RestoreTest: "unknown"}
	finish := func(err error) (HealthReport, error) {
		return HealthReport{Status: h.Status, Databases: []DatabaseHealth{h}}, err
	}
	if h.SourceIdentity == "" {
		h.Reason = "Native source identity is not configured"
		return finish(fault.Wrap(fault.Configuration, "health.source_identity", fmt.Errorf("required for pitr monitoring")))
	}
	var maxAge time.Duration
	if s.Config.Health.MaxBackupAge != "" {
		var err error
		maxAge, err = time.ParseDuration(s.Config.Health.MaxBackupAge)
		if err != nil || maxAge <= 0 {
			h.Reason = "Invalid freshness policy"
			return finish(fault.Wrap(fault.Configuration, "health.max_backup_age", fmt.Errorf("must be a positive duration")))
		}
		seconds := maxAge.Seconds()
		h.MaxAgeSeconds = &seconds
	}
	native := pitr.Service{Config: s.Config, Store: s.Store}
	items, err := native.List(ctx)
	if err != nil {
		h.Reason = "Native metadata listing unavailable or invalid"
		return finish(fault.Wrap(fault.Storage, "native health list", err))
	}
	var latest *pitr.Record
	for _, m := range items {
		if m.Identity == h.SourceIdentity {
			copy := m
			latest = &copy
			break
		}
	}
	if latest == nil {
		h.Status = Critical
		h.Reason = "No registered native backup for the configured source identity"
		return finish(nil)
	}
	h.BackupName = latest.Name
	h.StoredBytes = latest.Size
	at := latest.Until.UTC()
	h.LastBackupAt = &at
	chain, err := native.Chain(ctx, latest.Name)
	if err != nil {
		h.Status = Critical
		h.Reason = "Native dependency chain is missing or invalid"
		return finish(fault.Wrap(fault.Integrity, "native health chain", err))
	}
	objects, err := s.Store.List(ctx, "pitr_")
	if err != nil {
		h.Reason = "Native artifact listing unavailable"
		return finish(fault.Wrap(fault.Storage, "native health artifacts", err))
	}
	sizes := map[string]int64{}
	for _, o := range objects {
		sizes[o.Key] = o.Size
	}
	for _, m := range chain {
		exists, e := s.Store.Exists(ctx, m.Name)
		if e != nil {
			h.Reason = "Native artifact availability could not be checked"
			return finish(fault.Wrap(fault.Storage, "native health artifact", e))
		}
		if !exists {
			h.Status = Critical
			h.Reason = "Native chain artifact is missing"
			h.ArtifactExists = &exists
			return finish(nil)
		}
		if size, ok := sizes[m.Name]; !ok || size != m.Size {
			h.Status = Critical
			h.Integrity = "failed"
			h.Reason = "Native chain artifact size does not match metadata"
			return finish(nil)
		}
	}
	exists := true
	h.ArtifactExists = &exists
	if verify {
		for _, m := range chain {
			if err = native.VerifyStored(ctx, m); err != nil {
				h.Reason = "Native stored-artifact verification unavailable"
				var typed *fault.Error
				if errors.As(err, &typed) && typed.Kind == fault.Integrity {
					h.Status = Critical
					h.Integrity = "failed"
					h.Reason = "Native stored-artifact verification failed"
				}
				return finish(err)
			}
		}
		h.Integrity = "verified"
		verifiedAt := s.now()
		h.LastVerifiedAt = &verifiedAt
	}
	now := s.now()
	if at.After(now) {
		h.Reason = "Native coverage timestamp is in the future; check clock synchronization"
		return finish(nil)
	}
	age := now.Sub(at)
	seconds := age.Seconds()
	h.AgeSeconds = &seconds
	if maxAge == 0 {
		h.Reason = "No backup freshness policy configured"
		return finish(nil)
	}
	stale := age > maxAge
	h.Stale = &stale
	if stale {
		h.Status = Critical
		h.Reason = "Native coverage is beyond the configured freshness limit"
		return finish(nil)
	}
	if !verify {
		h.Status = Warning
		h.Reason = "Native coverage is within policy; stored checksum verification was not requested"
	} else {
		h.Status = Healthy
		h.Reason = "Native coverage is within policy and the complete stored chain passed verification"
	}
	return finish(nil)
}
