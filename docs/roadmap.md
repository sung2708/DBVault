# Project Roadmap

This document outlines the phased development roadmap for DBVault, based on an audit of the current repository state and prioritized engineering milestones.

---

## 1. Current State

The repository implements all four database engines and storage providers,
Slack, retention and persistent cron schedules. See
[verified status](implementation-status.md) for executed checks.
Version v0.6.0 includes native WAL/binlog/oplog recovery chains, KMS/Vault key
providers, native scheduling/retention and independent freshness monitoring.
Differential backups and live-cloud validation remain future milestones. See
[logical delta chains and encrypted recovery operations](enhancements.md),
[native PITR](pitr.md) and [independent monitoring](independent-monitoring.md).

### Recovery operations

- [x] New dated restore destinations, destination safety backups, restore history
  and verified exports (`v0.5.0`).
- [x] Inline restore selection, source/destination plans and structured results
  (`v0.5.0`); JSON/automation never prompt.

- [x] SQLite recovery-drill CLI (`v0.3.0`) with isolated restore, structural validation,
  owned cleanup and immutable exact-backup health evidence.
- [x] PostgreSQL recovery drills in new Docker-isolated servers (`v0.4.0`).
- [x] MySQL/MongoDB recovery drills in new isolated Docker servers with fresh
  credentials, structural validation and real integration coverage.

### Available in Phase 0:
- [x] Comprehensive architectural blueprint and pipeline design.
- [x] Standardized configuration schema specification (`dbvault.yaml`).
- [x] Clean architecture package boundary definitions.
- [x] Core interface definitions (`database.Adapter`, `storage.Provider`, `compression.Compressor`).
- [x] Threat model and security policies.
- [x] Architecture Decision Records (ADRs 0001 - 0006).

---

## 2. Completed Phase 1: Minimum Viable Engine

Phase 1 delivered an end-to-end backup and restore workflow for PostgreSQL to local storage.

### Targeted Deliverables:
- [x] **Go Module:** Go 1.26+ (`github.com/sung2708/DBVault`).
- [x] **CLI:** backup, restore, test, list, verify, inspect, delete, cleanup, config, version.
- [x] **PostgreSQL:** custom archives with pg_dump/pg_restore and authenticated psql preflight.
- [x] **Streaming Pipeline:** pipe error propagation and stored-byte SHA-256.
- [x] **Compression:** none, gzip, zstd streaming codecs.
- [x] **Local Storage:** confined keys and no-overwrite atomic publication.
- [x] **Metadata:** validated versioned JSON sidecars.
- [x] **Preflight:** native dependency/version and authenticated server-version query.

---

## 3. Completed Phase 2: Relational Multi-Engine & Cloud Storage

- [x] **MySQL Adapter:** Oracle MySQL 8.x/InnoDB dump/restore, matching release-series compatibility, and real Docker restore drills.
- [x] **AWS S3 Storage Provider:** Official SDK multipart upload and conditional publication; LocalStack restore drills.
- [x] **Retention Cleanup:** per-database keep_days/keep_count protections and dry-run.
- [x] **Dry-Run:** backup preflight and verified restore preflight.

---

## 4. Mid-Term Milestones (Phase 3: High-Performance & Observability)

- [x] **Zstandard:** streaming github.com/klauspost/compress/zstd.
- [x] **Slack Notifications:** HTTPS completion/failure messages; delivery outages do not alter backup results.
- [x] **Local Container Packaging:** Four non-root runtime targets for PostgreSQL, MySQL, MongoDB and SQLite; builds and version/native-client checks executed locally.
- [x] **Container release workflow (`v0.4.0`):** Versioned GHCR images for four
  runtime targets, Linux amd64/arm64, published after intentional release tags.
- [ ] **First official container publication:** Execute the prepared workflow on
  the `v0.4.0` release workflow and verify registry access.
- [x] **In-Process Scheduler:** Five-field cron, persistent CRUD, timezones, overlap prevention and cancellation.

---

## 5. Long-Term Horizons (Phase 4: Distributed & Enterprise Storage)

- [x] **MongoDB Adapter:** Native archives, quiesced-write contract and collection remapping.
- [x] **SQLite Adapter:** Consistent snapshots and online backup API restore.
- [x] **Google Cloud Storage (GCS):** Official SDK resumable uploads and emulator restore drills.
- [x] **Azure Blob Storage:** Official SDK block blobs and emulator restore drills.
- [x] **Client-Side Envelope Encryption:** AES-256-GCM with environment-referenced wrapping keys and key rotation.
- [x] **Managed key providers:** AWS KMS / HashiCorp Vault Transit envelope encryption.
- [x] **Native increments and PITR:** PostgreSQL WAL, MySQL binlogs and MongoDB replica-set oplogs through separate `pitr` commands.
- [x] **Native scheduled capture and retention:** Source-specific parent selection, periodic baselines and verified whole-chain cleanup.
- [x] **Logical incremental chains:** Dump deltas, dependency verification and retention protections.
- [x] **Scheduled recovery:** Latest-backup drills with owned target cleanup and evidence.
- [x] **Prometheus Metrics:** Integrated metrics exporter exposing backup durations, byte sizes, and failure counts.
