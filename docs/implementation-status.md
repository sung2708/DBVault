# Verified implementation status

## Independent freshness monitoring (2026-10-06)

Existing logical health/JSON/metrics were reviewed before changes; no repository
or ancestor `AGENTS.md` was present. Logical freshness remains implemented through
completed manifests and `health.max_backup_age`. New native scope adds explicit
source selection, full parent/artifact checks and optional stored-byte hashing
without decryption keys or write access. Runtime preflight failures now produce
unknown JSON reports, and health diagnostics omit raw provider/config details
while retaining typed exit codes. `--state ""` skips local schedule metadata.

Full unit tests, vet, formatting/diff checks and Windows CLI build passed.
Uncached targeted race tests passed with `-work` after Windows prevented Go from
deleting a test executable in the normal cleanup path. Python's six monitor tests
passed, including seven real-CLI scenarios with no producer/database process:
fresh/overdue/absent logical backups, storage initialization failure, and
fresh/overdue/wrong-source native backups. HTTP storage fixtures verify typed
provider failure/timeout exits and private-diagnostic suppression. Native service
fixtures enforce read-only access, unknown wrapping keys, complete-chain hashing,
missing/corrupt ancestors, boundary/future timestamps and storage errors.
Monitor profile validation and CLI help were checked. Deployment on a real
independent host, live cloud IAM, physical producer shutdown and actual alert
delivery were not performed; the guide covers external supervision and limits.

## v0.6.0 enhancements (2026-10-06)

Version v0.6.0 adds MySQL/MongoDB isolated Docker recovery drills,
AES-256-GCM envelope encryption with environment-referenced key rotation,
logical dump incremental chains, Prometheus metrics and scheduled latest-backup
recovery jobs. See [operator instructions](enhancements.md) and
[ADR-0013](adr/0013-encrypted-incremental-recovery-operations.md).

Verified locally on Windows: the full unit suite, targeted race suites, vet,
build and protected sample configuration. Added coverage checks authenticated
truncation/tampering and metadata binding, encrypted chain reconstruction/export,
old-key requirements, ancestor corruption, dependency retention/deletion, lock
contention, dry-run behavior, target ownership/isolation, failure preservation,
scheduled SQLite recovery and HTTP metrics shutdown.

The final cleanup fixes were rechecked with an uncached full unit run and race
checks. Regression cases cover retained-chain ancestry, child-before-base
deletion ordering, cyclic dependencies, deletion previews of referenced bases,
and complete stored-chain verification with a corrupt ancestor.
The combined Docker integration suite was rerun successfully after these fixes
(513.562 seconds), including all three server engines, SQLite, and cloud storage
emulators. No recovery containers remained after completion.

Real Docker PostgreSQL/MySQL/MongoDB backup/destroy/restore workflows passed,
including encrypted logical increments and isolated recovery evidence. Native
fixtures compare restored data and preserve source data. S3/GCS/Azure emulator
workflows passed with encrypted full backups across all codecs and encrypted
increments, reconstruction, recovery drills and dependency-safe deletion.

During validation, MySQL recovery was corrected to require TLS, and MongoDB
readiness was corrected to wait for the final PID-1 daemon instead of its
entrypoint's temporary authenticated server. Native workflows passed after these
corrections. No release publication or live-cloud IAM/KMS validation was run.
Native `pitr` commands now add cluster/instance baselines and WAL/binlog/oplog
capture, plus AWS KMS and Vault Transit v2 envelope providers. See
[native recovery](pitr.md) and [managed keys](managed-keys.md) for prerequisites,
fresh-target requirements and format details. PostgreSQL prepares an offline
target; the operator starts the matching server and checks that replay reached
the target. Native retention/scheduling are available through native jobs and
whole-chain cleanup.
Differential backups and cross-timeline PostgreSQL restore remain unsupported.
Logical increments still scan the full database and use temporary
disk. Older release sections below describe their historical implementation.

## Restore workflows (`v0.5.0`)

Release v0.5.0 adds new destinations named with UTC date/time, optional
full verified destination backups, source/destination plans, terminal backup
selection, structured restore results/history and verified file exports.
See [restore workflows](restore-workflows.md) for engine creation permissions,
MongoDB lazy creation/concurrency, retention and validation limits.

Verified locally on Windows: unit/race suites, vet, module verification and build.
Native PostgreSQL, MySQL and MongoDB workflows and S3/GCS/Azure emulator workflows
passed in separate integration runs, including new destinations, refusal of
existing targets, full safety backups, rollback data checks, history and exports.
SQLite CLI acceptance also covered preview, restore, overwrite protection,
history, exports and automation output. Combined runs exceeded deadlines on the
local Docker host; pending groups were rerun separately and passed. Remote
release artifacts and container publication require the release workflow to pass.

Release snapshot `v0.4.0`, 2026-10-02. The tag-triggered workflow publishes binary
archives and versioned GHCR images after validation. Registry publication and
ARM64 runtime execution remain unverified locally; check the release workflow
and package access before using published artifacts.

## PostgreSQL recovery and container distribution (`v0.4.0`)

PostgreSQL recovery drills use a newly created Docker-isolated server with fresh
credentials and a preloaded official image matching the source major. They reuse
the private verified snapshot and restore pipeline, validate catalog/table
readability, and persist separate exact-backup evidence consumed by Health/Status.
Dry-run starts no server and checks image availability only. Explicit cleanup
removes the owned container and anonymous volumes after success; failures preserve
the target, stopped to prevent background restore after cancellation. MySQL/MongoDB
drills remain unsupported. Unpopulated materialized views are counted as catalog
objects and skipped during table scans. See ADR-0011.

The release workflow now prepares versioned GHCR images for all four engine
targets on Linux amd64/arm64, with SBOM/provenance and per-architecture CI smoke
checks. The `v0.4.0` tag triggers the first official container publication;
artifacts become available only after the workflow succeeds. The sections below
retain the implementation and verification history of earlier releases.

Verified locally on Windows with Docker: unit/race suites, `go vet`, module
verification, workflow validation with actionlint and documentation file links
passed. The full native/cloud integration suite passed; the final PostgreSQL
fixture also passed all codecs through CLI JSON/cleanup with an unreachable
configured source host, unpopulated materialized view, exact restored fixture
comparison and unchanged source data. Four Linux amd64 runtime images were
packaged from the cross-compiled binary using the named build context and
smoke-tested for CLI, non-root users and bundled tools. ARM64 runtime execution
is configured in CI but was not run locally; GHCR publishing was not exercised.

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

## Native PITR and managed-key verification (2026-10-06)

Native automation now includes saved `pitr` jobs, automatic source-specific parent
selection, explicit periodic baseline refresh and optional post-backup retention.
Native retention counts complete baseline chains, protects the newest baseline
per source (including timestamp ties), validates ancestry, and verifies archives
before child-first deletion. A shared storage lock protects publication,
cleanup and restoration across CLI processes. Unit and race checks cover initial
baseline creation, refresh, no silent fallback after capture errors, source
selection, empty ranges, retained-archive corruption, missing parents, cycles,
chain limits, previews, deletion ordering, lock contention and persisted job
options. A Windows regression uncovered stale TAR file sizes while the dump
writer remained open; MySQL dumps and MongoDB oplog writers now close before
publication. Full unit tests, targeted race checks, vet and Windows build passed.
Docker automation fixtures passed MySQL with GTID OFF/ON and MongoDB. PostgreSQL
initially exceeded the old two-minute startup-readiness window while fsyncing its
physical directory on a Windows bind mount; the fixture now allows five minutes.
Its final isolated rerun passed in 331.784 seconds, verifying paused WAL replay
and exact restored IDs. MySQL/MongoDB passed again with the final writer-close
fixes in 438.432 seconds. The fixtures use `pitr backup` for both initial baseline
creation and automatic-parent log capture with post-publication cleanup, then
check timestamp recovery and unchanged source data.

Docker fixtures exercised native PostgreSQL WAL, MySQL binlogs with GTID both
OFF and ON, and MongoDB majority-committed oplogs. Each fixture captures changes
after one baseline and checks exact restored IDs before an exclusive timestamp,
while preserving the source rows. PostgreSQL fixtures start the prepared cluster
and confirm that recovery pauses at the requested target. The combined native
and managed-key integration run passed in 521.996 seconds. After adding target
visibility checks, MySQL (GTID OFF/ON), MongoDB and managed keys passed again;
the aggregate run exhausted its previous 15-minute fixture deadline before
PostgreSQL restore. The isolated PostgreSQL rerun passed in 98.229 seconds.
The fixture deadline is now 25 minutes and CI allows 30 minutes.

Managed-key fixtures use LocalStack KMS and a real local Vault Transit server,
including key rotation and encrypted logical full/incremental restoration.
Unit coverage checks provider protocols, metadata binding, tampering and endpoint
restrictions. Targeted race checks and vet passed. See [ADR-0014](adr/0014-native-pitr-managed-keys.md).

Live cloud IAM/KMS, actual Slack delivery, macOS native execution,
multi-architecture published images and deployment operations are unverified.
ADRs 0007–0009 define the supported contract.
