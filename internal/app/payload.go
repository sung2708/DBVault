package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/sung2708/DBVault/internal/compression"
	"github.com/sung2708/DBVault/internal/delta"
	"github.com/sung2708/DBVault/internal/encryption"
	"github.com/sung2708/DBVault/internal/keymanager"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/pipeline"
)

func removeTemp(f *os.File) {
	if f != nil {
		f.Close()
		os.Remove(f.Name())
	}
}

// chainLock serializes incremental publication with deletion across CLI processes.
// A crashed owner leaves the lock for operator inspection; never steal a live lock.
func (s *Service) chainLock(ctx context.Context) (func(), error) {
	const key = "dbvault_chain.lock"
	if err := s.Store.Put(ctx, key, bytes.NewReader([]byte("DBVault chain operation in progress\n")), -1); err != nil {
		return nil, fmt.Errorf("backup chain locked or storage unavailable (inspect dbvault_chain.lock after a crashed operation): %w", err)
	}
	return func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.Store.Delete(c, key); err != nil && s.Logger != nil {
			s.Logger.Warn("backup chain lock could not be released; inspect dbvault_chain.lock before the next chain operation")
		}
	}, nil
}
func (s *Service) latestBase(ctx context.Context) (metadata.Manifest, error) {
	items, err := s.List(ctx, "")
	if err != nil {
		return metadata.Manifest{}, err
	}
	for _, m := range items {
		if m.Database.Engine == s.DB.Name() && m.Database.Name == s.Config.Database.Database && m.Database.Format == s.DB.Format() && reflect.DeepEqual(m.Database.IncludeTables, nonempty(s.Config.Database.Options.IncludeTables)) && reflect.DeepEqual(m.Database.ExcludeTables, nonempty(s.Config.Database.Options.ExcludeTables)) && reflect.DeepEqual(m.Database.IncludeCollections, nonempty(s.Config.Database.Options.IncludeCollections)) && reflect.DeepEqual(m.Database.ExcludeCollections, nonempty(s.Config.Database.Options.ExcludeCollections)) {
			return m, nil
		}
	}
	return metadata.Manifest{}, fmt.Errorf("no matching base backup; create a full backup first")
}
func nonempty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func validateBase(m, base metadata.Manifest) error {
	baseDatabase, currentDatabase := base.Database, m.Database
	baseDatabase.Version, currentDatabase.Version = "", ""
	if m.Delta == nil || base.ID != m.Delta.BaseID || base.Checksum.Hash != m.Delta.BaseHash || !reflect.DeepEqual(baseDatabase, currentDatabase) {
		return fmt.Errorf("incremental base identity or database mismatch")
	}
	depth := 0
	if base.Delta != nil {
		depth = base.Delta.Depth
	}
	if m.Delta.Depth != depth+1 {
		return fmt.Errorf("invalid incremental chain depth")
	}
	return nil
}

// VerifyChain checks stored integrity and dependency identities for the complete
// recovery chain before cleanup relies on a backup as a retained recovery point.
func (s *Service) VerifyChain(ctx context.Context, key string) error {
	m, err := s.Verify(ctx, key)
	if err != nil {
		return err
	}
	return s.dependencyChain(ctx, m, true)
}

// dependencyChain validates metadata and artifact existence/size without
// opening archives. Explicit verification additionally hashes every ancestor.
func (s *Service) dependencyChain(ctx context.Context, m metadata.Manifest, verify bool) error {
	for depth := 0; m.Delta != nil; depth++ {
		if depth >= 32 {
			return fmt.Errorf("incremental chain exceeds limit")
		}
		base, err := s.ReadManifest(ctx, m.Delta.BaseName)
		if err != nil {
			return err
		}
		if err = validateBase(m, base); err != nil {
			return err
		}
		exists, err := s.Store.Exists(ctx, base.Name)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("incremental base artifact is missing")
		}
		objects, err := s.Store.List(ctx, base.Name)
		if err != nil {
			return err
		}
		found := false
		for _, o := range objects {
			if o.Key == base.Name {
				found = true
				if o.Size != base.Pipeline.Stored {
					return fmt.Errorf("incremental base stored size mismatch")
				}
			}
		}
		if !found {
			return fmt.Errorf("incremental base was not listed")
		}
		if verify {
			quiet := *s
			quiet.Observe = nil
			if _, err = quiet.Verify(ctx, base.Name); err != nil {
				return err
			}
		}
		m = base
	}
	return nil
}

func (s *Service) materialize(ctx context.Context, key string, depth int) (*os.File, error) {
	if depth > 32 {
		return nil, fmt.Errorf("incremental chain exceeds 32 links or contains a cycle")
	}
	m, f, err := s.snapshot(ctx, key)
	if err != nil {
		return nil, err
	}
	defer removeTemp(f)
	return s.materializeSnapshot(ctx, m, f, depth)
}
func (s *Service) materializeSnapshot(ctx context.Context, m metadata.Manifest, f *os.File, depth int) (result *os.File, resultErr error) {
	if depth > 32 {
		return nil, fmt.Errorf("incremental chain exceeds limit")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var src io.Reader = pipeline.Reader{Context: ctx, Source: f}
	if m.Encryption != nil {
		if s.Config.Encryption == nil {
			return nil, fmt.Errorf("backup requires an encryption key mapping")
		}
		var r *encryption.Reader
		var err error
		if m.Encryption.Algorithm == encryption.ManagedAlgorithm {
			wrapper, e := keymanager.Resolve(ctx, s.Config.Encryption, m.Encryption.KeyID)
			if e != nil {
				return nil, e
			}
			r, err = encryption.NewManagedReader(ctx, src, wrapper, encryptionContext(m))
		} else {
			env := s.Config.Encryption.Keys[m.Encryption.KeyID]
			if env == "" {
				return nil, fmt.Errorf("backup encryption key ID is not configured")
			}
			key, e := encryption.Key(env)
			if e != nil {
				return nil, e
			}
			r, err = encryption.NewReader(src, key, encryptionContext(m))
		}
		if err != nil {
			return nil, err
		}
		src = r
	}
	// Authenticate the entire ciphertext before allowing decompression or restore.
	encoded, err := os.CreateTemp("", "dbvault-encoded-*")
	if err != nil {
		return nil, err
	}
	defer removeTemp(encoded)
	if _, err = io.Copy(encoded, src); err != nil {
		return nil, err
	}
	if _, err = encoded.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	c, err := compression.New(m.Pipeline.Compression, m.Pipeline.Level)
	if err != nil {
		return nil, err
	}
	reader, err := c.Decompress(pipeline.Reader{Context: ctx, Source: encoded})
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	out, err := os.CreateTemp("", "dbvault-payload-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			removeTemp(out)
		}
	}()
	if m.Delta != nil {
		base, err := s.ReadManifest(ctx, m.Delta.BaseName)
		if err != nil {
			return nil, err
		}
		if err := validateBase(m, base); err != nil {
			return nil, err
		}
		quiet := *s
		quiet.Observe = nil
		bf, err := quiet.materialize(ctx, base.Name, depth+1)
		if err != nil {
			return nil, err
		}
		defer removeTemp(bf)
		if err := delta.Decode(out, pipeline.Reader{Context: ctx, Source: reader}, bf, m.Pipeline.Raw); err != nil {
			return nil, err
		}
	} else {
		n, err := io.Copy(out, io.LimitReader(pipeline.Reader{Context: ctx, Source: reader}, m.Pipeline.Raw+1))
		if err != nil {
			return nil, err
		}
		if n != m.Pipeline.Raw {
			return nil, fmt.Errorf("uncompressed size mismatch")
		}
	}
	if _, err = out.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if m.Delta != nil {
		hash, n, err := pipeline.Hash(ctx, out, io.Discard)
		if err != nil {
			return nil, err
		}
		if hash != m.Delta.RawHash || n != m.Pipeline.Raw {
			return nil, fmt.Errorf("reconstructed dump checksum mismatch")
		}
		if _, err = out.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Bind archive identity, engine, selectors, codec and delta references to the
// wrapped data key. Sizes and stored checksum are only known after streaming.
func encryptionContext(m metadata.Manifest) []byte {
	v := struct {
		ID, Name, Type string
		CreatedAt      time.Time
		Database       metadata.Database
		Compression    string
		Level          int
		Delta          *metadata.Delta
		Encryption     *metadata.Encryption
	}{m.ID, m.Name, m.BackupType, m.CreatedAt, m.Database, m.Pipeline.Compression, m.Pipeline.Level, m.Delta, m.Encryption}
	data, _ := json.Marshal(v)
	return data
}
