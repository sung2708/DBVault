# ADR-0013: Encrypted logical delta chains and scheduled recovery operations

- Status: Accepted
- Date: 2026-10-06

## Decision

Extend the existing logical dump application layer with optional client-side
AES-256-GCM envelope encryption and same-offset 64 KiB delta chains. Keep native
engine WAL/binlog/oplog recovery contracts separate. Use environment-referenced
wrapping key IDs, fresh per-backup data keys, random wrapping nonces, sequence
nonces for data frames and an authenticated empty terminal frame. Bind archive
identity, creation time, database descriptor, codec and delta references to the
wrapped data key. Compression precedes encryption; SHA-256 covers stored bytes.

Validate ciphertext, trailers, sizes and all base identities/hashes before
destination preparation. Reconstruct into private temporary files with bounded
memory. Base manifests pin immutable IDs/checksums and reconstructed dump hashes.
Limit chains to 32 links. Protect all ancestors of retained backups, select
obsolete chains in child-before-base order, and verify complete stored chains
before cleanup. Reject explicit deletion and deletion previews of referenced
bases; serialize publication/deletion with a create-only
storage lock. No automatic stale-lock stealing is permitted.

Extend server recovery isolation to official preloaded MySQL and MongoDB images.
Pin image IDs, generate fresh credentials, disable networking and published
ports, prohibit host binds, validate ownership before commands/removal, stop
retained targets and preserve failure evidence. MySQL uses TLS and CHECK TABLE;
MongoDB uses authenticated in-container tooling, collection validation and
readability checks. No source server connection is required for a drill.

Extend durable scheduler jobs with operation, backup type and SQLite recovery
directory. Recurring recovery requires explicit authorization when added and
creates a fresh destination each run. Reuse the existing serialized scheduler
and normal recovery command/evidence/notifications.

Provide Prometheus text and an optional bounded HTTP server. Derive latest
backup gauges from manifests; optional immutable operation records supply
outcome, duration and byte counters. Metrics recording is disabled by default.

## Consequences

Logical dump generation still scans the full database, and offset changes can
limit incremental savings. Recovery and base comparison require temporary disk,
whose use grows with chain length. Periodic full backups are necessary.
Incremental deletion requires all participating binaries and external lifecycle
rules to honor dependencies. Crashed operations need operator lock inspection.

Old plaintext full backups remain supported. Old binaries cannot decode the
new manifest fields. Key rotation retains old key mappings until dependent
backups expire. Key material is never placed in metadata/argv/logs. Metadata
remains visible and records are operator-controlled; no signed attestation,
KMS/Vault provider or native point-in-time recovery is claimed.

Metrics records consume storage and scrapes scan the saved records. Failed
metrics persistence is advisory. The exporter needs trusted network access.
Structural drills do not establish application semantics or production RTO.

See [operator instructions](../enhancements.md) for configuration and commands.
