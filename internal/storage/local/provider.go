package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sung2708/DBVault/internal/pipeline"
	"github.com/sung2708/DBVault/internal/storage"
)

// Provider confines flat object keys using os.Root, including symlink resolution.
// The root must be private to the backup operator; untrusted concurrent writers
// are outside the filesystem threat model.
type Provider struct {
	root *os.Root
	path string
}

func New(path string) (*Provider, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &Provider{r, abs}, nil
}
func (p *Provider) Close() error { return p.root.Close() }
func ValidKey(key string) error {
	if key == "" || len(key) > 255 || key == "." || key == ".." || strings.ContainsAny(key, "/\\:\"<>|?*\x00\r\n") || strings.HasSuffix(key, ".") || strings.HasSuffix(key, " ") || strings.HasPrefix(key, ".") {
		return fmt.Errorf("storage key must be a flat, filesystem-safe filename")
	}
	for _, r := range key {
		if r < 32 {
			return fmt.Errorf("storage key contains a control character")
		}
	}
	base := strings.ToUpper(strings.Split(key, ".")[0])
	base = strings.NewReplacer("¹", "1", "²", "2", "³", "3").Replace(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
		return fmt.Errorf("reserved storage key")
	}
	return nil
}
func (p *Provider) Key(target string) (string, error) {
	// A bare name is an object key. Paths must resolve directly inside this root.
	if err := ValidKey(target); err == nil {
		return target, nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(p.path, abs)
	if err != nil {
		return "", err
	}
	return rel, ValidKey(rel)
}
func (p *Provider) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	tmp := ".tmp-" + hex.EncodeToString(b)
	f, err := p.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer p.root.Remove(tmp)
	defer f.Close()
	n, err := io.Copy(f, pipeline.Reader{Context: ctx, Source: r})
	if err != nil {
		return err
	}
	if size >= 0 && size != n {
		return fmt.Errorf("storage stream size mismatch")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Fail rather than replace an existing backup. IDs are random; a collision
	// or a concurrent writer must never overwrite a registered artifact.
	if _, err = p.root.Lstat(key); err == nil {
		return fmt.Errorf("storage object already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Hard-link publication is atomic and cannot replace an existing key, even
	// when another DBVault process publishes the same name concurrently.
	if err = p.root.Link(tmp, key); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		dir, e := p.root.Open(".")
		if e != nil {
			p.root.Remove(key)
			return e
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			p.root.Remove(key)
			return e
		}
	}
	return nil
}
func (p *Provider) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidKey(key); err != nil {
		return nil, err
	}
	f, err := p.root.Open(key)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = fmt.Errorf("object is not a regular file")
		}
		return nil, err
	}
	return f, nil
}
func (p *Provider) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := ValidKey(key); err != nil {
		return false, err
	}
	_, err := p.root.Stat(key)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (p *Provider) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidKey(key); err != nil {
		return err
	}
	err := p.root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (p *Provider) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	f, err := p.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	result := []storage.ObjectMetadata{}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(e.Name(), prefix) || ValidKey(e.Name()) != nil || e.Type()&os.ModeSymlink != 0 || e.IsDir() {
			continue
		}
		s, err := e.Info()
		if err != nil {
			return nil, err
		}
		if s.Mode().IsRegular() {
			result = append(result, storage.ObjectMetadata{Key: e.Name(), Size: s.Size(), LastModified: s.ModTime()})
		}
	}
	return result, nil
}
