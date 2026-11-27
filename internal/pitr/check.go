package pitr

import (
	"context"
	"fmt"
	"io"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/pipeline"
)

// CatalogChain validates ancestry using manifests from ReadCatalog.
func CatalogChain(ctx context.Context, items []Record, name string) ([]Record, error) {
	byName := make(map[string]Record, len(items))
	for _, m := range items {
		byName[m.Name] = m
	}
	return walkChain(ctx, name, func(ctx context.Context, key string) (Record, error) {
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		m, ok := byName[key]
		if !ok {
			return Record{}, fmt.Errorf("PITR manifest %s missing from catalog", key)
		}
		return m, nil
	})
}

// VerifyStored checks ciphertext/clear stored bytes without needing a decryption
// key, writing evidence, unpacking data or mutating the recovery destination.
func (s *Service) VerifyStored(ctx context.Context, m Record) error {
	if err := m.validate(); err != nil {
		return fault.Wrap(fault.Integrity, "native metadata", err)
	}
	r, err := s.Store.Get(ctx, m.Name)
	if err != nil {
		return fault.Wrap(fault.Storage, "native archive read", err)
	}
	defer r.Close()
	hash, n, err := pipeline.Hash(ctx, io.LimitReader(r, m.Size+1), io.Discard)
	if err != nil {
		return fault.Wrap(fault.Storage, "native archive read", err)
	}
	if n != m.Size || hash != m.Hash {
		return fault.Wrap(fault.Integrity, "native archive checksum", fmt.Errorf("stored bytes do not match metadata"))
	}
	return nil
}
