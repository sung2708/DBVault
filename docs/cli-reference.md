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
| `--json` | | boolean | `false` | Format stdout/stderr logs as JSON lines. |
| `--help` | `-h` | boolean | `false` | Display help information for the command. |

### Exit Codes
- `0`: Operation completed successfully.
- `1`: General error (e.g., configuration parsing error, invalid flags).
- `2`: Network or database connection failure.
- `3`: Native tool dependency missing from PATH.
- `4`: Checksum integrity verification failed.
- `5`: Execution canceled (SIGINT / SIGTERM / Timeout).

---

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

Runs pre-flight connectivity checks against the database and ensures all required native client utilities exist on `PATH`.

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
