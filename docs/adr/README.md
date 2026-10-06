# Architecture Decision Records (ADRs)

This directory contains the Architecture Decision Records for DBVault. ADRs capture significant architectural decisions along with their context, rationale, alternatives considered, and consequences.

---

## Index of Decisions

| ADR | Title | Status | Date |
|---|---|:---:|---|
| [ADR-0001](0001-use-go.md) | Use Go as the Primary Implementation Language | Proposed | 2026-10-01 |
| [ADR-0002](0002-native-database-tools.md) | Leverage Native Database Client Binaries for Dumping and Restoration | Proposed | 2026-10-01 |
| [ADR-0003](0003-streaming-backup-pipeline.md) | In-Memory Streaming Pipeline with Zero Intermediate Disk Spooling | Proposed | 2026-10-01 |
| [ADR-0004](0004-storage-abstraction.md) | Pluggable Storage Provider Abstraction | Proposed | 2026-10-01 |
| [ADR-0005](0005-secret-management.md) | Environment-Driven Secret Management and Process Isolation | Proposed | 2026-10-01 |
| [ADR-0006](0006-scheduling-strategy.md) | External OS/Orchestrator Scheduling with Future In-Process Daemon | Proposed | 2026-10-01 |
| [ADR-0007](0007-verified-local-foundation.md) | Verified local foundation and PostgreSQL archive contract | Accepted | 2026-10-01 |
| [ADR-0008](0008-mysql-full-logical-strategy.md) | Oracle MySQL full logical strategy | Accepted | 2026-10-01 |
| [ADR-0009](0009-mongodb-sqlite-cloud-scheduling.md) | MongoDB, SQLite, cloud, notifications and scheduling | Accepted | 2026-10-01 |
| [ADR-0010](0010-safe-recovery-drills.md) | Fail-closed SQLite recovery drills and separate evidence | Accepted | 2026-10-01 |
| [ADR-0011](0011-postgresql-container-recovery.md) | PostgreSQL recovery in an owned network-isolated Docker server | Accepted | 2026-10-02 |
| [ADR-0012](0012-restore-workflows-and-history.md) | Verified restore destinations, safety backups and separate history | Accepted | 2026-10-02 |
| [ADR-0013](0013-encrypted-incremental-recovery-operations.md) | Encrypted logical delta chains, metrics and scheduled recovery | Accepted | 2026-10-06 |
| [ADR-0014](0014-native-pitr-managed-keys.md) | Native log chains, PITR and managed envelope keys | Accepted | 2026-10-06 |
