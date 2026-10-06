# Monitor backup freshness from another host

## Existing evidence and the missing pieces

`dbvault health` already reads completed logical manifests from local/S3/GCS/Azure
storage, checks artifact availability/size, applies `health.max_backup_age`, and
returns structured status and a nonzero exit for overdue/missing backups or
unavailable storage. It does not contact the database or send Slack messages.
No heartbeat registry or distributed scheduler is needed to detect a missed
backup SLA. Saved schedules describe intent; they cannot establish process
liveness or successful execution. Operation counters record outcomes, so an
absent run has no failure event to count.

The additions are storage-only native PITR health, explicit disabling of local
schedule inspection with `--state ""`, JSON reports for runtime initialization
failures, and a standalone freshness probe example. Logical health remains the
default and retains its status/exit semantics.

## Deploy on the independent monitoring host

1. Install the reviewed DBVault CLI and Python 3 on a host independent of the
   backup producer. Copy [the example probe](../scripts/monitor-backups.py) and
   adapt [the monitor profile](../configs/monitor-s3.yaml).
2. Point at the exact same bucket/prefix (or GCS/Azure container/path) as the
   producer. For logical backups, match engine and exact database name. Use
   absolute paths for shared local mounts, verify the mount is present before
   probing, and mount it read-only. Local initialization creates a missing
   directory, so an absent mount must not become a new empty local destination.
   Storage hosted only on the producer is unavailable when that host is off;
   a separate storage service/cloud bucket is preferable for recovery and checks.
3. Give the monitor a separate identity with list/read access only. S3 requires
   `ListBucket` for the selected prefix and `GetObject`; storage-side encryption
   can also require its service's decrypt permission. Use workload identity or
   protected credential files/environment. Do not copy database passwords,
   Slack webhooks, client encryption keys, KMS wrapping permissions or Vault
   tokens. SQL monitor `database.user`/`password_env` fields are unused placeholders;
   leave that password environment variable unset.
   Grant list/read/traverse access for a shared local mount. Use a dedicated
   monitor environment without unintended `DBVAULT_DB_*` or storage overrides
   that would redirect the selected profile.
4. Set `health.max_backup_age` explicitly to the backup cadence plus expected
   dump duration and allowed lateness. For a six-hour cadence, `7h` is an example,
   not an inferred default. Synchronize clocks. Retention and cron expressions
   do not supply a freshness policy.
5. Run the check every five minutes using the monitor host's scheduler or
   monitoring platform. Configure alerts on nonzero probe exits and missing/late
   probe results. A cron entry alone does not deliver alerts.

```sh
python3 /opt/dbvault/monitor-backups.py \
  --dbvault /usr/local/bin/dbvault --config /etc/dbvault/monitor.yaml
```

Example cron entry on the monitor host (wire results to your monitoring adapter):

```cron
*/5 * * * * /usr/bin/python3 /opt/dbvault/monitor-backups.py --dbvault /usr/local/bin/dbvault --config /etc/dbvault/monitor.yaml
```

On Windows Task Scheduler, use Python's absolute executable path and pass the
script, `--dbvault C:\DBVault\dbvault.exe` and an absolute monitor config path
as arguments. Use a protected task identity/environment and configure failure
and missing-run alerts in the monitoring platform. Do not put credentials in
arguments, cron entries or alert messages.

The probe invokes `health --state "" --output json` with a deadline, validates
the response and emits one JSON object. Probe exit **0** means fresh registered
backup; **1** means critical/unknown and should alert. A fresh default health
warning caused solely by unknown checksum history is accepted for this
freshness-only probe, retaining `dbvault_status: warning` and its original exit
code. Missing policy, malformed/inconsistent JSON, command failure, future
timestamps, corrupt/missing artifacts and storage errors never pass.
Raw child diagnostics, configuration paths and credentials are not forwarded.

```json
{"status":"healthy","reason_code":"fresh_backup_integrity_unknown","dbvault_status":"warning","dbvault_exit_code":1,"checked_at":"2026-10-06T12:00:00+00:00"}
```

The probe is an example adapter, not a notification service. Monitoring software
must handle retries/deduplication, delivery and its own missing-probe alarm.
For stricter integrity checks, consume `dbvault health` directly: exit 0 is
healthy; completed warning/critical/unknown return 1; integrity failures return 4;
timeout/cancellation returns 5. Unknown checksum history remains a warning.
Enable `protection.verify_after_backup` on the producer for logical verification
evidence that cheap health can read with read-only credentials. Logical
`health --verify` writes verification evidence and needs write access, so it is
unsuitable for a strictly read-only monitor.

## Native PITR profiles

PITR backups are instance-wide and stored separately from logical dumps. Configure
an explicit source identity from `pitr list --json` (the record's `identity` field):

```yaml
health:
  max_backup_age: 15m
  backup_scope: pitr
  source_identity: REPLACE_WITH_EXACT_NATIVE_IDENTITY
```

The monitor selects the newest archived coverage for that engine/identity,
validates ancestry and checks every chain artifact's availability/size. It never
connects to the source or substitutes another server's identity. Baselines count
as completed native backups. `last_backup_at`/`age_seconds` measure native
`until` coverage, rather than logical dump start. Future coverage is unknown.
The database name is a profile label in this scope, not a database selector.

Default native checks warn about unknown checksum integrity; the freshness probe
handles this as described above. Native `health --verify` hashes the complete
stored chain, including encrypted bytes, without keys or evidence writes. It
does not authenticate/decrypt ciphertext, test replay or prove recovery. Increase
the deadline for large reads. Source replacement/promotion/rollback may require
a new identity and operator-reviewed monitor configuration; an old identity
eventually alerts. Idle MongoDB sources without new oplog entries may not advance
capture coverage.

## Existing Prometheus option

The existing `dbvault metrics --listen 127.0.0.1:9090 --config monitor.yaml` can
also run on the independent host and read shared storage. It needs no producer
connection. Secure network access; the endpoint has no built-in authentication.
Current freshness gauges cover logical backups, not native PITR.

Alert on all three conditions, using your deployment's actual target labels:

```promql
time() - dbvault_last_backup_timestamp_seconds{job="dbvault-monitor",engine="postgres",database="production"} > 25200
absent(dbvault_last_backup_timestamp_seconds{job="dbvault-monitor",engine="postgres",database="production"})
up{job="dbvault-monitor"} == 0
```

The first catches frozen timestamps even when the producer is off. The other
rules cover no backup series, exporter failure and storage errors (HTTP 503).
Also alert if the scrape target disappears from discovery. These gauges measure
metadata freshness, not full health/integrity. Do not rely only on operation
failure counters or an exporter on the producer host.

## Limits

Detection occurs after the configured freshness threshold plus polling/delivery
latency. It cannot immediately distinguish a stopped scheduler from producer
shutdown, network failure, invalid credentials or a slow backup, and does not
assert scheduler PID/service liveness. Use existing host/service monitoring for
that. DBVault's foreground scheduler does not replay missed runs. No control
plane, distributed daemon or HA mechanism is introduced.

Storage/sidecars must remain trusted: unsigned metadata and SHA-256 do not prevent
replacement by a storage administrator. Freshness does not prove recoverability;
keep separate recovery drills. Each profile checks one logical database or native
source. Monitor availability, alert delivery and storage must themselves be
supervised independently of the producer.
Publication/cleanup between storage reads can transiently fail a check; retry
and deduplicate in the monitoring platform rather than treating missing evidence
as success. Live cloud IAM and actual alert delivery require deployment validation.
