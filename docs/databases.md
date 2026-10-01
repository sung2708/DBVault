# Database Adapters Reference

This document provides technical specifications for database adapters in DBVault, detailing their implementation status, native prerequisites, backup strategies, and known caveats.

The current binary implements PostgreSQL, Oracle MySQL 8.x, MongoDB and SQLite.
Full and selected PostgreSQL
logical dumps are implemented; incremental/differential/PITR are unsupported.
See [verified status](implementation-status.md) for executed restore-test evidence.

---

## Capability Summary Matrix

| Engine | Status | Native Tool(s) | Full Backup | Full Restore | Selective (Tables) | Incremental Strategy |
|---|:---:|---|:---:|:---:|:---:|---|
| **PostgreSQL** | Implemented | `pg_dump`, `psql`, `pg_restore` | Yes | Yes | Yes | Unsupported |
| **MySQL** | Implemented | `mysqldump`, `mysql` | Yes (InnoDB) | Yes | Backup only | Unsupported |
| **MongoDB** | Implemented | `mongodump`, `mongorestore` 100.x | Full | Full | One collection or excludes | Unsupported |
| **SQLite** | Implemented | Embedded modernc SQLite | Full | Full | Unsupported | Unsupported |

---

## 1. PostgreSQL Adapter

### Implementation Status
**Implemented** (Core relational engine for initial release).

### Native Tool Dependencies
- `pg_dump`: Generates logical SQL dumps or custom archive streams.
- `psql`: Executes SQL restore streams with `ON_ERROR_STOP=1`.
- `pg_restore`: Restores PostgreSQL custom archive format (`-Fc`).

### Connection Validation
Validated via a transient TCP dial and authentication handshake using the Go `database/sql` driver or direct `psql -c "SELECT 1"` query.

### Backup Strategy
DBVault spawns `pg_dump` with structured arguments:
```bash
pg_dump --host 127.0.0.1 --port 5432 --username postgres --dbname production_app \
  --format=custom --compress=0 --no-owner --no-privileges --no-password
```
- Injects `PGPASSWORD` through environment variables (never command-line flags).
- Streams stdout directly through gzip compression and SHA-256 calculation.

### Restore Strategy
Streams a decompressed custom archive into `pg_restore`:
```bash
pg_restore --dbname=production_app --exit-on-error --single-transaction \
  --no-owner --no-privileges --no-password
```

### Caveats & Limitations
- **Large Objects (BLOBs):** Ensure proper permissions when dumping databases utilizing large object OIDs.
- **Role Permissions:** The backup user must have `SELECT` privileges on all targeted tables and `USAGE` on all schemas.

---

## 2. MySQL Adapter

### Implementation Status
**Implemented for Oracle MySQL 8.x/InnoDB**. Server/dump-client release series
must match; MariaDB and cross-series restore are unsupported. Default option
files/login paths are ignored. MYSQL_PWD is passed in the child environment.
Selected-table backup is supported; selected restore and `--clean` are rejected.
Restore SQL may itself contain DROP statements and cannot roll back all DDL.
Prevent concurrent DDL during backup. See [ADR-0008](adr/0008-mysql-full-logical-strategy.md).

### Native Tool Dependencies
- `mysqldump`: Generates consistent logical SQL dumps.
- `mysql`: Executes SQL restore statements.

### Connection Validation
Validated via TCP socket handshake and `SELECT 1` query execution.

### Backup Strategy
DBVault executes `mysqldump` with options ensuring transactional consistency:
```bash
mysqldump --host=127.0.0.1 --port=3306 --user=root \
  --single-transaction --quick --routines --triggers production_db
```
- `--single-transaction`: Acquires a consistent snapshot for InnoDB tables without locking tables.
- `--quick`: Forces `mysqldump` to retrieve rows line-by-line rather than buffering entire tables in client memory.

### Restore Strategy
Streams decompressed SQL directly into `mysql`:
```bash
mysql --host=127.0.0.1 --port=3306 --user=root production_db
```

### Caveats & Limitations
- **Non-InnoDB Engines:** Preflight rejects nontransactional base tables; native --single-transaction does not guarantee their consistency.
- **GTID Mode:** The strategy explicitly uses --set-gtid-purged=OFF. Arbitrary extra_flags are rejected.
- **Authentication/TLS:** Use ssl_mode=require or verified TLS for caching_sha2_password over TCP. The adapter does not automatically retrieve an unauthenticated RSA server key.

---

## 3. MongoDB Adapter

### Implementation Status
**Implemented**, with the strategy in [ADR-0009](adr/0009-mongodb-sqlite-cloud-scheduling.md).

### Native Tool Dependencies
- `mongodump`: Creates BSON archive streams.
- `mongorestore`: Restores BSON streams into MongoDB collections.

### Strategy
- `mongodump --archive --db=NAME` streams directly into compression. Stop all
  writes throughout the dump and set `database.options.quiesced: true` to
  acknowledge this requirement; DBVault does not enforce a write lock.
- `include_collections` accepts one literal collection; `exclude_collections`
  accepts multiple exclusions. Table/schema options are not MongoDB selectors.
- Restore always limits namespaces to the source database, remaps to the
  configured destination when needed, and supports repeatable `--collection`.
  `--clean` adds `--drop`. Restore is not atomic across collections.
- Server major and Database Tools versions must match the source. Passwords
  use a temporary private YAML file rather than process arguments.
- TLS `require` and `verify-full` both verify certificates using system trust.
  Use `disable` only for trusted development networks. Custom CA configuration
  is outside the current schema.

---

## 4. SQLite Adapter

### Implementation Status
**Implemented**, with a pure Go SQLite driver; no `sqlite3` executable is needed.

### Native Tool Dependencies
- Embedded SQLite through `modernc.org/sqlite`.

### Strategy
- Set `database.database` to the source/destination filesystem path. Backups
  use `VACUUM INTO` to create a consistent snapshot, including committed WAL
  pages, then stream that snapshot into the common compression/storage pipeline.
- Temporary disk must hold the complete snapshot. Source files are opened
  read-only and must already exist.
- Restore validates the snapshot with `PRAGMA quick_check` and uses SQLite's
  online backup API against an existing destination. It respects SQLite locks
  and cancellation, and replaces the destination's contents. Stop application
  writes during restore. Selective restore and `--clean` are rejected.
- SQLite 3 snapshots are supported; incremental/differential are rejected.
