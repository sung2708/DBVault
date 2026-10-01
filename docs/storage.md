# Storage Providers Reference

This document describes the storage backend providers supported by DBVault, their configuration options, security considerations, and lifecycle behaviors.

Local, S3, GCS and Azure providers are implemented with official SDKs. Local storage uses
flat keys confined by `os.Root`, requires a private operator-controlled root and
a filesystem supporting hard links, and refuses to replace existing objects.
Only completed manifests register backups for listing/cleanup. Existing directory
permissions are unchanged; Windows requires operator-managed ACLs.

---

## Storage Provider Summary

| Provider | Status | Streaming Support | Atomic Writes | Retention Pruning |
|---|:---:|:---:|:---:|:---:|
| **Local Filesystem** | Implemented | Yes (`os.File`) | Yes (no-overwrite hard link) | Yes |
| **AWS S3** | Implemented | Multipart | Conditional publication | CLI / service lifecycle |
| **Google Cloud Storage (GCS)** | Implemented | Resumable | DoesNotExist condition | CLI / service lifecycle |
| **Azure Blob Storage** | Implemented | Block blob | Conditional block-list commit | CLI / service lifecycle |

---

## 1. Local Storage Provider

### Status: Implemented

The Local Storage provider writes backup archives and sidecar metadata manifests directly to the host filesystem or mounted network volumes (NFS, SMB, CephFS).

### Configuration Options
```yaml
storage:
  type: local
  local:
    path: /var/backups/dbvault    # Destination directory
    permissions: "0700"          # Directory permissions (octal string)
```

### Operational Mechanics
1. **Directory Initialization:** If the target directory does not exist, DBVault creates it with the permission mode configured (default: `0700`, accessible only by the running user).
2. **Atomic Writes:** Data is written to a private random temporary file. Once streaming succeeds, the file is synced/closed and published with a no-overwrite hard link. The temporary name is removed and the directory synced on POSIX.
3. **Failure Cleanup:** If the backup stream terminates unexpectedly or is canceled, the temporary file is unlinked automatically.

---

## 2. AWS S3 Provider

### Status: Implemented

The AWS S3 provider streams compressed backups directly into an Amazon S3 bucket or S3-compatible object storage (MinIO, Ceph, Cloudflare R2) using multipart upload.

### Configuration Specification
```yaml
storage:
  type: s3
  s3:
    bucket: my-production-backups
    region: us-east-1
    prefix: database/
    endpoint: ""                 # Optional: MinIO / Ceph endpoint
    access_key_env: AWS_ACCESS_KEY_ID
    secret_key_env: AWS_SECRET_ACCESS_KEY
```

### Security & Credential Strategy
- Credentials must **never** be hardcoded. They are loaded dynamically from environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) or IAM Role instance profiles when running on EC2 or EKS.
- Default endpoints use HTTPS. An explicit custom endpoint can use HTTP for
  development emulators; configure HTTPS for production.
- Omit custom credential env names to use the AWS default credential chain.
  `AWS_SESSION_TOKEN` is honored. Optional `encryption: AES256` or `aws:kms`
  requests server-side encryption; `kms_key_id` selects a KMS key.
- Unknown-size streams use 128 MiB parts with one worker; 10,000 parts limits
  an archive to roughly 1.22 TiB. Failed/cancelled multipart uploads are aborted
  with an independent ten-second context; abort failures are reported. Configure
  lifecycle expiration of incomplete uploads after crashes or lost permissions.

---

## 3. Google Cloud Storage (GCS) Provider

### Status: Implemented

The GCS provider streams through the official Go SDK with 8 MiB chunks.
Omit `credentials_file_env` to use Application Default Credentials. Uploads
require the object not to exist. Interrupted sessions are not resumed after
process restarts. `STORAGE_EMULATOR_HOST` is intended only for integration tests.

### Configuration Specification
```yaml
storage:
  type: gcs
  gcs:
    bucket: my-company-gcs-backups
    prefix: dbvault/
    credentials_file_env: GOOGLE_APPLICATION_CREDENTIALS
```

---

## 4. Azure Blob Storage Provider

### Status: Implemented

The Azure provider streams block blobs through the official Go SDK using one
worker and 8 MiB blocks. Omit `account_key_env` to use DefaultAzureCredential
(including managed identities). Final commit refuses to replace an existing
object. Uncommitted blocks after failure follow Azure's service expiration.

### Configuration Specification
```yaml
storage:
  type: azure
  azure:
    container: dbvault-backups
    account_name: mystorageaccount
    account_key_env: AZURE_STORAGE_KEY
```

All cloud targets must already contain the configured bucket/container. Prefixes
are normalized relative object paths; command `--target` values are flat backup
filenames, not URLs or prefixed keys. Listing and retention remain confined to
the prefix. Credentials require read/write/list/delete permissions for that
scope. Metadata is registered last; an upload without its manifest is not a
completed backup. A process crash can leave an unregistered object for an
operator to inspect and remove. See [ADR-0009](adr/0009-mongodb-sqlite-cloud-scheduling.md)
for tested guarantees and live-cloud verification limits.
