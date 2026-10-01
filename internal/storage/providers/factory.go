package providers

import (
	gcstorage "cloud.google.com/go/storage"
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/storage"
	azureprovider "github.com/sung2708/DBVault/internal/storage/azure"
	gcsprovider "github.com/sung2708/DBVault/internal/storage/gcs"
	"github.com/sung2708/DBVault/internal/storage/local"
	s3provider "github.com/sung2708/DBVault/internal/storage/s3"
	"google.golang.org/api/option"
	"os"
)

func Open(ctx context.Context, c config.Storage) (storage.Provider, func() error, error) {
	closeFn := func() error { return nil }
	switch c.Type {
	case "local":
		p, err := local.New(c.Local.Path)
		if err != nil {
			return nil, closeFn, err
		}
		return p, p.Close, nil
	case "s3":
		ns, err := storage.NewNamespace(c.S3.Prefix)
		if err != nil {
			return nil, closeFn, err
		}
		opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(c.S3.Region)}
		if c.S3.AccessKeyEnv != "" {
			key, secret := os.Getenv(c.S3.AccessKeyEnv), os.Getenv(c.S3.SecretKeyEnv)
			if key == "" || secret == "" {
				return nil, closeFn, fmt.Errorf("storage.s3 credential environment variables are missing")
			}
			opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, os.Getenv("AWS_SESSION_TOKEN"))))
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil, closeFn, err
		}
		client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
			if c.S3.Endpoint != "" {
				o.BaseEndpoint = aws.String(c.S3.Endpoint)
				o.UsePathStyle = true
			}
		})
		return &s3provider.Provider{Client: client, Bucket: c.S3.Bucket, Namespace: ns, Encryption: c.S3.Encryption, KMSKeyID: c.S3.KMSKeyID}, closeFn, nil
	case "gcs":
		ns, err := storage.NewNamespace(c.GCS.Prefix)
		if err != nil {
			return nil, closeFn, err
		}
		opts := []option.ClientOption{gcstorage.WithJSONReads()}
		if c.GCS.CredentialsFileEnv != "" {
			file := os.Getenv(c.GCS.CredentialsFileEnv)
			if file == "" {
				return nil, closeFn, fmt.Errorf("storage.gcs credential file environment variable is missing")
			}
			opts = append(opts, option.WithCredentialsFile(file))
		}
		if c.GCS.Endpoint != "" {
			opts = append(opts, option.WithEndpoint(c.GCS.Endpoint))
		}
		client, err := gcstorage.NewClient(ctx, opts...)
		if err != nil {
			return nil, closeFn, err
		}
		return &gcsprovider.Provider{Client: client, Bucket: c.GCS.Bucket, Namespace: ns}, client.Close, nil
	case "azure":
		ns, err := storage.NewNamespace(c.Azure.Prefix)
		if err != nil {
			return nil, closeFn, err
		}
		endpoint := c.Azure.Endpoint
		if endpoint == "" {
			endpoint = "https://" + c.Azure.AccountName + ".blob.core.windows.net"
		}
		var client *azblob.Client
		if c.Azure.AccountKeyEnv != "" {
			key := os.Getenv(c.Azure.AccountKeyEnv)
			if key == "" {
				return nil, closeFn, fmt.Errorf("storage.azure account key environment variable is missing")
			}
			credential, e := azblob.NewSharedKeyCredential(c.Azure.AccountName, key)
			if e != nil {
				return nil, closeFn, fmt.Errorf("storage.azure account key is invalid")
			}
			client, err = azblob.NewClientWithSharedKeyCredential(endpoint, credential, nil)
		} else {
			credential, e := azidentity.NewDefaultAzureCredential(nil)
			if e != nil {
				return nil, closeFn, e
			}
			client, err = azblob.NewClient(endpoint, credential, nil)
		}
		if err != nil {
			return nil, closeFn, err
		}
		return &azureprovider.Provider{Client: client, Container: c.Azure.Container, Namespace: ns}, closeFn, nil
	}
	return nil, closeFn, fmt.Errorf("unsupported storage provider")
}
func Key(p storage.Provider, target string) (string, error) {
	if resolver, ok := p.(interface{ Key(string) (string, error) }); ok {
		return resolver.Key(target)
	}
	return target, storage.FlatKey(target)
}
