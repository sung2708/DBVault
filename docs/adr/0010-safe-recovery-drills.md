# ADR-0010: Fail-closed SQLite recovery drills and separate evidence

Status: Accepted — 2026-10-01

## Context

Normal restore intentionally overwrites operator-selected targets after confirmation.
A recovery drill requires a stronger invariant: it must not turn testing into a
production overwrite. Configured server/database strings alone do not prove
credential confinement, server identity or ownership. MySQL SQL dumps and
PostgreSQL custom archives can execute SQL; MongoDB remapping does not supply a
server/ownership attestation. Backup manifests are immutable registration objects.

## Decision

V1 exposes `recovery drill` only for SQLite and fails closed for server engines.
It requires explicit backup name and a new target path. Resolve source/target
parents, refuse ambiguous/equal/existing paths and any SQLite sidecars, create
the main target exclusively only after checksum and compatibility checks, and
check the created file's identity before restore/validation/cleanup. Keep parents
private under the existing trusted-operator/no-untrusted-concurrent-writer model.

Reuse the existing private verified snapshot and online SQLite restore; do not
reopen the artifact after verification or introduce a second restore stack.
Embedded version preflight uses an in-memory connection so production need not
exist; normal target preflight remains inside restore. Post-restore validation is
read-only `PRAGMA integrity_check` plus a catalog query. Dry-run performs preflight
without creating the target and is never passed recovery evidence.

Preserve targets by default and on failure/cancellation. Explicit cleanup removes
only this run's exact owned file after successful validation and when no SQLite
sidecars remain. Persist separate no-overwrite `.recovery.json` records, associated
by backup ID/name/SHA-256/engine/source; never rewrite a completed backup manifest.
Records contain stage evidence, not secrets or raw errors. Health validates them
and keeps history distinct from freshness/current integrity. No shell hooks,
force bypass, Docker dependency or ordinary restore Slack notification is added.

## Consequences

SQLite drills can prove actual restore and structural consistency, not application
semantics or completeness. Filesystem race resistance relies on operator-owned
directories and detects identity changes; it is not an adversarial sandbox.
History records require storage writes, are operator-controlled rather than
signed, and are not automatically pruned. Failed evidence publication makes the
command fail while its stage results retain actual restore/validation outcomes.
Native drills remain unsupported until an engine-specific isolation mechanism
and real integration evidence are implemented.
