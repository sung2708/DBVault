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

## 7. `dbvault schedule`

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
