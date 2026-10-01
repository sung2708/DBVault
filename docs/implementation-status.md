# Verified implementation status

Release snapshot `v0.3.0`, prepared on branch develop, 2026-10-02. Binary archives
are published by the tag-triggered workflow; Docker images are not published.

## Guided setup (`v0.2.0`)

`dbvault init` creates runtime-schema YAML with inline prompts or complete flags.
It supports all four database engines and storage backends, environment password
references, compression, a redacted review, optional reuse of database/native-tool
testing, and protected atomic config publication. Default path: `dbvault.yaml` in
the working directory. Non-TTY/JSON/quiet/non-interactive callers never prompt.

Verified in this work: fake-prompter coverage for all engine/backend combinations;
real SQLite init/load/test acceptance; concurrent no-overwrite config creation;
secret redaction, cancellation and missing-tool guards; Windows terminal arrows,
numbered fallback, masked password input and cancellation exit code 5. Full unit,
vet and race suites passed, as did the existing PostgreSQL/MySQL/MongoDB/cloud
integration suite. Windows amd64, Linux amd64 and macOS arm64 binaries built.
macOS runtime was not executed. An additional Linux binary/container smoke test
could not be verified because Docker stopped responding after the integration run.

## Official update check (`v0.2.0`)

`dbvault update check` compares the shared build version with GitHub's official
latest stable release. It validates the SemVer tag and exact release URL, caches
only public release metadata for 24 hours, and displays a pinned Go install
command plus the official release page for prebuilt binaries. Development builds
skip the network. The command never downloads or replaces a binary.

Verified with local HTTP fixtures for version ordering, stable-only filtering,
malformed/rate-limited responses, timeout/cancellation and cache behavior; CLI
JSON, human failure output and help; and a live read-only check returning the
published `v0.1.0` release.

## Readiness diagnostics (`dbvault doctor`, `v0.2.0`)

The command reports configuration, authenticated database/native-tool preflight,
storage access, temporary-directory readiness and Slack configuration. Cloud
storage is checked with a narrow list request only; cloud writes/deletes and Slack
delivery are deliberately not exercised. Local storage probes use a temporary
file that is removed. The report is available as human text or JSON; failed checks
return exit code 1 while warnings remain advisory.

### Protection overview (`dbvault status`, `v0.3.0`)

Status composes the current health report with up to five validated recent backup
manifests, exact-backup recovery evidence, storage type and matching saved
schedules. It does not connect to a database or read archive bytes. No separate
Protection Policy evaluator exists, so the field reports `not_configured` rather
than inferring general policy satisfaction. Integrity comes from immutable
stored-artifact verification records; absent exact-backup evidence remains
unknown unless an immediate metadata/listed-size failure is found.
Unreadable schedule state is a partial warning; saved schedules never imply a
live daemon. JSON, narrow terminal, redirected output and deterministic relative
times are covered by tests.

## Implemented scope

### Backup operational health (`v0.3.0`)

`dbvault health` evaluates the latest validated completed manifest matching the
single configured engine/database. Explicit `health.max_backup_age` controls
staleness; no default or inferred cron SLA is applied. Default checks read
sidecars/existence/listed size without opening archives. `--verify` reuses the
streaming integrity service for the candidate only. Verification state uses the latest immutable exact-backup record created by
`verify`, `health --verify` or configured post-backup verification; absent history
remains unknown. Restore-test state is unknown unless validated recovery records match the exact
selected backup identity/checksum. JSON, quiet/plain output, typed operational
errors and nonzero warning/critical/unknown monitoring results are supported.
Saved schedule definitions are advisory and never establish daemon liveness.

Coverage includes fixed-clock boundaries/timezones, future timestamps, fresh,
stale and absent backups, invalid sidecars, missing/size-mismatched/corrupt
artifacts, registry matching, cheap default reads, storage failures/cancellation,
schedule enabled/disabled state, configuration validation and terminal/JSON modes.

### Verify after backup (`v0.3.0`)

`protection.verify_after_backup: true` reads the registered stored artifact back
through the existing streaming SHA-256/size verifier after the immutable manifest
is published. A separate create-only JSON record carries the backup ID/name/hash,
engine/database, outcome, byte count, completion timestamp and bounded failure
category. Verification failures return nonzero without deleting the archive.
The same records are consumed by Health and Status. Cloud reads are full-object
reads and may incur time, API and egress cost. None of this is restore evidence.

Unit fixtures cover stored-byte corruption, read failure, evidence-write failure,
cancellation with evidence persistence, bounded streaming, Health/Status and JSON.
Cloud emulator integration exercises the option through S3, GCS and Azure.

### Safe recovery drill (`v0.3.0`)

The operator-facing `recovery drill` supports SQLite through a new explicitly
isolated file, using existing verified snapshot/restore machinery and read-only
structural/catalog validation. Confirmation, dry-run, owned successful-run-only
cleanup, failure preservation and separate immutable recovery records are
implemented. Health reads exact-backup records without claiming current checksum
verification or collapsing recovery/freshness into a single status.

| Capability | PostgreSQL | MySQL | MongoDB | SQLite |
|---|---|---|---|---|
| Safe Recovery Drill CLI | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | SUPPORTED |

SQLite real database/CLI fixtures pass for all codecs, with source protection,
exact data comparison and post-restore validation. Native engines fail closed
until proven target/credential isolation and corresponding actual drills exist;
previous native restore test-harness evidence does not imply new CLI support.

### Native tool discovery during setup (`v0.3.0`)

`dbvault init` now resolves a complete PostgreSQL/MySQL/MongoDB toolchain from
PATH or bounded known installation directories (including versioned PostgreSQL
Windows/Linux locations), probes each executable, checks toolset version
consistency, and saves absolute paths in `database.tools`. `--native-tool-dir`
selects an explicit complete installation. Operational adapters share the same
resolver, so doctor/test/backup/restore use saved paths. SQLite remains embedded.
Discovery is bounded: Windows checks immediate PostgreSQL/MySQL/MongoDB Tools
version directories under Program Files; Linux checks PATH, `/usr/bin`,
`/usr/local/bin`, and versioned `/usr/lib/postgresql` directories; macOS checks
PATH and common Homebrew/Postgres.app locations. Other custom locations use
`--native-tool-dir`.

Verified on Windows with Docker on 2026-10-01: the full expanded integration
suite passed for PostgreSQL 16, MySQL 8.4, MongoDB 8.0 and SQLite with none/gzip/
zstd, including health checks against actual completed backups and existing
full/selected restore dataset checks. SQLite recovery drills also passed through
S3 (LocalStack 4.7.0), GCS (fake-gcs-server 1.56.1) and Azure (Azurite 3.35.0),
including explicit cleanup and persisted exact-backup health evidence. Unit
fixtures cover target replacement before restore and age crossing the stale
threshold during verification. Windows symlink tests require host privileges.

| Area | Implementation | Evidence |
|---|---|---|
| CLI/configuration | Cobra, strict YAML, environment/flag precedence, typed exit codes | Guards, schema and every example tested |
| Pipeline | Streaming none/gzip/zstd, SHA-256 over compressed stored bytes | Round trips, corruption and failure injection |
| Metadata/management | Versioned manifests; list/inspect/verify/delete/retention | Newest protection, age/count boundaries, failure cleanup |
| Local | os.Root confinement, private files, fsync, no-overwrite hard links | Lifecycle, concurrent publication, traversal tests |
| PostgreSQL | Native custom dump; table filters; transactional pg_restore | PostgreSQL 16 full restore for every codec and selected clean restore |
| MySQL | Oracle 8.x/InnoDB logical dump and SQL restore | MySQL 8.4 full restore for every codec; MyISAM rejected |
| MongoDB | Native 100.x archives, collection filters and namespace remapping | MongoDB 8.0 full restore and remapping for every codec |
| SQLite | VACUUM INTO snapshot and online backup API restore | Real WAL fixture, corruption, locked-target cancellation |
| S3 | Official SDK, bounded multipart, conditional publication | LocalStack 4.7.0 lifecycle and SQLite restore for every codec |
| GCS | Official SDK, resumable upload, absent-generation condition | fake-gcs-server 1.56.1 lifecycle and SQLite restore for every codec |
| Azure | Official SDK, block blobs, conditional commit | Azurite 3.35.0 lifecycle and SQLite restore for every codec |
| Slack | HTTPS, redaction, independent timeout/outage handling | TLS test server; no real messages sent |
| Scheduling | Durable CRUD, cron/timezones, overlap gate, cancellation | Parser/state/CLI tests; embedded timezone data |
| Native supervision | Windows suspended-start Job Objects; Unix process groups | Windows test terminates forked descendants |

## Capability matrix

| Capability | PostgreSQL | MySQL | MongoDB | SQLite |
|---|---|---|---|---|
| Connection test | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Full backup | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Full restore | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Selective backup | SUPPORTED | SUPPORTED | PARTIAL | UNSUPPORTED |
| Selective restore | SUPPORTED | UNSUPPORTED | SUPPORTED | UNSUPPORTED |
| Incremental | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED |
| Differential | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED |
| Compression | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Checksum | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Local storage | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| S3 | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| GCS | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Azure | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Cancellation | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |
| Restore-tested | SUPPORTED | SUPPORTED | SUPPORTED | SUPPORTED |

Selected backup covers table filters for PostgreSQL/MySQL. MongoDB is PARTIAL:
one included collection per archive, or multiple exclusions; multiple included
collections are rejected. SQLite takes a snapshot before streaming. Cloud
providers use the common engine-independent pipeline; cloud E2E drills use
SQLite, while native full and selected drills exercise the other three engines.

Logical strategies explicitly reject incremental/differential requests.
No full dump is relabeled as an advanced backup. Recovery chains, client-side
encryption and a metrics exporter are future, separately scoped work.

## Executed checks

- PASS: subsequent CLI help audit of all 22 public nodes, both help routes,
  examples/defaults/required inputs, typo suggestions and secret-safe flag errors.
  Final full unit/vet checks and CLI/config race tests passed; Windows and Linux
  binary help acceptance passed. See [CLI help audit](cli-help-audit.md).

- PASS: MongoDB 8.0 full backup/destroy/restore and collection remapping,
  checking all 1,000 documents for none/gzip/zstd.
- PASS: SQLite WAL snapshot/restore, corruption rejection before writes,
  cancellation with a locked destination.
- PASS: cloud emulator chunked/multipart uploads, existing-object rejection,
  byte comparison, list/cancel/delete, and SQLite restores for all codecs.
- PASS: SDK HTTP fixtures for conditional writes, multipart abort on failure,
  Azure size mismatch and GCS generation conditions.
- PASS: cancelled S3 multipart upload is aborted with an independent context;
  the S3 emulator lifecycle and all-codec restore drills passed after this fix.
- PASS: PostgreSQL restore drills for all codecs and selected backup with a
  deliberately incorrect inherited PGHOSTADDR. Native commands remove it.
  A first rerun exceeded the five-minute test deadline under concurrent builds;
  the isolated rerun passed in 71 seconds with a ten-minute bounded deadline.
- PASS: Mongo credential-file lifecycle/argv tests, TLS Slack outage/redaction
  tests, scheduler parser/state/CRUD tests and Windows process-tree cancellation.
- PASS: aggregate Windows unit suite and full integration rerun, including
  PostgreSQL/MySQL selected-table backups and MongoDB selected-collection backup.
- PASS: full Windows race suite before the subsequent CLI help audit, and go vet
  after moving temporary build/link
  directories to D:. An initial linker run ran out of space on C:; the rerun
  completed successfully.
- PASS: unit coverage generation before the subsequent CLI help audit,
  63.1% total statements; app 83.5%, runner
  84.9%, notifications 82.1%, scheduler 75.3%, retention 95.8%. This profile
  excludes coverage from the separate cloud/database integration drills.
- PASS: built Windows binary version/help and all seven example configs.
- PASS: Windows amd64, Linux amd64 and macOS arm64 builds. Windows executable
  reports development version, actual dirty Git commit and UTC build time.
  macOS was cross-compiled, not executed on a Mac.
- PASS: checksum/compression/local-storage microbenchmarks on Windows; these
  short runs are not production throughput measurements.
- PASS: Linux native child-tree cancellation, redaction and environment isolation
  tests executed inside a non-root container.
- PASS: Linux SQLite WAL snapshot/online restore/locked-target cancellation
  and local storage lifecycle, concurrent no-overwrite, failure cleanup and
  symlink escape tests, executed inside a non-root container.
- PASS: four Docker runtime targets built before the subsequent CLI help audit
  with the cross-compiled Linux
  binary supplied as a named build context, and version output checked. Native
  clients verified: PostgreSQL 16.15, MySQL 8.4.11, Mongo Database Tools 100.18.0.
  The source-building Docker pipeline also completed earlier in this session.
  No images were published.

The PostgreSQL/MySQL full restore drills passed before these extensions; their
aggregate rerun adds dedicated selected-backup drills. GCS emulator 1.54.0 ignored
overwrite conditions; tests now pin 1.56.1, which honors them. The Windows
symlink test skips explicitly when the host lacks privileges. It passed in the
local Linux container run and is included in CI; remote CI has not run.

## Operational limits

MongoDB database-scoped backup requires stopped writes throughout the dump and
quiesced: true acknowledgement. MySQL requires InnoDB and no concurrent DDL.
MongoDB/MySQL restores are not atomic across all data. SQLite backup needs
snapshot disk; restore needs compressed and decompressed temporary images and
an existing destination. Stop application writes during restore. Storage must be
trusted: unsigned sidecars detect corruption, not malicious replacement. Secure
existing roots and Windows storage/temp ACLs.

S3 unknown-size streams use 128 MiB parts, one worker and at most 10,000 parts
(about 1.22 TiB). Crashes can leave unregistered artifacts, credential files or
abandoned upload sessions; configure lifecycle policies and inspect orphans.
SDK retries do not resume uploads across restarts. Scheduling is a foreground
singleton, loads state at startup, skips overlaps and does not replay missed runs.

Live cloud IAM/KMS, actual Slack delivery, macOS native execution,
multi-architecture published images and deployment operations are unverified.
ADRs 0007–0009 define the supported contract.
