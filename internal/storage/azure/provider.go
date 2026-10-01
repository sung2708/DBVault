package azure

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/sung2708/DBVault/internal/storage"
	"io"
	"strings"
)

type Provider struct {
	Client    *azblob.Client
	Container string
	Namespace storage.Namespace
}

func (p *Provider) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	k, err := p.Namespace.Object(key)
	if err != nil {
		return err
	}
	star := azcore.ETag("*")
	_, err = p.Client.UploadStream(ctx, p.Container, k, storage.SizedReader(r, size), &azblob.UploadStreamOptions{BlockSize: 8 << 20, Concurrency: 1, AccessConditions: &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{IfNoneMatch: &star}}})
	return err
}
func (p *Provider) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	k, err := p.Namespace.Object(key)
	if err != nil {
		return nil, err
	}
	out, err := p.Client.DownloadStream(ctx, p.Container, k, nil)
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
func (p *Provider) Exists(ctx context.Context, key string) (bool, error) {
	k, err := p.Namespace.Object(key)
	if err != nil {
		return false, err
	}
	_, err = p.Client.ServiceClient().NewContainerClient(p.Container).NewBlobClient(k).GetProperties(ctx, nil)
	var e *azcore.ResponseError
	if errors.As(err, &e) && e.ErrorCode == "BlobNotFound" {
		return false, nil
	}
	return err == nil, err
}
func (p *Provider) Delete(ctx context.Context, key string) error {
	k, err := p.Namespace.Object(key)
	if err != nil {
		return err
	}
	_, err = p.Client.DeleteBlob(ctx, p.Container, k, nil)
	var e *azcore.ResponseError
	if errors.As(err, &e) && e.ErrorCode == "BlobNotFound" {
		return nil
	}
	return err
}
func (p *Provider) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	if strings.ContainsAny(prefix, "/\\\x00\r\n") {
		return nil, fmt.Errorf("invalid filename prefix")
	}
	full := p.Namespace.Prefix + prefix
	pager := p.Client.NewListBlobsFlatPager(p.Container, &azblob.ListBlobsFlatOptions{Prefix: &full})
	result := []storage.ObjectMetadata{}
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Segment.BlobItems {
			if o.Name == nil || o.Properties == nil {
				continue
			}
			if name, ok := p.Namespace.Name(*o.Name); ok {
				m := storage.ObjectMetadata{Key: name}
				if o.Properties.ContentLength != nil {
					m.Size = *o.Properties.ContentLength
				}
				if o.Properties.LastModified != nil {
					m.LastModified = *o.Properties.LastModified
				}
				if o.Properties.ETag != nil {
					m.ETag = string(*o.Properties.ETag)
				}
				result = append(result, m)
			}
		}
	}
	return result, nil
}
