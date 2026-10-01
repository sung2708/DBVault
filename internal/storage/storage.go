package storage

import (
	"context"
	"io"
	"time"
)

type ObjectMetadata struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}
type Provider interface {
	Put(context.Context, string, io.Reader, int64) error
	Get(context.Context, string) (io.ReadCloser, error)
	Exists(context.Context, string) (bool, error)
	Delete(context.Context, string) error
	List(context.Context, string) ([]ObjectMetadata, error)
}
