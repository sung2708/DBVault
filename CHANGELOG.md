# Changelog

All notable changes to DBVault will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Security
- Refresh glibc in the MySQL runtime image and APT and PCRE2 in the
  MongoDB runtime image. Remove MongoDB's unused `bsondump` and server
  startup YAML parser from the DBVault image.
- Move the PostgreSQL client image to the official PostgreSQL 16 Trixie
  base and apply Debian's APT and zlib updates. PostgreSQL client tools
  remain on major version 16.

## [v0.9.1] - 2026-11-11

### Security
- Update `go-jose/v4` to 4.1.9 for CVE-2026-116585, CVE-2026-116586 and
  CVE-2026-116587. Update `golang.org/x/crypto` to
  `v0.58.1-0.20261009181718-2f1f834330b2`, which removes the unmaintained
  OpenPGP packages reported in GO-2026-5932.
- Remove the unused `gosu` binary from the PostgreSQL, MySQL and MongoDB
  runtime images. Remove MySQL Shell and its bundled `certifi` from the
  MySQL runtime image. The images retain the native tools DBVault uses for
  backup and restore.

## [v0.9.0] - 2026-11-11

### Changed
- Interactive restore lists backups in aligned columns (created, source, type,
  encryption, stored size, name) that fit the terminal width, shows at most ten
  rows and filters with `/`, including full backup names shortened on screen.
  Backup selection reports when storage is loading and keeps the selected
  storage configuration through execution.
- The interactive restore plan uses labeled fields, omits empty selectors and
  highlights `--clean` as a warning.
- Interactive restores into an existing destination require typing the
  destination database name (SQLite: file name) instead of answering yes/no.
  `--confirm` still authorizes restores without a prompt.
- `export`, `inspect`, `verify` and `delete` offer the same backup picker when
  `--target` is omitted in a terminal, and accept `--non-interactive`. A picked
  deletion still requires a yes/no confirmation unless `--confirm` or
  `--dry-run` is given.
- Progress lines show smoothed throughput, and an ETA when the total is known,
  for stages running at least two seconds, and use `█░` bars on Unicode
  terminals.
- Replace the archived `gopkg.in/yaml.v3` with its maintained drop-in fork
  `go.yaml.in/yaml/v3`. Written configuration files are byte-identical.
- Update AWS, Google Cloud, Azure, MongoDB driver, SQLite and other direct
  dependencies with patch and minor upgrades.
- Correct stale documentation: the capability matrix now lists logical
  incremental chains and native `pitr` increments, and the roadmap and README
  describe the current release state.
- Run `govulncheck` in CI, update CI and release workflows to current GitHub
  Actions majors, and enable weekly Dependabot updates for Go modules,
  GitHub Actions and Docker base images.

### Security
- Update dependencies with known vulnerabilities reachable from DBVault:
  `klauspost/compress` 1.20.4, `golang.org/x/net` 0.62.0, `google.golang.org/grpc`
  1.85.0 and `go.opentelemetry.io/otel/sdk` 1.47.0.
- Build with the Go 1.26.11 toolchain instead of Go 1.26.0, picking up
  standard-library security fixes (`crypto/tls`, `crypto/x509`, `net/http`,
  `os`). The minimum `go` version stays 1.26.0.

## [v0.8.0] - 2026-11-09

### Changed
- Add a SQLite backup and restore quickstart.
- Cancel superseded pull request CI runs and keep the installed CLI smoke test offline.
- Use the verified snapshot directly when decompressing unencrypted logical
  backups, saving one stored-artifact-sized temporary copy during restore.

### Fixed
- Preserve backup archives when manifest publication cannot be confirmed.
- Bound logical restore downloads by the manifest's stored size.
- Resume interrupted backup deletions using durable deletion markers.
- Validate PITR retention archives without writing extracted payloads.
- Promote Docker Hub engine aliases only from the latest GitHub release.

## [v0.7.0] - 2026-11-09

### Added
- Opt-in `storage.s3.protect_deletes` to keep backups and manifests visible
  on Object Lock buckets while allowing DBVault operation locks to be released.
- Opt-in PostgreSQL PITR verification fixture against AWS S3 and KMS.

### Fixed
- Stop native capture at the supported chain limit and make interrupted native
  cleanup safe to retry. Logical cleanup verifies each archive once per run.
- Reject destructive cleanup when saving protected S3 schedules, and surface
  storage lock release failures.
- Serve metrics HTTP requests from a background snapshot with a freshness gauge.

### Changed
- Give each logical source a dedicated storage prefix or directory. Existing
  manifests remain readable. See [source isolation](docs/storage.md#isolate-logical-backup-sources).
- Omit `--cleanup` from backup jobs targeting protected S3 prefixes. Use dry-run
  cleanup to preview candidates.

Live AWS S3/KMS/Object Lock recovery remains unverified. See the
[verification procedure](docs/testing.md#release-verification-on-aws).

## [v0.6.0] - 2026-10-06

### Added
- Independent storage-only monitoring guide, read-only monitor profile and a
  freshness probe with JSON/exit codes and missing-probe deployment guidance.
- Native PITR health scope with explicit source identity, chain availability and
  optional read-only stored-byte verification; JSON reports on runtime health
  initialization failures and explicit `--state ""` for independent monitors.
- Native scheduler jobs (`--operation pitr`), automatic source-specific parent
  selection, optional baseline refresh, and verified whole-chain retention via
  `pitr cleanup` or post-backup `--cleanup`, with a shared storage lock.
- `pitr base/capture/list/restore`: instance-wide native baselines, incremental
  WAL/binlog/oplog ranges with pinned source identity and continuity checks,
  exclusive timestamp replay and fresh-target guards. PostgreSQL prepares an
  offline recovery directory; MySQL/MongoDB replay through native tools.
- AWS KMS and Vault Transit wrapping-key providers, authenticated v2 envelopes,
  previous v1 compatibility and remote key rotation support.
- MySQL and MongoDB recovery drills in newly created network-isolated Docker
  servers with fresh credentials, matching preloaded official images, structural
  validation, stopped failure targets and exact-backup recovery evidence.
- Optional client-side AES-256-GCM envelope encryption with authenticated stream
  termination, archive context binding, environment-referenced wrapping key IDs,
  key rotation and `encryption keygen` private key-file generation.
- `backup --type incremental` stores logical dump deltas for all engines,
  verified base identity and reconstructed SHA-256, with a 32-link limit,
  dependency-aware health/retention/deletion and a cross-process storage lock.
- `recovery drill --latest` and saved `schedule add --operation recovery` jobs
  with recurring authorization, unique destinations, success-only cleanup and
  configured recovery notifications. Backup schedules accept `--type incremental`.
- Prometheus `metrics` text output and optional `/metrics` HTTP endpoint,
  with opt-in durable operation outcomes, durations and stored-byte counters.

### Changed
- Project licensing is now Apache-2.0; release images also publish to Docker Hub.
- Restore authenticates and materializes the entire archive/dependency chain
  before preparing a destination. `export --decompress` produces a standalone
  decrypted/reconstructed native dump or SQLite image.
- Doctor checks the active encryption key; backup/inspect show encryption and
  base dependency details; schedule listings distinguish backup/recovery jobs.

Logical increments still scan the complete database. Native `pitr capture`
reads engine logs instead. Native jobs and whole-chain retention are available;
differential backups remain unsupported. See [operator instructions](docs/enhancements.md),
[native PITR](docs/pitr.md) and [managed keys](docs/managed-keys.md).

## [v0.5.0] - 2026-10-02

### Added
- Restore into new destinations with UTC date/time and unique names, explicit
  names via `--database`, read-only dry-run checks and refusal of existing targets.
- Optional full, verified destination backup with `--backup-before-restore`;
  failures abort before restore writes. Backup filters are cleared for this copy.
- Separate restore history with source manifest, destination, selectors, UTC
  timestamps, status, validation and safety backup; read with `dbvault history`.
- `dbvault export` verifies stored bytes before exporting to a new local file,
  optionally removing outer compression while retaining the native engine format.

### Changed
- Restore offers inline keyboard selection in a terminal, a source/destination
  plan, stage progress, post-restore checks and detailed results. JSON, quiet,
  non-terminal and `--non-interactive` invocations never prompt.
- Missing native-tool errors point to `dbvault doctor` and tool configuration.

## [v0.4.0] - 2026-10-02

### Added
- PostgreSQL recovery drills restore verified backups into newly created Docker
  servers with fresh credentials, no external network or host binds, read-only
  catalog/table readability validation, exact-backup health evidence and optional
  owned-container/anonymous-volume cleanup. Requires a preloaded official image
  matching the source major; failed targets are preserved for inspection.
- Tag-triggered GHCR publication for versioned PostgreSQL 16, MySQL 8.4,
  MongoDB 8 and SQLite images on Linux amd64/arm64, with provenance/SBOM.
  CI builds and smoke-tests each runtime on both architectures.

### Changed
- Installation and quick-start documentation distinguish published artifacts
  from checkout capabilities; security support policy uses the latest stable release.

## [v0.3.0] - 2026-10-02

### Added
- Optional `protection.verify_after_backup` reads completed stored artifacts back
  through the shared streaming size/SHA-256 verifier, preserves artifacts on
  verification failure and appends exact-backup immutable evidence consumed by
  Health/Status. Full cloud reads and request/egress costs are documented.
- `dbvault status` aggregates existing health, recent backup metadata, recovery
  evidence, storage type and advisory saved schedules. No archive hashing,
  database connection, restore or recovery drill runs by default.
- `dbvault init` displays OS-specific instructions for setting the configured
  database password environment variable in the current terminal, including
  hidden/masked prompts on macOS/Linux and Windows PowerShell.
- `dbvault init` discovers PostgreSQL, MySQL and MongoDB tools in `PATH` and
  supported known installation directories, validates version commands, and
  saves absolute paths for later doctor/test/backup/restore operations. Adds
  `--native-tool-dir` for explicit complete toolsets; SQLite remains embedded.
- `dbvault recovery drill` for SQLite backups: verified snapshot reuse, exclusively
  created isolated target, structural post-restore validation, dry-run, explicit
  owned-file cleanup, separate recovery records and exact-backup health evidence.
  Native server engines fail closed until safe isolation is implemented/tested.
- `dbvault health` with explicit `health.max_backup_age` policy, latest-backup
  availability/size evidence, stale detection, JSON and monitoring exit status.
  Optional `--verify` checks only the latest matching archive's size/SHA-256;
  default checks never read archive contents. Verify and health --verify results
  are persisted as immutable exact-backup evidence. Saved schedule state is advisory;
  checksum success never implies a passed recovery drill.

## [v0.2.0] - 2026-10-01

### Added
- `dbvault doctor` for secret-safe readiness checks of configuration, authenticated
  database/tool compatibility, storage access and temporary-directory usability.
  Cloud write/delete permissions and Slack delivery are never tested.
- `dbvault update check` for cached checks against the official latest stable
  GitHub release, with safe update instructions and machine-readable output.
- `dbvault init` with optional inline setup, flags-only automation, supported
  database/storage choices, environment password references, configuration review,
  optional database/tool testing and protected atomic YAML creation.

## [v0.1.0] - 2026-10-01

### Added
- Dedicated terminal presentation with cyan accents, concise summaries, responsive
  borderless tables and real stream-byte progress; automation-safe redirected output.
- `--output text|json`, `--quiet` and `--no-color`, including `NO_COLOR` support;
  JSON-mode errors are structured stderr records with secret redaction.
- Go module/VCS version metadata for `go install` and source builds; installation
  acceptance in CI, five-platform builds and validated SemVer release packaging
  with README/LICENSE and a portable `checksums.txt`.
- Official Release Governance, Documentation & Release Hygiene Policy
  (`docs/releasing.md`), establishing SemVer rules, immutable tags, documentation
  gates, repository hygiene, and agent release restrictions.
- MongoDB native archives and collection remapping, with private credential
  files and a real MongoDB 8.0 restore drill for every codec.
- SQLite consistent WAL-aware snapshots and online restore with corruption
  and locked-target cancellation tests.
- Official S3, GCS and Azure streaming providers, conditional publication,
  credential chains and emulator-backed SQLite restore drills for every codec.
- HTTPS Slack completion/failure notifications, redaction and outage isolation.
- Persistent schedule CRUD and a foreground five-field cron daemon with
  timezone support, overlap prevention and shutdown cancellation.
- Windows kill-on-close Job Objects and Unix process groups for descendant
  cancellation; native child-tree tests and engine-specific Docker targets.
- Go module and Cobra CLI with validated YAML/environment/CLI precedence.
- Secure cancellable native runner with bounded diagnostics and secret redaction.
- PostgreSQL custom archive backup/restore and table/schema restore selectors.
- Oracle MySQL 8.x/InnoDB SQL dump/restore, table backup selectors, native TLS,
  version compatibility checks and a real Docker backup-destroy-restore drill.
- Confined local storage, versioned metadata, mandatory SHA-256 verification,
  streaming none/gzip/zstd, retention, verify/inspect/delete/config commands.
- Unit/security/failure-injection tests, benchmarks, PostgreSQL Docker restore
  drill, Make targets, Docker packaging, and cross-platform CI builds.

### Changed
- Grouped root help, command/subcommand workflows and realistic examples;
  corrected configuration-derived defaults and destructive-operation wording.
- Actionable CLI errors with usage/help navigation, typo suggestions and valid
  compression/type/backend values; unknown commands now return failure.
- Clear inherited PostgreSQL host-address/service overrides so configured
  targets remain authoritative during backup, preflight and restore.
- Require Go 1.26+ for the selected dependencies and os.Root confinement.
- Document PostgreSQL custom archives, unique UTC backup filenames, private
  verified restore snapshots, and conservative PostgreSQL version compatibility.
- Reject arbitrary native extra_flags and omit skip-verify. Incremental and
  differential strategies fail explicitly rather than generating full dumps.

### Documentation baseline
- Comprehensive architecture and design blueprint for production-grade database backup CLI.
- Standardized configuration schema (`dbvault.yaml`) supporting environment-driven secret resolution.
- Clean Architecture package boundary specifications and Go interface definitions (`database.Adapter`, `storage.Provider`, `compression.Compressor`).
- Complete documentation suite including:
  - Getting started tutorial and OS installation guides (Linux, macOS, Windows).
  - CLI command reference with flags and exit codes.
  - Backup and restore lifecycle specifications with streaming pipeline designs.
  - Database adapter specifications for PostgreSQL, MySQL, MongoDB, and SQLite.
  - Storage provider documentation covering local filesystems, S3, GCS, and Azure Blob.
  - Security architecture, threat model, and permission enforcement rules.
  - Testing strategy including unit tests, race detection, and disaster recovery drills.
  - Development and extension guides.
  - Troubleshooting diagnostics matrix for operational errors.
  - Multi-phase project roadmap.
  - Architecture Decision Records (ADRs 0001 - 0006).
- Contributor guidelines (`CONTRIBUTING.md`) and Security vulnerability disclosure policy (`SECURITY.md`).
