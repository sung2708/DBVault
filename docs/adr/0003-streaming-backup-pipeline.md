# ADR-0003: In-Memory Streaming Pipeline with Zero Intermediate Disk Spooling

## Status

Proposed

## Context

When dumping large databases (tens to hundreds of gigabytes), naive backup scripts write the uncompressed SQL dump to a temporary file on the local filesystem, invoke a compression utility (like `gzip`) creating a second file, compute a checksum creating a third file, and finally upload the result to remote storage.

This approach introduces severe operational liabilities:
1. It requires $2\times$ to $3\times$ the database's size in free local disk space.
2. It increases disk I/O load, which can saturate the host storage system and degrade production database performance.
3. Disk write stalls can cause database snapshot timeouts.

## Decision

We propose implementing an in-memory streaming pipeline utilizing Go's `io.Pipe`, `io.TeeReader`, and streaming compression writers (`compress/gzip`).

Data emitted from the native tool's `stdout` will stream directly into a pipe, pass through the compression writer, pass into a streaming SHA-256 hasher, and write straight to the storage destination (local file or S3 multipart upload) without ever staging the uncompressed data on disk.

## Alternatives Considered

- **Staged File Pipeline:** Writing intermediate files to `/tmp` and compressing sequentially. Rejected due to excessive storage overhead and failure risk when `/tmp` runs out of space.
- **Shell Piping (`pg_dump | gzip > file`):** Spawning a shell with piped commands. Rejected due to security risks (shell injection), loss of fine-grained error reporting, and platform incompatibility on Windows.

## Consequences

### Positive
- $O(1)$ memory consumption: constant RAM footprint regardless of database size.
- Zero local disk space required for staging when streaming to cloud storage targets.
- Simultaneous compression and cryptographic hashing during a single I/O pass.

### Negative
- If a network error interrupts a stream to cloud storage, the backup cannot be resumed from disk; it must be retried or utilize resumable multipart chunking.
