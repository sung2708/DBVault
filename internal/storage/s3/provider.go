package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/sung2708/DBVault/internal/storage"
	"io"
	"strings"
	"time"
)

type Provider struct {
	Client               *sdk.Client
	Bucket               string
	Namespace            storage.Namespace
	Encryption, KMSKeyID string
	PartSize             int64
}

func (p *Provider) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	object, err := p.Namespace.Object(key)
	if err != nil {
		return err
	}
	input := &sdk.PutObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(object), Body: r, IfNoneMatch: aws.String("*")}
	if p.Encryption != "" {
		input.ServerSideEncryption = types.ServerSideEncryption(p.Encryption)
	}
	if p.KMSKeyID != "" {
		input.SSEKMSKeyId = aws.String(p.KMSKeyID)
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if size >= 0 && size < 5<<20 {
		// Small known-size objects are bounded before signing and publication;
		// this also keeps the SDK body seekable for authenticated HTTP uploads.
		b, e := io.ReadAll(io.LimitReader(r, size+1))
		if e != nil {
			return e
		}
		if int64(len(b)) != size {
			return fmt.Errorf("upload size mismatch")
		}
		input.Body = bytes.NewReader(b)
		_, err = p.Client.PutObject(ctx, input)
		return err
	}
	input.Body = storage.SizedReader(r, size)
	uploader := manager.NewUploader(p.Client, func(u *manager.Uploader) {
		u.PartSize = 128 << 20
		if p.PartSize > 0 {
			u.PartSize = p.PartSize
		}
		u.Concurrency = 1
		// We abort with an independent deadline: the manager otherwise attempts
		// cleanup with the already-cancelled upload context and ignores failure.
		u.LeavePartsOnError = true
	})
	_, err = uploader.Upload(ctx, input)
	if err != nil {
		var failed manager.MultiUploadFailure
		if errors.As(err, &failed) {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_, abortErr := p.Client.AbortMultipartUpload(cleanup, &sdk.AbortMultipartUploadInput{Bucket: input.Bucket, Key: input.Key, UploadId: aws.String(failed.UploadID())})
			var api smithy.APIError
			if abortErr != nil && !(errors.As(abortErr, &api) && api.ErrorCode() == "NoSuchUpload") {
				err = errors.Join(err, fmt.Errorf("abort incomplete multipart upload: %w", abortErr))
			}
		}
	}
	return err
}
func (p *Provider) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := p.Namespace.Object(key)
	if err != nil {
		return nil, err
	}
	out, err := p.Client.GetObject(ctx, &sdk.GetObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(object)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}
func missing(err error) bool {
	var e smithy.APIError
	return errors.As(err, &e) && (e.ErrorCode() == "NoSuchKey" || e.ErrorCode() == "NotFound")
}
func (p *Provider) Exists(ctx context.Context, key string) (bool, error) {
	object, err := p.Namespace.Object(key)
	if err != nil {
		return false, err
	}
	_, err = p.Client.HeadObject(ctx, &sdk.HeadObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(object)})
	if missing(err) {
		return false, nil
	}
	return err == nil, err
}
func (p *Provider) Delete(ctx context.Context, key string) error {
	object, err := p.Namespace.Object(key)
	if err != nil {
		return err
	}
	_, err = p.Client.DeleteObject(ctx, &sdk.DeleteObjectInput{Bucket: aws.String(p.Bucket), Key: aws.String(object)})
	return err
}
func (p *Provider) List(ctx context.Context, prefix string) ([]storage.ObjectMetadata, error) {
	if strings.ContainsAny(prefix, "/\\\x00\r\n") {
		return nil, fmt.Errorf("list prefix must be a filename prefix")
	}
	pager := sdk.NewListObjectsV2Paginator(p.Client, &sdk.ListObjectsV2Input{Bucket: aws.String(p.Bucket), Prefix: aws.String(p.Namespace.Prefix + prefix)})
	result := []storage.ObjectMetadata{}
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			if name, ok := p.Namespace.Name(aws.ToString(o.Key)); ok {
				result = append(result, storage.ObjectMetadata{Key: name, Size: aws.ToInt64(o.Size), LastModified: aws.ToTime(o.LastModified), ETag: aws.ToString(o.ETag)})
			}
		}
	}
	return result, nil
}
