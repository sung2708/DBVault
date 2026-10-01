# ADR-0004: Pluggable Storage Provider Abstraction

## Status

Proposed

## Context

Different deployment environments require backing up to different storage targets:
- Development and bare-metal environments typically back up to local paths or mounted NFS/SMB volumes.
- Cloud environments require direct streaming to Amazon S3, Google Cloud Storage, or Azure Blob Storage.

Tightly coupling backup logic to local filesystem APIs or AWS S3 SDKs harms maintainability and prevents multi-cloud support.

## Decision

We propose defining a unified `storage.Provider` interface in `internal/storage` that abstracts object storage semantics across local filesystems and cloud backends:

```go
type Provider interface {
    Put(ctx context.Context, key string, reader io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Exists(ctx context.Context, key string) (bool, error)
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string) ([]ObjectMetadata, error)
}
```

The core orchestration engine interacts exclusively with this interface.

## Alternatives Considered

- **Using Direct Filesystem Operations Only:** Relies on external sync tools (e.g. `rclone` or `aws s3 sync`) to move files off-box. Rejected as it introduces external tool dependencies and two-step backup lag.
- **Direct Cloud SDK calls in Application Engine:** Violates separation of concerns and complicates unit testing.

## Consequences

### Positive
- Allows seamless addition of new storage backends (Local, AWS S3, MinIO, GCS, Azure Blob).
- Simplifies testing through mock storage implementations without touching disk or cloud APIs.
- Enables consistent sidecar metadata manifest management across all storage backends.

### Negative
- Cloud providers have differing streaming upload constraints (e.g., minimum 5MB part sizes for S3 multipart uploads), requiring buffered chunking within the cloud provider implementation.
