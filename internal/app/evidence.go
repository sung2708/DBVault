package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/storage"
)

func evidencePrefix(kind, backupID string) string {
	return fmt.Sprintf("%sby_backup_%x_", kind, sha256.Sum256([]byte(backupID)))
}

type cachedEvidence[T any] struct {
	object storage.ObjectMetadata
	record T
}

// EvidenceCache is owned by one status watch. Only legacy evidence needs caching.
type EvidenceCache struct {
	storage config.Storage
	drills  map[string]cachedEvidence[DrillResult]
	checks  map[string]cachedEvidence[VerificationRecord]
}

func (c *EvidenceCache) reset(cfg config.Storage) {
	if c != nil && (c.drills == nil || !reflect.DeepEqual(c.storage, cfg)) {
		*c = EvidenceCache{
			storage: cfg,
			drills:  make(map[string]cachedEvidence[DrillResult]),
			checks:  make(map[string]cachedEvidence[VerificationRecord]),
		}
	}
}

func evidenceMatches(key, kind, currentPrefix string) (match, legacy bool) {
	legacy = !strings.HasPrefix(key, kind+"by_backup_")
	return legacy || strings.HasPrefix(key, currentPrefix), legacy
}

func readEvidence[T any](ctx context.Context, store storage.Provider, object storage.ObjectMetadata, cache map[string]cachedEvidence[T], decode func(io.Reader) (T, error)) (record T, err error, unavailable bool) {
	if entry, ok := cache[object.Key]; ok && entry.object == object {
		return entry.record, nil, false
	}
	reader, err := store.Get(ctx, object.Key)
	if err != nil {
		return record, err, true
	}
	defer reader.Close()
	record, err = decode(reader)
	if cache != nil && err == nil && ctx.Err() == nil {
		cache[object.Key] = cachedEvidence[T]{object: object, record: record}
	}
	return record, err, false
}
