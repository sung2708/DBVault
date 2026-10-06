# Backup Operations & Pipeline Architecture

For client-side encryption, logical incremental chains, all-engine isolated
recovery drills, metrics and recurring recovery jobs, see [operator instructions](enhancements.md).

This document details the internal lifecycle, streaming pipeline, data integrity mechanisms, and operational best practices for executing backups with DBVault.

## Implemented contract

`dbvault health` evaluates the latest completed registered backup for the selected
database against optional `health.max_backup_age`. Freshness uses `created_at`
(dump/snapshot start), not completion time. Exactly the age limit remains fresh;
older backups are stale. Missing artifacts remain critical even when an older
backup exists. Archives without valid completed sidecars are not registered backups.
Default health reads metadata and checks existence/listed size; it does not hash
archives. `health --verify` actively verifies the latest matching artifact and its incremental ancestors.
`verify` and configured `protection.verify_after_backup` append immutable
stored-artifact verification records, so health/status can distinguish verified,
failed and unknown evidence for the exact latest backup. `recovery drill` provides separate evidence for all four engines through actual
isolated restore and structural validation; health associates records with the
exact selected backup ID/name/hash. Healthy backup does not mean a recovery drill
has passed. See [health](cli-reference.md#dbvault-health) and
[recovery drill](cli-reference.md#dbvault-recovery-drill).

All engines use the same compression, checksum and registration pipeline.
MySQL uses `.sql`, MongoDB `.archive`, and SQLite `.sqlite`, followed by the
selected compression extension. MongoDB requires stopped writes and an explicit
`quiesced` acknowledgement. SQLite first writes a consistent private snapshot
to temporary disk. All four storage backends publish conditionally and register
the manifest last. See [database strategies](databases.md) and [storage](storage.md).

PostgreSQL writes native custom archives using `pg_dump --format=custom
--compress=0`, wrapped by none/gzip/zstd and hashed after compression. Names are
`{sanitized_database}_{UTC_YYYYMMDD}_{HHMMSS}_{128bit_random_id}.dump[.gz|.zst]`.
The ID makes same-second backups collision resistant; display/list the actual
generated key before restoring it. The older timestamp-only examples below are
illustrative and do not match generated keys.

Local storage writes a randomly named private temporary file, syncs/closes it,
publishes with an atomic hard link without replacing an existing key, then
removes the temporary name. Hard-link support is required. Completed `.meta.json`
manifests register successful backups; list/retention ignore orphan artifacts.
Failure to register metadata triggers artifact deletion, with cleanup failures
reported. A host crash between publication and registration can leave an orphan.

Native client/server major versions must match for PostgreSQL backup. The binary
supports PostgreSQL 10+. Ctrl+C terminates the private process scope; pipe cleanup
has a five-second limit. Vendor wrapper scripts/process trees are not supported.
See [ADR-0007](adr/0007-verified-local-foundation.md).

---

## 1. Backup Lifecycle Overview

Every backup operation executed by DBVault progresses through nine deterministic phases:

```text
1. Configuration Loading & Flag Merging
         ↓
2. Credential Resolution (Environment Variable Extraction)
         ↓
3. Dependency Validation (configured, PATH-resolved or platform-discovered tools)
         ↓
4. Connection Validation (Pre-flight Ping)
         ↓
5. Native Backup Process Invocation (pg_dump / mysqldump)
         ↓
6. Streaming Pipeline (io.Pipe + Compression Engine)
         ↓
7. Cryptographic Checksumming (crypto/sha256 calculation)
         ↓
8. Storage Destination Write (Local filesystem / S3 Multipart)
         ↓
9. Atomic Metadata Registration (.meta.json manifest generation)
10. Optional Stored-Artifact Verification (read back + SHA-256 evidence record)
```

---

## 2. Zero-Spool Streaming Pipeline

Traditional backup scripts dump database content into an intermediate raw file on local disk before compressing and moving it to storage. This approach requires $2\times$ to $3\times$ disk overhead and risks filling storage volumes during large operations.

DBVault uses an **in-memory streaming pipe** (`io.Pipe`):

```text
[pg_dump stdout]
       │ (Raw SQL stream)
       ▼
 [io.PipeWriter]  ──►  [io.PipeReader]
                             │
                             ▼
                    [gzip / zstd Compressor]
                             │
                             ▼
                    [io.TeeReader] ──► [SHA-256 Hasher]
                             │ (Compressed bytes)
                             ▼
                   [Storage Provider.Put()]
                             │
                             ▼
                  [Local Disk / Cloud Object]
```

### Memory Footprint Guarantee:
Memory usage is bounded by standard Go buffer sizes (typically 32KB to 64KB per pipe). A 500GB database backup consumes the same minimal RAM footprint as a 10MB backup ($O(1)$ memory consumption).

---

## 3. Temporary Files and Atomic Writes

For local storage operations:
1. Backups are written to a temporary staging file: `{filename}.tmp.{uuid}`.
2. The destination directory is created with restricted POSIX permissions (`0700`).
3. Once the native tool exits successfully and buffers are flushed, the file is synced and atomically published with a no-overwrite hard link; the temporary name is removed.
4. If an error occurs or a cancellation signal is received, the temporary file is unlinked immediately.

---

## 4. Cryptographic Integrity: SHA-256 Checksumming

As compressed bytes pass through the pipeline, an `io.TeeReader` passes every byte into `crypto/sha256.New()`. 
When the stream closes:
- The SHA-256 hex digest is finalized.
- The digest is embedded in the atomic sidecar metadata manifest (`.meta.json`).
- This is the checksum calculated while writing; it does not establish that stored bytes can later be read.
- `protection.verify_after_backup: true` reads the published artifact through the normal storage `Get` API, streams it through the shared SHA-256 verifier and appends a separate immutable verification record. It never rewrites the completed manifest.
- When a restore command is issued, DBVault computes the checksum of the archive and compares it with this manifest before restoring data.

Post-backup verification reads every stored byte. For cloud storage this adds a
full object read after upload, which costs time and provider read/API charges and
may incur network egress. If required verification fails, the command fails but
keeps the registered artifact and records the outcome when storage permits.
Verification proves stored-byte integrity, not that a database can be restored;
only a recovery drill tests an actual restore.

---

## 5. Backup Naming Conventions

Backup archives follow a deterministic timestamp naming pattern:

```text
{sanitized_database_name}_{UTC_YYYYMMDD}_{HHMMSS}_{random_128bit_id}.{extension}
```

**Examples:**
- PostgreSQL (gzip): `production_db_20261001_020000.dump.gz`
- MySQL (gzip): `store_db_20261001_020000_<id>.sql.gz`
- Sidecar Manifest: `production_db_20261001_020000.dump.gz.meta.json`

Metadata records include/exclude table filters so a selected-table backup is not
mistaken for an unfiltered full database snapshot. `backup_type: full` describes
the strategy applied to the selected scope, not an incremental chain.

---

## 6. Failure Recovery & Signal Handling

DBVault handles operational interruptions gracefully:
- **SIGINT / SIGTERM:** The orchestrator catches cancellation signals and calls `cancel()` on the operation's `context.Context`.
- **Subprocess Termination:** The direct native child is killed immediately by context cancellation; pipe cleanup has a five-second WaitDelay.
- **Artifact Cleanup:** DBVault intercepts failures and unlinks any partial files or objects in storage, ensuring that broken backups are never left behind.

---

## 7. Handling Large Databases (> 100 GB)

When backing up very large datasets:
1. **Timeouts:** Ensure CLI timeouts are adjusted:
   ```bash
   dbvault backup --config dbvault.yaml --timeout 8h
   ```
2. **Network Throughput:** For remote databases, ensure database server bandwidth is sufficient to avoid TCP timeouts.
3. **Compression Level:** Use gzip level `1` or `3`, or zstd, when compression CPU time limits streaming throughput.
