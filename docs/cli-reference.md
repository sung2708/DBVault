# CLI Command Reference

This document provides a comprehensive command-line reference for the `dbvault` binary.

The CLI implements all four database engines and local/S3/GCS/Azure storage.
Run `dbvault COMMAND --help` for the exact live flags. MongoDB restore accepts
repeatable `--collection`; PostgreSQL accepts `--table` and `--schema`.
Operation commands accept flags rather than positional arguments; `help` accepts
a command/subcommand path. `--output json` (alias `--json`) emits one JSON result
on stdout and JSON diagnostic/error records on stderr. Human mode prints concise
summaries and borderless tables; progress/status go to stderr. Redirected streams
contain no ANSI or cursor control. `--no-color` and `NO_COLOR` disable colors and
animation; `--quiet` suppresses progress and hints while keeping data/errors.
`--verbose` adds redacted diagnostic detail. Help remains human-readable.
See [terminal output](terminal-ui.md) for progress semantics and examples.

`dbvault config` validates the configuration schema without network access.
`dbvault doctor` checks readiness using authenticated database preflight and
storage read access. It performs no database writes or cloud object writes/deletes;
Slack delivery is never tested. Warnings do not fail readiness, but failed required
checks return exit code 1. Use `--json` for the structured report.
`dbvault init` creates configuration with optional interactive setup or flags.
It discovers supported native database tools in `PATH` and known installation
locations, then saves validated executable paths. Use `--native-tool-dir DIR`
to select a complete tool installation explicitly.
`dbvault update check` checks official release metadata and prints safe update
instructions; it never downloads or installs a binary.
`dbvault verify --target KEY` hashes stored bytes and checks metadata size/hash.
`dbvault inspect --target KEY` reads validated metadata without hashing bytes.
`dbvault delete --target KEY --confirm` unregisters and deletes a backup;
`--dry-run` previews deletion without requiring confirmation.

---

## Global CLI Conventions

### Synopsis
```bash
dbvault [command] [flags]
```

### Global Flags
All commands inherit the following root flags:

| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--config` | `-c` | string | `"dbvault.yaml"` | Path to the YAML configuration file. |
| `--verbose` | `-v` | boolean | `false` | Enable detailed debug log output. |
| `--json` | | boolean | `false` | Alias for `--output json`: one stdout result, structured stderr logs/errors. |
| `--help` | `-h` | boolean | `false` | Display help information for the command. |

### Exit Codes
- `0`: Operation completed successfully.
- `1`: General error (e.g., configuration parsing error, invalid flags).
- `2`: Network or database connection failure.
- `3`: Native tool dependency missing from configured paths, PATH or known locations.
- `4`: Checksum integrity verification failed.
- `5`: Execution canceled (SIGINT / SIGTERM / Timeout).

---

## `dbvault status`

```text
dbvault status [--timeout 30s] [--state .dbvault-schedules.json]
dbvault status --output json
dbvault status --no-color
```

Status aggregates the configured database's latest health evaluation, up to five
validated recent manifests, exact-backup recovery evidence, storage type and
saved schedule definitions. It does not connect to the database, hash archive
bytes, restore data or run a drill. The general protection policy currently
reports `not_configured`; `health.max_backup_age` remains the freshness policy.
The independent `verify_after_backup` field reports the configured read-back
setting and does not imply that a broad protection policy was evaluated.
Integrity uses the latest matching immutable verification record, or `unknown`
when none exists; health also detects immediate missing/size-mismatched bytes.
Recovery and checksum integrity remain distinct facts.

The state file is advisory: a saved cron definition does not prove a running
scheduler process. An unreadable scheduler state only affects that subsection;
the rest of status still renders. Unreadable configuration remains fatal. JSON is
one structured result on stdout. `--quiet`, `--no-color`, `NO_COLOR` and redirected
output follow the global conventions above. Exit code follows operational read
errors; status evidence such as stale/no backup is reported in the result.

## `dbvault health`

```text
dbvault health [--verify] [--timeout 30s] [--state .dbvault-schedules.json]
dbvault health --output json
dbvault health --no-color
dbvault health --quiet
```

Health evaluates the single database in the selected config, matching manifest
engine and exact database name. It uses the existing validated completed sidecars,
selecting the newest `created_at` (ties use backup name ascending). Age is measured
from dump/snapshot start in UTC, so a long-running backup does not hide old data.
Future creation/completion timestamps produce unknown rather than negative ages.
There is no separate registry or multi-profile configuration.

Set `health.max_backup_age: 12h` in YAML. There is no default freshness limit and
cron cadence is not treated as an SLA. Exactly max age is fresh; strictly older
is stale. Retention does not define freshness.

Age is refreshed after metadata/active-verification I/O, so a backup crossing its
freshness limit during a long verification does not retain a healthy start-time status.

| Status | Evidence |
|---|---|
| `healthy` | Fresh completed backup, valid metadata, artifact exists and latest exact-backup stored-artifact verification evidence passed |
| `warning` | Fresh backup with unknown verification history, or fresh verified backup with all matching saved schedules disabled |
| `critical` | No registered matching backup, latest artifact missing, size mismatch, failed stored-artifact verification, failed active verification, or stale backup |
| `unknown` | Missing freshness policy, future timestamps, or insufficient evidence because a check failed |

Missing/broken artifacts take precedence over absent freshness policy. Unregistered
orphan archives (including missing sidecars) never count as successful backups.
An invalid/unreadable sidecar anywhere in the configured namespace prevents a
reliable latest selection and fails the check; health does not silently skip it
or fall back to an older artifact. Sidecars from other databases are validated
but only exact matching engine/database manifests are candidates.

Default checks list the configured provider namespace, read bounded sidecars,
check only the selected artifact's existence and compare listed size if available.
The current provider API returns the full namespace listing, so large registries
still incur listing/sidecar reads; configure a dedicated cloud prefix to bound it.
Archive contents are never downloaded by default. Local provider initialization
follows normal storage conventions and creates a missing output directory.
No database connection, native tools, password resolution, restore or Slack send
is involved. Cloud read credentials are still required.

`--verify` reuses `verify` and streams only the candidate's full stored bytes;
this can cost substantial time/bandwidth. Increase the positive `--timeout` for
large archives. Standalone `verify`, `health --verify`, and configured
post-backup verification append small immutable records associated with the
exact backup ID/name/SHA-256/engine/database. JSON `integrity` is `unknown`,
`verified` (latest matching record passed), or `failed` (latest matching record
failed). A restore's mandatory pre-write checksum check is not recorded as a
standalone verification event.
`restore_test` is `unknown` unless validated separate recovery evidence matches
the exact selected backup ID/name/SHA-256/engine/source. It then reports the latest
drill's `passed`, `failed` or `cancelled` result with timestamp and record key.
Unreadable/invalid/future history produces unknown evidence with a note; no
positive result is inferred. Health reads these small records, never hashes the
archive to display history. History is advisory and does not change freshness or
current checksum status. **Healthy backup does not mean a recovery drill
has passed.** Checksum integrity is not evidence of successful recovery.

`--state` reads existing schedule definitions, matching the selected config's
absolute path. Matching IDs, cron expressions and enabled state are advisory.
No matching definition is not evidence that external scheduling is absent.
Saved enabled schedules do not prove the foreground daemon is running; missed
runs are not replayed. Health does not calculate next-run time or infer cadence.

Exit 0 means healthy. Completed warning/critical/unknown checks return 1 and
already include their reason in stdout, without a duplicate stderr diagnostic.
Configuration/storage check failures use existing error handling (normally 1);
invalid/unreadable manifests and active verification failures preserve the existing
integrity exit code 4; cancellation/deadline returns 5. JSON results are one pure
stdout object (`status`, `databases`); operational errors additionally use JSON
stderr diagnostics. Missing evidence fields are omitted instead of fabricated:
timestamps/age, policy/stale flag, artifact existence and backup identity/size.
Quiet retains result data/errors and suppresses decoration/hints, consistent
with the other commands. Non-TTY, `--no-color` and `NO_COLOR` suppress ANSI output.

## `dbvault recovery drill`

```text
dbvault recovery --help
dbvault recovery drill --target BACKUP-NAME --recovery-database NEW-SQLITE-PATH --dry-run
dbvault recovery drill --target BACKUP-NAME --recovery-database NEW-SQLITE-PATH --confirm [--cleanup] [--timeout 2h] [--output json]
```

SQLite support shipped in `v0.3.0`. Release `v0.4.0` also supports **PostgreSQL in a
new Docker-isolated server**. MySQL/MongoDB return an unsupported
error before contacting the database or creating a recovery target. No force
bypass, existing-server target or arbitrary validation hook is provided.

### PostgreSQL (`v0.4.0`)

```bash
docker pull postgres:16-bookworm # use the source major from the backup
dbvault recovery drill --target BACKUP-NAME --recovery-database recovery_check --dry-run
dbvault recovery drill --target BACKUP-NAME --recovery-database recovery_check --confirm --cleanup
```

The recovery database must be a lowercase name (letters, digits and underscores,
starting with a letter, at most 63 characters), different from the configured
and manifest source and PostgreSQL system databases. The CLI requires a trusted
Docker daemon and preloaded official `postgres:<source-major>-bookworm` image.
It never pulls images automatically. Source server/credentials and configured
host tool paths are not used. Arbitrary images and external recovery servers
are unsupported; extensions missing from the official image cause failure.

After stored-byte verification, create a container by pinned local image ID,
with random credentials, network `none`, no ports/host binds/shared volumes, and
anonymous database volumes. Inspect its exact ID, image, ownership label and
isolation before commands and cleanup. Native tools execute only inside it.
Restore uses the normal verified snapshot/decompression pipeline, then a
read-only catalog query and scans ordinary tables/populated materialized views.
Unpopulated materialized views are counted but not scanned. The
recorded object count and elapsed time do not prove application semantics,
expected row equality or production-equivalent recovery time.

Dry-run verifies the archive and checks image availability/source major only;
live compatibility, restore and validation are skipped. It creates no container
or recovery record. Real runs retain the container by default and on failure.
`--cleanup` removes only this invocation's container and anonymous volumes after
successful validation. Retained containers are stopped on success/failure/cancellation;
if the daemon cannot stop one, the command reports an error. The JSON
`recovery_target` is the container ID, usable with `docker inspect ID` and
`docker start ID` for inspection; retained targets need operator lifecycle management.
No local native tools or Docker-socket mounts in backup images are required.

### SQLite

`--target` follows existing restore resolution: backup name from `list`, not its
manifest ID; local paths must resolve inside storage. `--recovery-database` is a
new SQLite file in an already existing operator-controlled private directory.
Both flags are required. Configured production and manifest source paths are
normalized (including parent links, Windows case and existing file aliases).
Ambiguous paths, the same destination, any existing target or `-wal`/`-shm`/
`-journal` sidecar fail closed. Even an empty pre-existing recovery file is refused.
No production safety guard can be bypassed with confirmation.

`--confirm` authorizes target creation and restore for non-dry runs. The target
is created with exclusive no-overwrite semantics **after** metadata, stored
SHA-256/size, engine/format and embedded-engine compatibility checks. Existing
restore then preflights the new target and uses the same private verified snapshot
for its normal decompression/online SQLite restore. Target identity is checked
before writes and before validation/cleanup. The configured production file need
not exist; embedded version preflight uses an in-memory database. Its parent must
still resolve unambiguously. All inputs/archives remain operator-trusted under
the existing threat model; directories must not have untrusted concurrent writers.

Post-restore validation opens the target read-only, requires exactly one `ok`
from `PRAGMA integrity_check`, and queries `sqlite_schema` object count. This
proves structural consistency/catalog readability after an actual restore. It
does not establish application business invariants, row completeness, production
capacity or an RTO. The result keeps metadata/integrity/compatibility/restore/
validation/cleanup/record stages separate.

`--dry-run` creates no target or recovery record and never restores. It verifies
the artifact, checks isolation and embedded-engine compatibility, and reports
`preflight_passed`, with restore/validation skipped. It needs no `--confirm` and
cannot be combined with `--cleanup`. Temporary verified snapshots are still used
and removed, consistent with normal restore dry-run.

By default, recovery files are **preserved**. `--cleanup` removes only the exact
regular file created by the current invocation after successful validation, if
its filesystem identity/path still match and no SQLite sidecars remain. Failure
or cancellation preserves it (possibly partially restored) for inspection.
Pre-existing/replaced files and unexpected sidecars are never removed.
An identity/path change reports `ownership_changed` rather than claiming the
original target was preserved at the supplied path.
Ordinary `restore --confirm` still follows its existing destructive behavior.

`--timeout` defaults to 2h and must be positive. Native cancellation behavior
and temporary-snapshot cleanup are reused. Where OS free-space reporting works,
the CLI checks space after compressed verification and before target creation:
two raw-image sizes on temporary disk (conservatively budgeting a shared target
volume), plus one raw-image size on the target volume. These estimates cannot
reserve disk space; earlier snapshot writes and concurrent usage can still fail.

Real runs with known backup identity attempt to publish a separate random
`recovery_*.recovery.json` object into the configured backup namespace. Records
contain ID/name/SHA-256 association, engine/source, times/duration, target state,
stages and built-in validation evidence, with no credentials or raw errors.
Configured secrets are redacted in record path/name fields. Successful manifests
are immutable and never rewritten. Recording failures make the command fail;
restore/validation stages still report what actually happened. Existing records
are not overwritten or automatically pruned; deleting a backup does not delete
its drill history. Storage read **and write** permissions are needed for evidence.

JSON is a single stdout result with `status` (`passed`, `failed`, `cancelled` or
dry-run `preflight_passed`), stage states, times, target state, validation and
record key when publication succeeded. Errors use normal redacted stderr records.
Exit 0 means passed (or successful preflight when explicitly dry-run); other
results are nonzero using existing categories: normally 1, connection 2, dependency
3, integrity 4, cancellation/deadline 5. Non-TTY/quiet/JSON never prompt. Output
flags follow current terminal policies; there are no fabricated progress counts.
Drills do not send ordinary restore Slack notifications.

## `dbvault doctor`


Run readiness checks before the first backup. Doctor validates configuration,
checks authenticated database connectivity and required native-tool compatibility,
verifies local storage readability/writeability or makes a narrow cloud list
request, and confirms that the OS temporary directory accepts a removable probe.
Where supported it reports currently available temporary-disk bytes; it does not
estimate how much a particular backup will need. A missing local output directory
is not created during diagnosis.

Doctor does not create database objects, backups, or cloud objects. It does not
test cloud write/delete permissions or send Slack messages. Enabled Slack is only
checked for a configured webhook environment variable. Warnings are advisory;
failed required checks return exit code 1. Cancellation or timeout returns 5.

```text
dbvault doctor [--config FILE] [--timeout DURATION] [--json]
```

`--timeout` defaults to 30 seconds and applies to the full readiness run. The JSON
report has `ready`, `summary` counts and a `checks` array with stable names,
categories, statuses, safe summaries, details and remediation hints.

---

## `dbvault init`

Create a configuration with inline keyboard prompts, or supply complete flags
to skip interaction. Default destination: `dbvault.yaml` in the current directory.
No existing config is needed. The generated file uses the runtime schema and
passes its validation before any write.

```bash
dbvault init
dbvault init --database postgres --storage local
dbvault init --non-interactive --database sqlite --database-name ./app.db --storage local
dbvault init --non-interactive --database postgres --database-name production --user dbvault --storage s3 --bucket backups --region us-east-1 --config ./production.yaml
```

| Flags | Meaning |
|---|---|
| `--database`, `--storage`, `--database-name` | Required engine, backend and database name/file |
| `--user` | Required for PostgreSQL/MySQL/MongoDB |
| `--host`, `--port` | Runtime host and engine-specific port defaults |
| `--password-env` | Environment variable name; default `DBVAULT_DB_PASSWORD`; no password flag |
| `--ssl-mode` | Default `prefer` for PostgreSQL/MySQL; `require` for MongoDB |
| `--auth-database`, `--quiesced` | MongoDB authentication DB (default `admin`) and stopped-writes acknowledgement |
| `--output-dir` | Local directory; default `./backups` |
| `--bucket`, `--region` | S3 requires both; GCS requires bucket |
| `--container`, `--account-name` | Required Azure settings |
| `--prefix` | Optional cloud object prefix |
| `--compression` | gzip (runtime default), zstd or none |
| `--non-interactive` | Never prompt; fail on missing required flags |
| `--force` | Authorize replacing an existing regular configuration file |
| `--test`, `--timeout` | Optional database/native-tool test before writing; timeout defaults to 30s and must be positive |
| `--native-tool-dir` | Explicit directory with a complete native toolset for PostgreSQL, MySQL or MongoDB |

An explicit flag skips its question. When any required field is missing and both
stdin and stderr are terminals, setup asks for missing values and optional settings.
Complete required flags skip all prompts and use defaults for unspecified options.
Non-TTY input, JSON output and quiet mode never prompt. JSON emits one result
record on stdout; errors remain structured stderr records through the normal CLI.

Existing files fail without `--force` when prompts are disabled. Interactive
setup offers cancel, a summary of existing configuration, or overwrite; no merge.
Esc/Ctrl+C cancel with exit code 5. Invalid settings return exit code 1. No partial
file is left by cancellation before publication. The parent directory must exist.

`--test` reuses the ordinary test service, checks database/native tools, and does
not contact storage. Non-interactive tests require the referenced password variable.
Interactive tests may request a masked temporary password without saving it.
Failed tests require interactive approval to save anyway; automation fails without
writing. Native PostgreSQL/MySQL/MongoDB tools are separate prerequisites.

Interactive setup shows a platform-specific command for setting the configured
`password_env` variable in the current terminal: a masked PowerShell prompt on
Windows, or a hidden `read -s` prompt on macOS/Linux. Run later DBVault commands
from that same terminal. A password entered only for init's optional test is
temporary; set the environment variable again before backup/restore.

Cloud credentials use the AWS default chain, Google ADC or Azure default identity.
Setup does not ask for cloud secrets, create buckets or check their permissions.
Use the generated file with normal config/test/backup/list/restore commands.

---

## 1. `dbvault backup`

Executes a full or selective database backup, streaming output through compression, calculating SHA-256 checksums, and storing the archive.

### Synopsis
```bash
dbvault backup [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--database` | `-d` | string | `""` | Target database name (overrides config). |
| `--output-dir` | `-o` | string | `""` | Destination directory for local backups (overrides config). |
| `--compression` | | string | Configuration | Override with `none`, `gzip`, or `zstd`. |
| `--dry-run` | | boolean | `false` | Validate environment, flags, and connectivity without creating a dump. |
| `--timeout` | | duration | `2h` | Execution timeout (e.g., `30m`, `2h`, `4h`). |
| `--type` | | string | `full` | Incremental/differential are explicitly unsupported. |

Set `--timeout 0` to disable the operation deadline. An omitted `--compression`
uses configuration; help does not advertise a fixed CLI default. Backup dry-run
checks tools and authentication but does not initialize or probe write access to
storage. It creates no dump or artifact.

Set `protection.verify_after_backup: true` to read the published artifact back
from storage and use the same streaming size/SHA-256 verifier as `dbvault verify`.
This adds a full object read (including cloud read/API and possible egress cost).
The immutable manifest is not changed; a separate verification record is written.
If verification fails or is cancelled, the command exits nonzero and reports
`backup_status: success` with a failed/cancelled verification state because the
artifact and manifest were already created. DBVault preserves them for inspection.
When the option is omitted/false, no post-backup read is performed and JSON says
`verification.status: not_requested`.

### Examples
```bash
# Backup using defaults from config
dbvault backup --config /etc/dbvault/example.yaml

# Backup with dry-run verification
dbvault backup --config /etc/dbvault/example.yaml --dry-run

# Override database name and target output directory
dbvault backup --config example.yaml --database analytics_db --output-dir /mnt/backups
```

---

## 2. `dbvault restore`

Restores a database from a compressed backup archive. Requires verification of the sidecar SHA-256 manifest prior to restoring.

### Synopsis
```bash
dbvault restore [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--target` | `-t` | string | `""` | Path or object key of the backup archive to restore (Required). |
| `--confirm` | | boolean | `false` | Mandatory confirmation flag to authorize destructive restore operations. |
| `--clean` | | boolean | `false` | Drop backed-up PostgreSQL objects before restoring. |
| `--timeout` | | duration | `4h` | Execution timeout for restore operation. |
| `--database` | `-d` | string | Config | Override the destination database. |
| `--dry-run` | | boolean | `false` | Verify archive, compatibility and connectivity without database writes. |
| `--table` | | strings | Empty | Select PostgreSQL tables (repeatable/comma-separated). |
| `--schema` | | strings | Empty | Select PostgreSQL schemas. |

Integrity checks are mandatory; there is no checksum bypass flag. Targets are
flat storage keys or paths resolving directly within the configured local root.
Restore dry-run does not require `--confirm`; it still makes a private verified
temporary snapshot and contacts the target database. There is no interactive
confirmation prompt. `--timeout 0` disables the deadline.

### Examples
```bash
# Safe restore with pre-flight checksum verification
dbvault restore --config example.yaml --target ./backups/demo_db_20261001_020000.dump.gz --confirm

# Restore with database cleaning
dbvault restore --config example.yaml --target ./backups/demo_db_20261001_020000.dump.gz --clean --confirm
```

---

## 3. `dbvault test`

Runs pre-flight connectivity checks against the database and ensures all required native client utilities resolve from configured paths, `PATH` or supported installation locations.

### Synopsis
```bash
dbvault test [flags]
```

### Examples
```bash
dbvault test --config dbvault.yaml
```

**Output:**
```text
[OK] Native binary 'pg_dump' found: /usr/bin/pg_dump (PostgreSQL 16.1)
[OK] Native binary 'psql' found: /usr/bin/psql (PostgreSQL 16.1)
[OK] Connection to PostgreSQL at 127.0.0.1:5432 established successfully.
[SUCCESS] All pre-flight tests passed.
```

---

## 4. `dbvault list`

Lists all backup archives and sidecar manifests in the configured storage destination.

### Synopsis
```bash
dbvault list [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--prefix` | | string | `""` | Filter backups by filename or key prefix. |
| `--limit` | `-n` | integer | `50` | Maximum number of backups to display. |

### Examples
```bash
dbvault list --config dbvault.yaml
```

---

## 5. `dbvault cleanup`

Applies retention policies (`keep_days` and `keep_count`) to delete stale backup archives and sidecar manifests.

### Synopsis
```bash
dbvault cleanup [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--dry-run` | | boolean | `false` | Preview backups eligible for deletion without deleting them. |
| `--keep-days` | | integer | Config value | Retain backups newer than N days. |
| `--keep-count` | | integer | Config value | Retain the most recent N backups. |

### Examples
```bash
# Preview cleanup
dbvault cleanup --config dbvault.yaml --dry-run

# Execute cleanup
dbvault cleanup --config dbvault.yaml
```

---

## 6. `dbvault version`

Displays the compiled DBVault version, Git commit SHA, and Go runtime version.

### Synopsis
```bash
dbvault version
```

### Output
```text
dbvault version 0.1.0 (commit: e9a1bf0, built: 2026-10-01, runtime: go1.26.5)
```

---

## 7. `dbvault update check`

Checks GitHub's official latest published stable DBVault release. The check
compares SemVer versions, ignores development builds and never suggests a
downgrade. Finding an update is a successful check and does not install it.

```bash
dbvault update --help
dbvault update check
dbvault update check --force
dbvault update check --output json
dbvault update check --no-color
```

`--force` bypasses the 24-hour metadata cache. The cache contains only public
release version, URL and check time in the operating system's user cache
directory. Successful JSON results go to stdout and structured failures go to
stderr; network/API failure is exit code 2, cancellation is exit code 5.
`dev` builds report development status without making a network request. The
command only contacts GitHub's official release API and sends no database,
storage, credential or machine identity data. It does not download assets,
modify PATH, invoke package managers or replace the running executable.

Prebuilt installs should open the official release page and select the matching
platform archive. Go module installs can run the explicit versioned command
shown by the checker. `dbvault version` remains local and works offline.

## 8. `dbvault schedule`

The foreground daemon accepts `--cron` and a configuration path, or loads
definitions from `--state` (default `.dbvault-schedules.json`). External
schedulers remain recommended; see [scheduling](scheduling.md).

Runs DBVault as an in-process cron scheduler executing automated backup routines.

### Synopsis
```bash
dbvault schedule [flags]
```

### Flags
| Flag | Shorthand | Type | Default | Description |
|---|---|---|---|---|
| `--cron` | | string | `""` | Cron expression (e.g., `"0 2 * * *"` for 2:00 AM daily). |
| `--config` | `-c` | string | `"dbvault.yaml"` | Configuration file path. |

`schedule add --id daily --cron "0 2 * * *" --config configs/example.yaml`
persists a definition. `schedule list`, `remove --id`, `enable --id` and
`disable --id` manage definitions. Restart the daemon to load changes. Default
timezone is UTC; `CRON_TZ=Asia/Bangkok 0 2 * * *` selects a timezone. Overlapping
jobs are skipped; missed runs are not replayed. SIGINT/SIGTERM cancels work.
