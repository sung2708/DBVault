# ADR-0012: Verified restore destinations, safety backups and separate history

Status: Accepted  
Date: 2026-10-02

## Context

Operators need to restore into a separate database or SQLite file, protect an
existing destination before overwriting, inspect restore outcomes and export
backups without running a database restore. These workflows must preserve the
existing native formats and checksum verification contract.

## Decision

Use one verified private snapshot throughout a restore or export. New destination
names contain UTC restore time and a random suffix. Explicit names are validated;
existing targets are refused. PostgreSQL and MySQL create databases through a
maintenance connection, SQLite exclusively creates a file, and MongoDB uses
namespace remapping with absence checks and lazy creation.

An opt-in safety backup clears selection filters, uses normal configured storage
and compression, and verifies the completed artifact before restore writes.
MongoDB requires operator-quiesced writes. Safety backups follow normal retention.

Store unique versioned restore history records separately from backup manifests
and recovery-drill evidence. Persist source identity, destination, selectors,
timings, operation status and validation without credentials or raw native errors.
History failure is reported while preserving the actual restore outcome.
Dry runs do not create destinations, safety backups or history records.

Export verified bytes to an exclusively created local file. Optional decompression
removes the outer codec and preserves the native engine format.

Terminal selection and progress remain optional presentation over the same
services; JSON, quiet, non-terminal and non-interactive modes never prompt.

## Consequences

Restore history is operational evidence, not proof of application data completeness
or an isolated recovery drill. Storage failures and process termination can prevent
history recording. Failed new targets are preserved; automatic rollback is avoided.
MongoDB has no atomic database reservation, so operators must prevent concurrent
creation and writes. Export and restore require temporary disk space.

See [restore workflows](../restore-workflows.md) for permissions and validation limits.
