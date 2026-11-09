package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// PublishManifest resolves a lost write response without discarding the archive.
func PublishManifest(ctx context.Context, store Provider, key string, data []byte) error {
	err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)))
	if err == nil {
		return nil
	}
	check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if r, readErr := store.Get(check, key); readErr == nil {
		defer r.Close()
		actual, readErr := io.ReadAll(io.LimitReader(r, int64(len(data))+1))
		if readErr == nil && bytes.Equal(actual, data) {
			return nil
		}
	}
	return fmt.Errorf("manifest publication unconfirmed for %s (archive preserved): %w", key, err)
}

const deleteSuffix = ".deleting"

// DeleteRegistered records intent before removing the manifest. Call under the chain lock.
func DeleteRegistered(ctx context.Context, store Provider, key, manifestSuffix string) error {
	manifest := key + manifestSuffix
	marker := manifest + deleteSuffix
	pending, err := store.Exists(ctx, marker)
	if err != nil {
		return err
	}
	if !pending {
		if err := store.Put(ctx, marker, strings.NewReader(key), int64(len(key))); err != nil {
			return err
		}
	}
	if err := store.Delete(ctx, manifest); err != nil {
		return err
	}
	if err := store.Delete(ctx, key); err != nil {
		return err
	}
	return store.Delete(ctx, marker)
}

func ResumeDeletes(ctx context.Context, store Provider, manifestSuffix string) error {
	objects, err := store.List(ctx, "")
	if err != nil {
		return err
	}
	suffix := manifestSuffix + deleteSuffix
	for _, object := range objects {
		key, ok := strings.CutSuffix(object.Key, suffix)
		if !ok {
			continue
		}
		// Registered entries must pass current dependency and retention checks.
		registered, err := store.Exists(ctx, key+manifestSuffix)
		if err != nil {
			return err
		}
		if !registered {
			if err := DeleteRegistered(ctx, store, key, manifestSuffix); err != nil {
				return err
			}
		}
	}
	return nil
}

func DeletePending(ctx context.Context, store Provider, key, manifestSuffix string) (bool, error) {
	return store.Exists(ctx, key+manifestSuffix+deleteSuffix)
}
