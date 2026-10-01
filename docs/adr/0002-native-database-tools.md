# ADR-0002: Leverage Native Database Client Binaries for Dumping and Restoration

## Status

Proposed

## Context

To back up and restore relational databases (PostgreSQL, MySQL) and document stores (MongoDB), an application can either:
1. Re-implement wire-protocol querying in pure Go (e.g. running `SELECT *` over TCP and encoding custom CSV/JSON/SQL files).
2. Or invoke vendor-maintained, highly optimized native client utilities (`pg_dump`, `pg_restore`, `mysqldump`, `mysql`, `mongodump`).

Re-implementing logical database dumping from scratch is fraught with edge cases: handling schema dependencies, sequences, stored procedures, triggers, custom types, character encodings, and transaction isolation levels across database versions.

## Decision

We propose delegating physical and logical database dumping and restoration to verified native database client utilities (`pg_dump`, `mysqldump`, `psql`, `mysql`). 

DBVault acts as an orchestrator: resolving client tools from explicit configuration, `PATH`, and bounded platform installation locations; validating their versions; generating safe structured arguments; managing process lifecycles via `os/exec.CommandContext`; and intercepting stdout/stdin streams. `dbvault init` stores discovered absolute paths so later operations use the same installation.

## Alternatives Considered

- **Pure-Go Driver Extraction:** Querying tables using standard Go SQL drivers (`pgx`, `go-sql-driver/mysql`). This approach cannot reliably extract database schemas, triggers, indexes, and extensions across database engine versions without recreating massive database-specific logic.
- **Direct Filesystem Snapshotting:** Taking snapshots of underlying data directories. This requires storage-level integration (e.g. ZFS/LVM) and fails to provide cross-platform logical backups.

## Consequences

### Positive
- Guaranteed compatibility with database features, versions, and transactional snapshot semantics (e.g., PostgreSQL MVCC snapshots and MySQL `--single-transaction`).
- Eliminates thousands of lines of maintenance-heavy reverse-engineering code for complex database schemas.
- Vendor utilities are heavily tested, optimized, and maintained by upstream database teams.

### Negative
- Requires target hosts or container images to have corresponding native client utilities installed. Custom locations can be configured explicitly; DBVault does not modify the host `PATH`.
- Version divergence between client utilities and database servers must be monitored.
