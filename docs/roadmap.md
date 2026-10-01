# Project Roadmap

This document outlines the phased development roadmap for DBVault, based on an audit of the current repository state and prioritized engineering milestones.

---

## 1. Current State

The repository implements all four database engines and storage providers,
Slack, retention and persistent cron schedules. See
[verified status](implementation-status.md) for executed checks. Incremental and
differential recovery chains, client-side encryption, live-cloud validation and
published releases remain separate future work.

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
- [ ] **Container Packaging:** Official multi-arch Docker image containing DBVault and native client binaries (`pg_dump`, `mysqldump`).
- [x] **In-Process Scheduler:** Five-field cron, persistent CRUD, timezones, overlap prevention and cancellation.

---

## 5. Long-Term Horizons (Phase 4: Distributed & Enterprise Storage)

- [x] **MongoDB Adapter:** Native archives, quiesced-write contract and collection remapping.
- [x] **SQLite Adapter:** Consistent snapshots and online backup API restore.
- [x] **Google Cloud Storage (GCS):** Official SDK resumable uploads and emulator restore drills.
- [x] **Azure Blob Storage:** Official SDK block blobs and emulator restore drills.
- [ ] **Client-Side Envelope Encryption:** AES-256-GCM data encryption with AWS KMS / HashiCorp Vault key rotation.
- [ ] **Prometheus Metrics:** Integrated metrics exporter exposing backup durations, byte sizes, and failure counts.
