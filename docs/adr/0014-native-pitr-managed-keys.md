# ADR-0014: Native log chains, PITR and managed envelope keys

Status: Accepted

Date: 2026-10-06

## Context

Logical dump deltas reduce stored bytes but still read a full database dump. Recovery to a timestamp requires engine logs and a consistent baseline. Environment wrapping keys also require operators to handle raw key material.

## Decision

Provide separate `pitr base`, `capture`, `list` and `restore` commands with immutable baseline/log archives and validated parent hashes, source identities, versions and cursors. PostgreSQL uses a physical `pg_basebackup` and closed archived WAL segments. MySQL uses an all-database locked dump and closed binary logs, preserving GTID mode. MongoDB uses a quiesced full dump and majority-committed replica-set oplog increments. Captures read logs rather than creating another full dump.

Verify every archive and authenticate encrypted content before destination writes. Restore only into a fresh PostgreSQL directory or independent empty MySQL/MongoDB instance with full database visibility. Timestamps are exclusive whole-second UTC boundaries. PostgreSQL restoration prepares recovery configuration; starting a compatible isolated server and confirming the recovery target remains an explicit operator step.

Add envelope format v2 with a fresh data key wrapped by AWS KMS or Vault Transit. Bind wrapping and stream authentication to backup metadata. Keep v1 environment-key decryption compatible. Provider credentials use AWS's credential chain or an environment-referenced Vault token; remote keys remain with the provider.

## Consequences

Native backups require engine-specific source setup, log retention and baseline maintenance windows. PostgreSQL requires local archive access and rejects external tablespaces; MongoDB requires drained writers during the baseline. A changed identity, version, timeline, GTID mode, rollback or missing cursor requires a new baseline. Native scheduler jobs reload credentials and select a chain for the authenticated source. Baseline refresh and post-publication cleanup are explicit job settings. Retention expires whole chains after verification, retaining the newest chain per source. A storage lock serializes mutations and restoration. Native recovery-drill and metrics integration remain future work.

Managed keys depend on remote availability and operator-configured permissions. DBVault does not provision keys or renew Vault tokens. Key rotation preserves prior versions needed for existing archives. Local integration fixtures exercise Vault Transit and LocalStack KMS; real cloud IAM must be configured separately.

See [PITR](../pitr.md) and [managed keys](../managed-keys.md) for configuration and operational contracts.
