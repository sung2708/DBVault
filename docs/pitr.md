# Native log backups and PITR

Native archives are separate from the existing database-scoped logical dump and
logical delta formats. `pitr base` creates an instance-wide baseline;
`pitr capture --parent NAME` reads new engine logs without dumping application
tables again. Native objects are immutable `pitr_*.tar[.enc]` archives with
`.pitr.json` records. Logical cleanup does not remove them.

## Engine prerequisites

| Engine | Baseline | Incremental capture | Prerequisites |
| --- | --- | --- | --- |
| PostgreSQL | `pg_basebackup`, physical cluster | Closed WAL segments | Matching client/server major; replication privileges; `wal_level=replica`, `archive_mode=on`, a working `archive_command`; archive directory accessible to DBVault |
| Oracle MySQL 8.x | All databases via `mysqldump --source-data=2 --lock-all-tables` | Remote raw closed binlogs via `mysqlbinlog` | Binary logging; backup/read-lock and log-rotation/replication privileges; matching restore release series |
| MongoDB replica set | Full `mongodump --archive --oplog` | BSON ranges from `local.oplog.rs` | Database Tools 100.x; privileges to inspect replica-set identity, rollback ID and oplog; paused application writes during baseline |

The MySQL baseline holds a global read lock. Schedule this initial full backup
for a maintenance window. Subsequent captures rotate the binary log and read
only closed logs. PostgreSQL captures request a WAL switch; if archival has not
finished, wait and retry. Configure archive retention to preserve all segments
from the initial base cursor until capture completes.

MongoDB baselines require `pitr.quiesced: true`. Pause application writes, DDL,
TTL deletions and other writers; drain open/prepared transactions and wait for
majority commit. DBVault verifies that only noop entries occur
during the baseline and that the initial oplog cursor remains retained. Resume
writes after successful publication. Captures use a majority-committed boundary
and reject oplog expiry and rollback.

Cluster scope is mandatory: table/collection selectors and extra dump flags
are rejected. SQLite native PITR is not implemented. PostgreSQL external
tablespaces/symlinks and cross-timeline recovery are rejected; create a new base
after a promotion. Server replacement and version changes require a new base.

## Configuration and commands

Keep the normal database, storage and encryption configuration. Add:

```yaml
pitr:
  archive_directory: /srv/dbvault/wal # PostgreSQL only, already fed by archive_command
  quiesced: true                    # Required for MongoDB baseline
```

Native tools can be located in PATH or configured by absolute executable paths
under `database.tools` (`pg_basebackup`, `mysqlbinlog`, and existing tools).
The MySQL DBVault Docker target includes the matching `mysqlbinlog`; the upstream
minimal `mysql:8.4` image alone does not. The integration tool image can be built
with `docker build -f Dockerfile.mysql-native-tools -t dbvault:native-mysql-tools .`.

```sh
dbvault pitr base --json
dbvault pitr capture --parent pitr_BASE_ID.tar.enc --json
dbvault pitr capture --parent pitr_PREVIOUS_CAPTURE_ID.tar.enc --json
dbvault pitr list --json
```

Each new range pins its parent's checksum, source identity, cursor and coverage
time. Explicit `capture` can create branches. For automatic parent selection use
`pitr backup --type incremental`: it authenticates the source, creates a baseline
if none exists for that source, and otherwise captures from its latest baseline
chain. Missing logs, broken ancestry and version changes fail without silently
resetting the chain. Create a new baseline before 1,024 records accumulate.

## Scheduled capture and retention

```sh
# SQL engines: capture every five minutes; refresh the baseline daily.
dbvault schedule add --id native --operation pitr --type incremental \
  --base-every 24h --cleanup --cron '*/5 * * * *' --config production.yaml
dbvault schedule

# Alternatively, create a separate full job for a maintenance window.
dbvault schedule add --id native-full --operation pitr --type full \
  --cleanup --cron '0 2 * * *' --config production.yaml

dbvault pitr cleanup --dry-run --config production.yaml
dbvault pitr cleanup --config production.yaml
```

The foreground daemon skips overlapping jobs. Credentials and source identity
are reloaded each run. `--base-every` defaults to zero: baseline refresh requires
an explicit full job unless an interval is supplied. Interval refresh happens
on the next run after it is due. MySQL baseline refresh holds a global read lock.
MongoDB baseline refresh requires paused/drained writers and
`pitr.quiesced: true`; DBVault does not pause applications. Create MongoDB
baselines during a maintenance window, then schedule incremental captures with
no baseline interval. If no baseline exists, incremental backup attempts one
and applies the same quiescence checks.

Native cleanup uses `retention.keep_count` and `keep_days`, but counts complete
baseline chains rather than individual log archives. Count and age protections
are combined; the newest baseline for each engine/source identity is always kept,
including all baselines tied at that timestamp.
Age uses the newest range's coverage end, so an active chain is retained in full.
Both zero disables deletion. Superseded whole chains are deleted child-first;
branches of a retained baseline are also retained. Rotate baselines regularly
to let storage expire. Cleanup verifies all archives, including retained ones,
before deleting data. Corruption or missing parents aborts deletion.

`pitr backup --cleanup` applies retention after publishing a successful backup.
Cleanup failure reports that publication succeeded. Logical cleanup still
manages only logical dumps. Native mutations and restores share an exclusive
storage lock, `dbvault_pitr.lock`; inspect and remove a stale lock only after
confirming its owner has stopped. Interrupted publication/deletion can leave
orphan archives or sidecars; inspect failures before retrying. Never manually
remove archives belonging to a recovery chain you need.

## Recover to a time

`--time` is a whole-second RFC3339 timestamp, interpreted in UTC. The boundary
is **exclusive**: operations at or after the target second are excluded.
The target must be within the baseline/captured coverage range. All archives
are checksum-verified and, when encrypted, fully authenticated before any
database writes. Gaps, broken parent identities and unsupported targets fail.

### PostgreSQL

```sh
dbvault pitr restore --target pitr_LAST_CAPTURE_ID.tar.enc \
  --time 2026-10-06T08:30:00Z --directory /srv/recovery/pg-new --confirm
```

The directory must not exist and its parent must already exist. DBVault writes
the physical cluster, private WAL archive, `recovery.signal`, and explicit
recovery settings. It reports `prepared_requires_server_start`, not a completed
database restore. Start this directory using the matching PostgreSQL major in
an isolated recovery environment with a compatible platform/build. Supply the appropriate OS ownership and
adjust source configuration paths, ports and socket directories as necessary.
Check `pg_is_wal_replay_paused()` and the server log to verify that the target was
reached; PostgreSQL fails recovery if logs cannot reach the requested time.
After validation, promote explicitly if you want a writable instance.

### MySQL and MongoDB

```sh
dbvault pitr restore --target pitr_LAST_CAPTURE_ID.tar.enc \
  --time 2026-10-06T08:30:00Z --target-config fresh-instance.yaml --confirm
```

The target must be a separately provisioned empty instance of the same release
series; MongoDB targets must be initialized replica sets. Source and target
server/replica-set identities are compared, including through host aliases.
The MySQL target account needs a direct global `SHOW DATABASES` privilege;
MongoDB requires permission to list all databases. Limited visibility is rejected
instead of treating hidden databases as an empty instance.
User databases cause refusal. MySQL targets must match the baseline's stable
GTID mode (ON or OFF); changing GTID mode requires a new baseline. All database names are preserved, with no namespace
remapping or selective replay. Full baselines can restore account/role data;
provision the MongoDB target with the same administrative credentials used in
the backup so authentication remains valid between baseline and oplog replay.
MySQL applies the baseline and generated binlog SQL through one authenticated
connection. Use a private, isolated target and verify application data after
native replay. Failed targets may contain partial data and are preserved.

## Storage, encryption and cost

The configured local/S3/GCS/Azure storage provider holds native archives. Native
archives currently use uncompressed tar independently of the logical dump's
compression setting. Both environment-key v1 and KMS/Vault v2 envelopes work.
See [managed keys](managed-keys.md). Only log ranges are read after the baseline;
storage use depends on WAL/binlog/oplog volume, not the whole database size.

Temporary disk must accommodate the baseline and selected log chain during
verification/restoration. Native history is listed separately and is not yet
included in logical-scope health, Prometheus counters or recovery-drill
evidence. Native CLI commands require their engine's privileges and tools;
regular `doctor` still checks the logical backup toolchain.
For storage-only native freshness checks, use `health.backup_scope: pitr` and an
explicit `health.source_identity`; see [independent monitoring](independent-monitoring.md).

## References

- [PostgreSQL continuous archiving and PITR](https://www.postgresql.org/docs/16/continuous-archiving.html)
- [MySQL binary log backup](https://dev.mysql.com/doc/refman/8.4/en/mysqlbinlog-backup.html)
- [MongoDB mongorestore oplog options](https://www.mongodb.com/docs/database-tools/mongorestore/)
