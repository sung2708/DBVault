# Changelog

All notable changes to DBVault will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

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
