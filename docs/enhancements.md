# Encryption, logical incremental backups and recovery operations

These features are included in v0.6.0. Existing unencrypted full
backups remain readable. Older DBVault binaries reject the new manifest fields;
use an updated binary to restore new encrypted or incremental backups.

## Client-side encryption and key rotation

Create a random wrapping key in a new private file:

```text
dbvault encryption keygen --file key-v1.txt
```

Load it into an environment variable. In PowerShell:

```powershell
$env:DBVAULT_KEY_V1 = (Get-Content ./key-v1.txt -Raw).Trim()
```

In a POSIX shell:

```sh
export DBVAULT_KEY_V1="$(cat key-v1.txt)"
```

Add this to the existing database/storage configuration:

```yaml
encryption:
  key_id: v1
  keys:
    v1: DBVAULT_KEY_V1
metrics:
  record_operations: true
```

`dbvault doctor` checks the active key without printing it. Full backups stream
native output through compression, client-side encryption and stored-byte
SHA-256. Encryption generates a new random 256-bit data key per archive, wraps
it with the selected 256-bit key using AES-GCM and encrypts 64 KiB frames with
authenticated sequence numbers and an authenticated end marker. Archive
identity, database/engine/selectors, compression and incremental references are
bound to the wrapped key. Encryption suffixes filenames with `.enc`.

To rotate, generate `key-v2.txt`, load `DBVAULT_KEY_V2`, add
`v2: DBVAULT_KEY_V2` to `keys` and change `key_id` to `v2`. Keep `v1` and its
environment variable available until every backup and incremental ancestor
using it has expired. Rotation affects new archives; it does not re-encrypt
existing objects. Losing a required key makes its backups unrecoverable.

Keys are referenced by ID in manifests; key material never enters metadata,
argv or logs. This implementation uses environment-provided wrapping keys;
AWS KMS and HashiCorp Vault Transit are available through separate
[`encryption.providers` mappings](managed-keys.md). Archive names,
database metadata and evidence remain visible in storage. Existing storage IAM
and operator trust requirements apply. Restrict key file and temporary directory
ACLs on Windows. Restore uses private temporary plaintext files, removed on
normal return; a process crash can leave temporary files behind.

## Logical dump incremental chains

```text
dbvault backup
dbvault backup --type incremental
dbvault backup --type incremental --dry-run
```

Incremental backups reuse the latest available backup with the same
engine/database/format and selectors. A full backup must exist first. DBVault
creates a new consistent full logical dump, compares 64 KiB blocks at the same
offset with the verified previous dump, and stores references for unchanged
blocks plus literal bytes for changed blocks. This works for PostgreSQL, MySQL,
MongoDB and SQLite using their existing consistency requirements. MongoDB still
requires stopped writes and `database.options.quiesced: true`.

This reduces stored/uploaded bytes when offsets remain stable. It still reads
the whole database, reads and reconstructs the base chain, and needs temporary
disk space. Dump ordering, timestamps or shifted data can reduce the savings.
Logical deltas do not archive WAL, binlog or oplog or provide point-in-time
recovery. Use the separate [native `pitr` commands](pitr.md) for log capture and
timestamp recovery. Logical adapter incremental capabilities remain unchanged; the
incremental implementation belongs to the application/storage layer.

Manifests pin the base name, ID, stored SHA-256, reconstructed dump SHA-256 and
chain depth. Chains are limited to 32 links. Create periodic full backups to
reset depth and bound recovery cost. Restore authenticates, verifies,
decompresses and reconstructs every dependency into private temporary files
before preparing or writing the destination. Memory remains bounded; temporary
disk usage grows with the number and size of ancestors. Provision space for
stored snapshots, decoded archives, reconstructed dumps and the restore target.

Deletion, including its preview, refuses a backup referenced by an incremental
child. Retention keeps every ancestor of retained backups, even if those bases
exceed age/count policy. Obsolete chains are selected in child-before-base order
so one cleanup run can remove the entire expired chain. Default health checks dependency
metadata, presence and listed sizes; `health --verify` also hashes ancestors.
`verify` alone checks the selected stored object; it does not decrypt or prove
the entire recovery chain.

A create-only `dbvault_chain.lock` storage object serializes incremental
publication and deletion across processes. A crashed operation can leave it
behind. Confirm that no chain operation is running before removing that exact
lock object with storage tooling. Do not mix concurrent deletion by older
binaries or external storage lifecycle rules with incremental chains.

Export with `--decompress` to produce a standalone native dump/image, including
decryption and reconstruction of increments:

```text
dbvault export --target ACTUAL-BACKUP-NAME --file recovered.dump --decompress
```

Without `--decompress`, export copies the exact stored object. An exported delta
still requires its base chain; an encrypted object still requires its key.

## MySQL and MongoDB recovery drills

Preload a trusted official image matching the backup source. MySQL uses its
8.x release series; MongoDB uses major/minor:

```text
docker pull mysql:8.4
docker pull mongo:8.0
dbvault recovery drill --latest --recovery-database recovery_check --dry-run
dbvault recovery drill --latest --recovery-database recovery_check --confirm --cleanup
```

Use the configuration for the engine being recovered. `--latest` selects the
latest completed backup for its configured engine/database. It is mutually
exclusive with `--target`. Every server drill creates a new server by pinned
local image ID with random credentials, network `none`, no published ports or
host binds and anonymous volumes. Source credentials and host native tools are
not used. MySQL target connections require TLS. MongoDB uses authenticated
in-container tools and private credential files.

MySQL checks every base table with `CHECK TABLE` and reads row counts. MongoDB
runs full collection validation and reads counts. These checks establish
structural readability, not application semantics or equality with a live
source. Views and application expectations need separate operator validation.
Dry-run authenticates/reconstructs the archive and checks image availability
without creating a server. Real runs save exact-backup recovery evidence.
Failed or cancelled targets are preserved and stopped. Successful targets are
also retained/stopped unless `--cleanup` is supplied; cleanup validates ownership
before removing only that target and its anonymous volumes.

## Scheduled recovery drills

Save a separate recovery job with explicit recurring authorization:

```text
dbvault schedule add --id weekly-recovery --operation recovery --confirm --cron "CRON_TZ=Asia/Bangkok 0 3 * * 0" --config production.yaml
dbvault schedule add --id hourly-increment --type incremental --cron "0 * * * *" --config production.yaml
dbvault schedule
```

For SQLite, also supply `--recovery-dir` pointing to an existing private
directory. Each execution generates a unique new destination, selects the latest
matching backup and cleans up only after successful validation. It shares the
existing scheduler overlap gate, two-hour deadline and cancellation handling.
The foreground daemon must remain running; saved jobs alone do not prove
liveness. Restart it after editing definitions. Configured Slack notifications
include recovery completion/failure; delivery errors do not alter drill evidence.

## Prometheus metrics

```text
dbvault metrics
dbvault metrics --listen 127.0.0.1:9090
```

The command prints Prometheus text or serves `GET /metrics`. It reads manifests
and records, never archives or databases. Bind the unauthenticated endpoint to a
trusted interface. Ctrl+C shuts it down. Storage failures return HTTP 503.

Enable `metrics.record_operations: true` in backup/drill configurations to save
durable success/failure counters, operation durations and stored byte totals.
It is disabled by default to avoid extra storage writes. Records contain no
error strings or credentials. Recording errors are logged and do not change the
backup/drill result. Counters cover recorded runs only and depend on retained
`operation_*.json` objects; deleting records resets that portion of the totals.
Metric reads currently scan records and manifests; large installations should
consider scrape frequency and cloud request costs.

Metrics include `dbvault_operations_total`,
`dbvault_operation_duration_seconds_sum`, `dbvault_backup_bytes_total`,
`dbvault_last_backup_timestamp_seconds`, `dbvault_last_backup_size_bytes` and
`dbvault_backup_age_seconds`. Labels identify the configured database and engine.
Timestamp/age/size series are absent when no matching completed backup exists.
