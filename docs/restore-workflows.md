# Restore destinations, history and export

Restore destinations, history and verified exports were introduced in
`v0.5.0`. The expanded picker and typed confirmation below describe the
`v0.9.0` workflow. See the [installation guide](installation.md) for the
latest published version.

## Restore into a new database or file

```sh
dbvault list
dbvault restore --target BACKUP-NAME --new-database --dry-run
dbvault restore --target BACKUP-NAME --new-database --confirm
# Choose an explicit PostgreSQL/MySQL/MongoDB name (SQLite: new file path).
dbvault restore --target BACKUP-NAME --new-database --database app_restore_20261002_043000 --confirm
```

Generated names contain **restore time in UTC**, plus a random suffix; the source
backup's creation time remains visible in the plan and history. Example:
`app_restore_20261002_043000_8f21ab90`. SQLite places a similarly named file beside
the configured source path. Names are generated anew per invocation; reuse a
previewed name by supplying it explicitly with `--database`.

PostgreSQL creates from `template0` using a connection to `postgres`; MySQL uses
`information_schema` to check absence, then `CREATE DATABASE`. These require
appropriate connection/catalog/create permissions. New network database names
must be lowercase ASCII identifiers up to 63 characters and cannot be system
database names. SQLite refuses existing files and sidecars and uses exclusive
file creation with mode 0600. Destination directories must already exist.

MongoDB checks database absence through the authenticated driver, rechecks before
restore and uses native namespace remapping. MongoDB creates the database lazily
when restoring collections; an empty archive may create no database. It has no
atomic database-reservation operation: prevent concurrent creation or writes to
that namespace. All destinations require an operator-controlled server/filesystem
and no concurrent target replacement. Normal restore is not an isolation boundary
for untrusted archives; use trusted backups and the recovery drill for isolated
PostgreSQL/SQLite recovery testing.

The exact stored snapshot passes SHA-256/size checks and engine/version checks
before destination creation. Existing destinations are refused rather than
silently reused. Failed/cancelled restores preserve the new destination (possibly
empty or partially restored); DBVault never drops it automatically.

SQLite checks restored structure with `integrity_check`. Other engines currently
check native restore exit status and post-restore connectivity/tool compatibility.
These checks do not establish application data completeness; verify business
queries before resuming writes. PostgreSQL recovery drills additionally check
catalog/table readability. MySQL/MongoDB restores are not atomic as a whole.

## Protect an existing destination

```sh
dbvault restore --target BACKUP-NAME --database existing_db --backup-before-restore --dry-run
dbvault restore --target BACKUP-NAME --database existing_db --backup-before-restore --confirm
```

Before restore writes, DBVault creates a full backup of the **destination**, using
the configured storage/compression and all table/collection filters cleared. It
then reads the stored copy back and verifies SHA-256 and size. If creation,
registration or verification fails, restore stops. The original source backup
remains the already verified private snapshot throughout this work.

This option is incompatible with `--new-database`. MongoDB requires
`database.options.quiesced: true` and writes actually stopped; DBVault does not
stop application writes. Dry-run creates no safety backup, new target or history;
it checks the selected artifact and destination compatibility, but cannot prove
backup write permissions or future create permissions. Safety copies are normal
registered backups subject to configured retention. Record their names and keep
them as long as rollback is needed. DBVault does not automatically roll back a
failed restore. Use the safety backup name with a reviewed restore command.

`--clean` is also incompatible with `--new-database`; new destinations have no
objects to drop. MySQL and SQLite do not accept clean or selective restore flags.

## Terminal selection and output

```sh
dbvault restore                         # Select backup, destination and confirm
dbvault restore --interactive           # Explicitly request terminal selection
dbvault restore --target BACKUP-NAME --non-interactive --dry-run --output json
```

The backup picker shows aligned columns: creation time (UTC), engine/database,
backup type (full/incremental), whether the backup is encrypted, stored size and
name. When every backup comes from the same database, that source moves into the
title. At most ten rows are visible at once; press `/` to filter by typed text.
Narrow terminals give up the source, encryption, type and size columns in that
order and shorten long names in the middle; the name itself is always shown.
Paste a full backup name after `/` even when its row is shortened.
Use arrows or j/k, Enter to select, Esc/Ctrl+C to cancel. The command uses
the configuration it loaded for selection through execution.

New destination is the default; choosing an existing destination offers a safety
backup. A labeled restore plan then shows the backup, source, destination, mode,
safety backup and any table/schema/collection selectors; `--clean` adds a
highlighted warning. A new destination is confirmed with yes/no, defaulting to
cancel. Restoring into an existing destination requires typing the destination
database name (SQLite: the file name, without its directory); any other text
cancels without writing. Explicit `--confirm` already authorizes the operation
and skips the confirmation prompt, for automation as before.

JSON, quiet, non-terminal and `--non-interactive` invocations never prompt;
`--interactive` in these modes fails with an actionable error. Explicit targets
without `--interactive` keep the script-compatible flags/confirmation contract.
Text output shows the plan and actual pipeline stages; percentages, a progress
bar and an ETA are shown only when a byte total is known. After the first two
seconds of a stage the line also shows smoothed throughput of consumed bytes. Stream consumption progress is not an estimate
of server-side restore completion. JSON writes one structured result to stdout,
with diagnostics on stderr. `--no-color` and `NO_COLOR` remain supported.

## Restore history

```sh
dbvault history --limit 20
dbvault history --output json
```

CLI restore attempts with a validated source manifest append separate
`restore_history_<UTC-time>_<random-id>.json` records to configured storage,
including failed integrity checks, failures and cancellations. Records contain
the source manifest/checksum, destination host/port/name, selectors, clean flag,
UTC start/completion/duration, status, validation and optional safety backup.
No passwords or raw native error text are persisted. Invalid inputs/missing
manifests and dry-runs produce no history. Process termination or a storage outage
can prevent recording; history is operational evidence, not a tamper-proof audit
log. A history-write failure is reported separately from the restore status and
never triggers another restore. Records are kept when source backups are deleted;
they do not claim that the artifact still exists. Existing low-level `Service.Restore`
remains compatible; use `RestoreWithResult` for history and the new workflow.

History does not count as a passed isolated recovery drill for `health`.

## Export a backup

```sh
dbvault export --target BACKUP-NAME --file ./copy.dump.gz
dbvault export --target BACKUP-NAME --file ./copy.dump --decompress
```

Works with local, S3, GCS and Azure storage. Export verifies the exact stored
bytes into a private snapshot, then creates a new local file exclusively (0600).
Existing files are never replaced; partial owned outputs are removed on failure.
`--decompress` removes only outer gzip/zstd compression, validates raw size and
decompressor errors/trailers. Output remains PostgreSQL custom dump, MySQL SQL,
MongoDB archive or SQLite image; it does not convert PostgreSQL dumps into SQL.
No native database tools/passwords are needed. Budget temporary disk space for
the stored snapshot plus output and account for cloud reads/egress.

Native formats follow the vendor tools: [MySQL single-database SQL dumps](https://dev.mysql.com/doc/refman/8.0/en/mysqldump-sql-format.html)
omit database creation/selection statements, and [MongoDB namespace remapping](https://www.mongodb.com/docs/database-tools/mongorestore/mongorestore-examples/)
selects the destination database during restore.
