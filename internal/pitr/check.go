package pitr

import (
	"context"
	"fmt"
	"io"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/pipeline"
)

// Chain exposes validated ancestry to storage-only monitors; no source connection.
func (s *Service) Chain(ctx context.Context, name string) ([]Record, error) {
	return s.chain(ctx, name)
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
