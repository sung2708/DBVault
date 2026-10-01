package gcs

import (
	sdk "cloud.google.com/go/storage"
	"context"
	"errors"
	"fmt"
	"github.com/sung2708/DBVault/internal/storage"
	"google.golang.org/api/iterator"
	"io"
	"strings"
)

type Provider struct {
	Client    *sdk.Client
	Bucket    string
	Namespace storage.Namespace
}

func (p *Provider) object(key string) (*sdk.ObjectHandle, error) {
	k, err := p.Namespace.Object(key)
	if err != nil {
		return nil, err
	}
	return p.Client.Bucket(p.Bucket).Object(k), nil
}
func (p *Provider) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	o, err := p.object(key)
	if err != nil {
		return err
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	w := o.If(sdk.Conditions{DoesNotExist: true}).NewWriter(child)
	w.ChunkSize = 8 << 20
	n, err := io.Copy(w, r)
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("upload size mismatch")
	}
	if err != nil {
		cancel()
		w.Close()
		return err
	}
	return w.Close()
}
func (p *Provider) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	o, err := p.object(key)
	if err != nil {
		return nil, err
	}
	return o.NewReader(ctx)
}
func (p *Provider) Exists(ctx context.Context, key string) (bool, error) {
	o, err := p.object(key)
	if err != nil {
		return false, err
	}
	_, err = o.Attrs(ctx)
	if errors.Is(err, sdk.ErrObjectNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (p *Provider) Delete(ctx context.Context, key string) error {
	o, err := p.object(key)
	if err != nil {
		return err
	}
	err = o.Delete(ctx)
	if errors.Is(err, sdk.ErrObjectNotExist) {
		return nil
	}
	return err
}
func (p *Provider) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	if strings.ContainsAny(prefix, "/\\\x00\r\n") {
		return nil, fmt.Errorf("invalid filename prefix")
	}
	it := p.Client.Bucket(p.Bucket).Objects(ctx, &sdk.Query{Prefix: p.Namespace.Prefix + prefix})
	result := []storage.ObjectMetadata{}
	for {
		a, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if name, ok := p.Namespace.Name(a.Name); ok {
			result = append(result, storage.ObjectMetadata{Key: name, Size: a.Size, LastModified: a.Updated, ETag: a.Etag})
		}
	}
	return result, nil
}
