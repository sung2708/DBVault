# ADR-0007: Verified local foundation and PostgreSQL archive contract

## Status

Accepted for the current implementation, 2026-10-01.

## Context

ADRs 0001–0006 are proposals. The blueprint conflicts on PostgreSQL format:
architecture metadata uses `custom`, while the restore guide describes SQL.
Timestamp-only filenames can collide. Verifying a file then reopening it allows
the restored bytes to differ. Arbitrary native flags can change output format,
redirect files, or override credentials. Skipping checksums conflicts with the
master execution contract's restore safety requirements.

## Decision

- Keep the Go/Cobra, native-tool, streaming, provider-interface, environment-secret
  and one-shot external-scheduling direction of the proposals.
- Require Go 1.25+ for `os.Root` filesystem confinement, including symlink escapes.
- PostgreSQL uses uncompressed native custom archives (`pg_dump --format=custom
  --compress=0`), wrapped in none/gzip/zstd. `pg_restore` supports table/schema
  selection and `--clean --if-exists`, with one transaction and exit on error.
- Require PostgreSQL 10+ and pg_dump matching the server major. Restore to the
  same or newer server major only, with pg_restore at least the source pg_dump
  major. This conservative check does not prove application/schema compatibility.
- SHA-256 covers the exact compressed stored bytes. Checksums are mandatory.
  `--skip-verify` is deliberately unavailable.
- Before restore, hash stored bytes into a private temporary file; restore that
  same snapshot. Backup remains streaming without an intermediate dump file.
  Restore requires local space for one compressed artifact. Remove the temporary
  file on success, failure, and cancellation.
- Names use sanitized database name, UTC timestamp and a random 128-bit ID:
  `{database}_{YYYYMMDD}_{HHMMSS}_{id}.dump[.gz|.zst]`.
- Keep manifest version `1.0` and documented nested fields; add backup identity,
  backup type, completion time, application/tool version and storage provider.
  Unknown manifest fields/versions fail closed. There were no legacy binaries.
- Local keys are flat filenames. A completed manifest is the registration marker.
- Publish synced local files with an atomic hard link that cannot overwrite an
  existing key, then unlink the temporary name. This requires a filesystem with
  hard-link support (e.g. NTFS, ext4, APFS). Unsupported filesystems fail explicitly.
  Sync the containing directory on POSIX; Windows durability depends on the OS.
  Listing excludes temporary files and unregistered artifacts. Failed metadata
  registration attempts to remove the finalized artifact and reports cleanup errors.
- Local directories are private to the operator. Existing directories retain
  their permissions; DBVault does not change ownership or Windows ACLs.
- Reject nonempty `extra_flags`. Native flags must become explicit, validated
  options. Reject database connection strings and option-like database names.
- Retention groups by database engine/name; keep-count and keep-days protections
  combine by union, including the age boundary. Always protect each group's
  newest backup. With both policies disabled, delete nothing. Verify all backups
  before executing cleanup; any corrupted or malformed entry aborts cleanup.
- Destructive restore and explicit deletion require `--confirm`, except dry-run.
  Cleanup is authorized by its configured policy and supports `--dry-run`.
- Cancellation kills the directly invoked native process immediately through
  `exec.CommandContext`; a five-second WaitDelay bounds pipe cleanup. Descendant
  process-tree supervision is not currently provided. Only vendor binaries are
  supported, not arbitrary wrapper scripts.

## Consequences

The old SQL examples are blueprint examples, not supported PostgreSQL archive
names. SHA-256 detects corruption; it does not authenticate an attacker-controlled
artifact and manifest. Restore only operator-trusted backups. PostgreSQL custom
archives can contain arbitrary SQL, and selected tables may need dependencies
already present in the target. Retention does not yet support incremental chains.

Upstream reference: [PostgreSQL pg_restore documentation](https://www.postgresql.org/docs/current/app-pgrestore.html).
