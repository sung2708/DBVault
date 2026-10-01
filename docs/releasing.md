# Release Governance, Documentation & Release Hygiene Policy

This document establishes the official **Release Governance Policy** for DBVault. It defines the rules, standards, and operational gates governing project releases, versioning, documentation synchronization, cryptographic hygiene, and repository cleanliness.

### Core Principle

> **"A release should be clean, complete, documented, reproducible, and useful — not artificially minimal."**

---

## 1. Release Is Intentional

A code change does not constitute or imply a release.

### Standard Development Cycle

```text
Implement
   ↓
Automated & Manual Tests
   ↓
Update Documentation (if applicable)
   ↓
Update CHANGELOG.md [Unreleased] (if notable)
   ↓
Stop & Await Review
```

### Prohibited Automated Flow

```text
Code Change
   ↓ (PROHIBITED AUTOMATION)
Bump Version
   ↓ (PROHIBITED AUTOMATION)
Tag Repository
   ↓ (PROHIBITED AUTOMATION)
Publish Release
```

Releases occur **only** when a maintainer expresses explicit release intent. When uncertain:

```text
DO NOT RELEASE
```

---

## 2. Versioning Strategy (Semantic Versioning)

DBVault strictly adheres to [Semantic Versioning 2.0.0](https://semver.org/):

```text
vMAJOR.MINOR.PATCH
```

Examples: `v0.1.0`, `v0.2.0`, `v0.2.1`, `v1.0.0`, `v1.1.0`, `v2.0.0`.

### Rules by Release Type

| Level | Scope | Criteria |
| :--- | :--- | :--- |
| **PATCH** | `vX.Y.Z+1` | Backward-compatible bug fixes and security hotfixes. |
| **MINOR** | `vX.Y+1.0` | Backward-compatible new features, storage providers, or database engines. |
| **MAJOR** | `vX+1.0.0` | Incompatible API, CLI flag, or storage metadata breaking changes (post-v1). |

### Initial Development (`v0.x`) Rules

- **PATCH:** Backward-compatible bug fixes and minor corrections.
- **MINOR:** New functional features or significant architectural capabilities.
- **Breaking Changes in `v0.x`:** Must be explicitly labeled with `BREAKING` in both the `CHANGELOG.md` and GitHub Release notes.
- **Promotion to `v1.0.0`:** Must **never** be automated by scripts or agents. Elevating the project to `v1.0.0` requires an explicit, deliberate decision by project maintainers.

---

## 3. Determining Version from Complete Delta

When a release is explicitly requested, evaluate the **entire delta** since the most recent release tag. Never base version increments solely on the latest commit.

### Inspection Commands

```bash
# Locate previous release tag
git describe --tags --abbrev=0

# Inspect complete commit history since previous tag
git log <previous-tag>..HEAD --oneline

# Inspect complete diff summary and file statuses
git diff --stat <previous-tag>..HEAD
git diff --name-status <previous-tag>..HEAD
```

### Evaluation Criteria

Version bump magnitude is determined exclusively by:
1. Public interface compatibility (CLI commands, flags, configuration schema, exit codes).
2. User-visible runtime behavior and error reporting.
3. Feature additions or engine/storage support.
4. Defect corrections and reliability fixes.
5. Security remediations and permission adjustments.
6. Breaking changes or format migrations.

SemVer is **never** based on lines of code added or removed.

---

## 4. Git Tag Rules & Immutability

Release tags must conform strictly to:

```text
vMAJOR.MINOR.PATCH
```

*Examples:* `v0.5.0`, `v0.5.1`, `v0.6.0`.

### Prohibited Tag Formats

Do not create or push ambiguous or informal tags such as:
- `final`
- `latest`
- `release1`
- `final2`
- `stable-new`
- `production-final`

### Tag Immutability

Published Git tags are permanently immutable:
- **Never move an existing tag.**
- **Never reuse a tag name.**
- **Never force-push (`git push --force`) a tag.**
- **Never delete and recreate a tag** to disguise a bug or release flaw.

If an issue is discovered in `v0.5.0` immediately after publication, the correct remediation is to publish `v0.5.1`.

---

## 5. Changelog Governance (`CHANGELOG.md`)

DBVault maintains a single, curated [CHANGELOG.md](file:///d:/git/DBVault/CHANGELOG.md) adhering to [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

### Structure & Sections

1. **Active development** entries are recorded under:
   ```markdown
   ## [Unreleased]
   ```
2. **Release preparation** freezes the unreleased delta under:
   ```markdown
   ## [vX.Y.Z] - YYYY-MM-DD
   ```
3. **Standard categories** (include only sections that contain notable entries):
   - `Added` — New user-facing features, adapters, or commands.
   - `Changed` — Changes in existing functionality or operational defaults.
   - `Fixed` — Bug fixes, concurrency fixes, or resilience enhancements.
   - `Security` — Vulnerability mitigations, permission hardening, or sanitization.
   - `Deprecated` — Features slated for removal in future releases.
   - `Removed` — Removed capabilities or deprecated flags.
   - `Breaking Changes` — Incompatible adjustments to schemas, CLI, or behavior.

Internal implementation noise (e.g., typos, formatting, private refactoring without behavior change) should not clutter user-facing release changelogs.

---

## 6. Standard GitHub Release Notes

Every GitHub Release must use a standardized title and description layout:

### Title Format

```text
DBVault vX.Y.Z
```

### Description Template

```markdown
# DBVault vX.Y.Z

Short executive summary of the release scope and significance.

## Highlights

- Key feature or critical fix 1
- Key feature or critical fix 2

## Added

- Detailed addition item

## Changed

- Detailed change item

## Fixed

- Detailed fix item

## Security

- Security hardening or vulnerability fix (if applicable)

## Breaking Changes

- Description of breaking behavior and necessary operator action (if applicable)

## Upgrade Notes

Specific migration instructions, config schema adjustments, or prerequisite updates.

## Installation

### Go Install
```bash
go install github.com/sung2708/DBVault/cmd/dbvault@vX.Y.Z
```

### Pre-built Binaries
Download the pre-compiled binary matching your operating system and architecture from the Assets section below.

### Verify Installation
```bash
dbvault version
dbvault --help
```

## Documentation

- [Updated Feature Guide](docs/...)
- [Updated CLI Reference](docs/cli-reference.md)

## Checksums / Artifacts

Verify asset integrity using the published `checksums.txt`:
```bash
sha256sum -c checksums.txt
```

## Full Changelog

https://github.com/sung2708/DBVault/compare/vPREVIOUS...vX.Y.Z
```

*Rule:* Do not fabricate release notes. Omit empty sections rather than leaving placeholder text.

---

## 7. Documentation Is Release Material

Technical documentation is **not** expendable clutter. It represents first-class project assets essential for operators, administrators, and contributors.

The following documentation assets must be tracked and preserved in every release snapshot:

```text
README.md
CHANGELOG.md
LICENSE
SECURITY.md
CONTRIBUTING.md

docs/
├── architecture.md
├── getting-started.md
├── installation.md
├── configuration.md
├── cli-reference.md
├── backup.md
├── restore.md
├── databases.md
├── storage.md
├── security.md
├── scheduling.md
├── testing.md
├── development.md
├── troubleshooting.md
├── roadmap.md
├── releasing.md
└── adr/
    ├── 0001-use-go.md
    ├── 0002-native-database-tools.md
    ├── 0003-streaming-backup-pipeline.md
    ├── 0004-storage-abstraction.md
    ├── 0005-secret-management.md
    ├── 0006-scheduling-strategy.md
    ├── 0007-verified-local-foundation.md
    ├── 0008-mysql-full-logical-strategy.md
    └── 0009-mongodb-sqlite-cloud-scheduling.md
```

Documentation files must never be pruned or excluded simply because the compiled runtime binary does not consume them.

---

## 8. Architecture Documentation Must Be Preserved

System architecture, design blueprints, and Architecture Decision Records (ADRs) document the rationale behind technical trade-offs.

Retain in the source repository:
- `docs/architecture.md`
- All records in `docs/adr/`
- Mermaid pipeline diagrams and component relationships
- Data streaming and cancellation models
- Backup and restore lifecycle flows
- Storage abstraction specifications
- Metadata schema specifications (`.meta.json`)
- Engine and storage capability matrices

Architecture documentation is permanent engineering IP, never disposable overhead.

---

## 9. Security Documentation Must Be Preserved

Security procedures and threat boundaries must remain accessible in the source repository:
- `SECURITY.md` — Responsible disclosure process and supported versions.
- `docs/security.md` — Threat model, subprocess privilege isolation, credential safety, filesystem hardening, and checksum validation.
- Integrity verification specifications and sidecar layout.

Security documentation is mandatory release material.

---

## 10. Cryptographic Material & Secrets Handling

Files with cryptographic or certificate extensions must be evaluated based on **purpose**, not extension alone.

### Safe / Public Project Material (Permissible to Retain)

- Public CA certificates required for documented runtime connectivity (e.g., custom database TLS).
- Public signing certificates and public verification keys.
- Self-contained test certificates designed explicitly and solely for automated integration test suites.
- Checksum manifests (`checksums.txt`) and public signature files.

### Secret Material (STRICTLY FORBIDDEN from Commits & Releases)

Under no circumstances may any of the following be committed or included in any release snapshot:
- Production or staging private keys (`*.key`, `*.pem`, `*.pfx`, `*.p12`).
- Cloud provider access tokens or service account keys (`credentials.json`, `*.pem`).
- Database passwords, authorization headers, or Slack webhook URLs.
- Local configuration files containing production credentials (`dbvault.yaml`, `.env`).

### Sensitive Extensions Requiring Immediate Audit

Exercise special vigilance over:
```text
*.key
*.pem
*.p12
*.pfx
*.jks
.env
.env.*
credentials.*
```

### Secret Detection Protocol

If an unredacted credential or private key is detected during release preparation:
```text
STOP RELEASE IMMEDIATELY
```
Do **not** output the secret value into logs, transcripts, or reports. Remediate the exposure before proceeding.

---

## 11. README Audit on Every Release

> **"Every release preparation MUST review README.md."**

A release must not be published if `README.md` is outdated or misleading.

### Audit Checklist for `README.md`

- [ ] **Project Overview:** Accurately reflects current capabilities.
- [ ] **Installation:** Supported Go version, pre-built binary options, and package steps work.
- [ ] **CLI Examples:** Syntax, flags, and subcommand names match the compiled binary.
- [ ] **Supported Engines:** PostgreSQL, MySQL, MongoDB, SQLite accurately reflect operational status.
- [ ] **Supported Storage:** Local filesystem, AWS S3, GCS, Azure Blob accurately documented.
- [ ] **Security Model:** Credential mechanisms, file permissions, and confirmation flags match reality.
- [ ] **Documentation Links:** Internal links to `docs/` resolve correctly.
- [ ] **Version References:** Badges and installation instructions reference accurate version targets.

*Rule:* If the implementation changes, update `README.md` before releasing. If `README.md` is already accurate, avoid cosmetic edits solely to produce a diff.

---

## 12. Full Documentation Audit Checklist

Before tagging any release, perform a complete documentation audit:

```text
[ ] README matches current features and CLI commands
[ ] CLI documentation matches current commands and flags
[ ] Configuration docs match dbvault.yaml schema
[ ] Architecture docs still match implementation
[ ] Backup documentation matches actual streaming pipeline
[ ] Restore documentation matches safety checks and preflight rules
[ ] Database capability matrix is accurate
[ ] Storage provider documentation is accurate
[ ] Security documentation remains truthful to the threat model
[ ] Installation instructions execute cleanly on fresh environments
[ ] Examples use real flags, commands, and redacted placeholders
```

If a release introduces new architectural decisions, author an ADR in `docs/adr/`. If CLI flags change, update `docs/cli-reference.md`.

---

## 13. Documentation Must Not Lie

Documentation must never advertise a feature as operational when it is partial or planned.

Consistently apply explicit support tiers:
- **SUPPORTED:** Fully implemented, validated with unit and integration tests.
- **PARTIAL:** Implemented with specific documented constraints or caveats.
- **PLANNED:** On the roadmap for future milestones; not available in the current build.
- **UNSUPPORTED:** Explicitly outside project scope or unsupported by upstream native tooling.

Documentation must describe the code as it runs today, not future intentions.

---

## 14. Examples and Configurations Are Not Clutter

The following reference files are vital user aids and must be retained:
- `configs/example.yaml`
- Engine-specific configuration templates (`configs/postgres.yaml`, `configs/mysql.yaml`, etc.)
- Deployment examples and Docker compose manifests (`docker-compose.yml`)

### Pre-Release Verification of Examples

- Contain no real credentials, tokens, or private endpoints.
- Contain only valid flags and active schema keys.
- Do not reference obsolete or removed commands.

---

## 15. Tests Are Project Assets

Never delete or exclude tests because production binaries do not execute them:
- `*_test.go`
- `test/integration/`
- Test fixtures and mock data in `testdata/`

Automated test suites guarantee reproducibility, maintainability, and regression detection across versions.

---

## 16. Build, CI, and Packaging Infrastructure

The current implementation uses `.github/workflows/release.yml` directly;
GoReleaser is not configured, and a second packaging system is unnecessary.
Only explicit stable SemVer tags trigger packaging. Validation requires a matching
prepared changelog section, module hygiene, formatting, vet, unit/race tests and
database/cloud drills. Archives contain the executable, README.md and LICENSE;
`checksums.txt` uses archive basenames so verification works after download.
CI ordinary branch builds never publish. The module's executable install path is
`github.com/sung2708/DBVault/cmd/dbvault@VERSION`.

The following operational tooling files must be preserved in source snapshots:
- `Makefile`
- `Dockerfile`
- `docker-compose.yml`
- `.github/workflows/` (CI/CD pipelines)
- `.goreleaser.yml` (automated packaging definitions)
- `go.mod` and `go.sum`
- Utility scripts in `scripts/`

These files enable reproducible compilation, testing, and container builds.

---

## 17. What Should Actually Be Ignored

Release hygiene focuses on eliminating **transient**, **local**, or **AI-generated** clutter:

### AI-Only Material (To Be Removed / Ignored)

- Temporary LLM or agent prompt experiments (`*.prompt.md`, `.prompts/`, `.ai/`, `.agent/`).
- Conversation transcripts or exports not intended for repo tracking.
- Transient scratchpads and one-off agent notes (`AI-NOTES.md`, `CODEX-NOTES.md`).

---

## 18. Handling Agent Instruction Files

Files such as `AGENTS.md`, `CLAUDE.md`, or `.github/copilot-instructions.md`:
- **Retain** if they define persistent repository guidelines, architecture conventions, or workflows valuable to future developers and automated tools.
- **Remove** only if they are one-off scratch notes or task-specific prompts.

Evaluate by enduring utility, not by filename.

---

## 19. Temporary and Local Files

Local runtime and editor artifacts must never be tracked or released:
- Log files (`*.log`)
- Temporary files (`*.tmp`, `*.temp`, `tmp/`, `.temp/`)
- Coverage profiles (`coverage.out`, `coverage.html`)
- Operating system noise (`.DS_Store`, `Thumbs.db`)
- Editor workspaces (`.vscode/`, `.idea/`)

Ensure these patterns are accounted for in `.gitignore`.

---

## 20. Generated Build Artifacts

Locally compiled binaries and archives must not be checked into Git:
- `bin/`, `dist/`, `build/`
- Compiled executables (`main.exe`, `dbvault`, `*.exe`, `*.dll`, `*.so`)
- Local archives and compressed packages

CI build systems (e.g., GoReleaser via GitHub Actions) reproducibly build and attach release packages to GitHub Releases from clean source tags.

---

## 21. Redundant Files Procedure

Before removing any file deemed obsolete or redundant:
1. Search the entire codebase for references across Go source files, tests, scripts, `Makefile`, `Dockerfile`, CI workflows, and documentation.
2. Confirm the file is genuinely unused and not required for build, test, or documentation verification.
3. If uncertainty remains, classify as **REVIEW** and consult the maintainer. Never delete on an unverified hunch.

---

## 22. `.gitignore` Governance

- Transient, local, or generated files must be ignored via `.gitignore`.
- Adding a pattern to `.gitignore` does not automatically untrack previously committed files. Use `git rm --cached <file>` when necessary.
- Never use `.gitignore` as a packaging mechanism to strip tracked files out of a release tag.

---

## 23. Git Tag Semantics & Scope

A Git tag is an immutable reference to an exact commit object representing a complete snapshot of tracked files.

Maintain clear distinctions between:
```text
Source Repository (All tracked engineering assets)
       ≠
Git Tag Snapshot (Immutable commit state)
       ≠
GitHub Source Archive (Tarball/zip of Git tree)
       ≠
Binary Release Package (Minimal distribution archive)
```

Packaging rules belong in release tooling (e.g., GoReleaser), not in Git tracking acrobatics.

---

## 24. Binary Release Packages Must Be Minimal

Pre-compiled binary archives distributed to end users should be lean and focused:

### Example Binary Packages

```text
dbvault_vX.Y.Z_linux_amd64.tar.gz
dbvault_vX.Y.Z_linux_arm64.tar.gz
dbvault_vX.Y.Z_darwin_amd64.tar.gz
dbvault_vX.Y.Z_darwin_arm64.tar.gz
dbvault_vX.Y.Z_windows_amd64.zip
checksums.txt
```

### Standard Archive Contents

- `dbvault` (or `dbvault.exe`)
- `README.md`
- `LICENSE`

Binary distributions omit test suites, internal docs, development configs, and build scripts.

---

## 25. Source Releases Must Be Complete

Conversely, the tagged source repository snapshot must be comprehensive:
- Complete Go source code
- Automated test suites, benchmarks, and test fixtures
- Full documentation suite (`docs/`) and ADRs (`docs/adr/`)
- Example configuration profiles (`configs/`)
- Build and container tooling (`Makefile`, `Dockerfile`, `docker-compose.yml`)
- CI/CD workflow definitions (`.github/workflows/`)
- Contributor guidelines and licensing (`CONTRIBUTING.md`, `LICENSE`, `SECURITY.md`)

> **Binary artifact minimal, source snapshot complete.**

---

## 26. Release Hygiene Audit Procedure

Before creating any release tag, conduct a structured repository audit:

```bash
git status
git ls-files
git diff
git diff --cached
```

When comparing against a prior release:
```bash
git diff --stat <previous-tag>..HEAD
git diff --name-status <previous-tag>..HEAD
git log --oneline <previous-tag>..HEAD
```

### Hygiene Classification Categories

| Category | Action | Definition |
| :--- | :--- | :--- |
| **KEEP** | Retain as tracked | Legitimate source code, tests, docs, configs, or CI assets. |
| **UPDATE** | Edit before release | Outdated documentation, version strings, or schema examples. |
| **IGNORE** | Add to `.gitignore` | Local, temporary, or build-generated artifacts. |
| **REMOVE** | Untrack / delete | Confirmed redundant, temporary, or accidental files. |
| **REVIEW** | Escalate to maintainer | Uncertain files requiring maintainer clarification. |

---

## 27. Release Documentation Gate

**A release tag must not be created if critical documentation diverges from actual implementation.**

Specifically, audit:
1. `README.md`
2. `docs/installation.md`
3. `docs/configuration.md`
4. `docs/cli-reference.md`
5. `docs/architecture.md`
6. `docs/security.md`
7. `docs/backup.md` & `docs/restore.md`
8. `docs/databases.md` & `docs/storage.md`

Documentation does not require perfection, but knowingly releasing misleading guidance is strictly forbidden.

---

## 28. Release Security Gate

Release preparation must halt immediately if any of the following are detected:
- Committed secrets, private TLS/SSH keys, or API tokens.
- Real production credentials in configuration examples.
- Hardcoded sensitive values in code or tests.

The issue must be thoroughly remediated before the release gate can be passed.

---

## 29. Release Readiness Verification

Before a release can be staged or published, execute the applicable verification suite:

```bash
# Code formatting
gofmt -s -w .

# Static analysis
go vet ./...

# Unit test suite
go test ./...

# Race condition detection
go test -race ./...

# Local binary build validation
go build -o ./bin/dbvault ./cmd/dbvault

# CLI smoke test
./bin/dbvault --help
./bin/dbvault version
```

### Readiness Checklist

- [ ] Code compiles without errors
- [ ] Unit tests pass cleanly
- [ ] Race detector reports zero data races
- [ ] `go vet` reports zero issues
- [ ] CLI help output is complete and accurate
- [ ] CLI version command reports accurate version string
- [ ] `README.md` audited and synchronized
- [ ] `CHANGELOG.md` entry prepared with release date
- [ ] Documentation suite audited against code
- [ ] Architecture and ADR records audited
- [ ] Security documentation audited
- [ ] Configuration examples verified clean and safe
- [ ] Release notes prepared using standard template
- [ ] Repository hygiene audit passed (no clutter, no untracked binaries)
- [ ] Secret scan passed (no credentials or private keys)
- [ ] Target SemVer version verified correct
- [ ] Git tag does not already exist

---

## 30. End-to-End Release Workflow

```text
Development & Testing
         ↓
Explicit Release Request (Maintainer Decision)
         ↓
Inspect Previous Tag & Complete Delta
         ↓
Determine Target SemVer (PATCH / MINOR / MAJOR)
         ↓
Release Hygiene Audit (KEEP / UPDATE / IGNORE / REMOVE / REVIEW)
         ↓
README.md Audit & Synchronization
         ↓
Documentation & ADR Audit
         ↓
Security & Secret Scan Gate
         ↓
Update CHANGELOG.md ([Unreleased] -> [vX.Y.Z])
         ↓
Prepare GitHub Release Notes
         ↓
Run Verification Suite (gofmt, go vet, go test -race, build)
         ↓
Review Clean Git Working State
         ↓
Create Annotated Git Tag (`git tag -a vX.Y.Z -m "DBVault vX.Y.Z"`)
         ↓
Push Git Tag (`git push origin vX.Y.Z`)
         ↓
Automated CI / GoReleaser Pipeline
         ↓
Build Cross-Platform Binaries & Checksums
         ↓
Publish GitHub Release with Release Notes
```

---

## 31. Prepare vs. Publish Separation

Release management distinguishes between **preparation** and **publication**:

### Release Preparation (`prepare release`)
- Inspect delta and calculate candidate version.
- Conduct repository hygiene and security audits.
- Synchronize `README.md`, documentation, and `CHANGELOG.md`.
- Draft GitHub Release notes.
- Execute full test and validation suites.
- Verify reproducible local builds.

### Release Publication (`publish release`)
- Create Git release tag.
- Push tag to remote repository.
- Trigger CI packaging pipelines.
- Publish GitHub Release assets.

**Publishing strictly requires explicit maintainer authorization.**

---

## 32. Coding Agent Rules

All AI coding assistants and automated agents operating in DBVault must strictly adhere to the following rules:

> **Coding agents must not create or publish Git tags/releases as a side effect of ordinary development tasks.**

> **Release cleanup must remove only confirmed AI-only, temporary, local, generated, redundant, or accidental material. Documentation, architecture records, security documentation, public certificates, examples, tests, build tooling, CI, and other legitimate project assets must not be removed merely because they are not required by the runtime binary.**

> **Every release preparation must audit README.md and relevant documentation against the actual implementation.**

---

## 33. Absolute Prohibitions

**Never:**
- Delete documentation because the runtime binary does not need it.
- Delete architecture documents or ADRs as clutter.
- Delete test suites or fixtures as clutter.
- Delete CI, build, or release infrastructure as clutter.
- Delete cryptographic files based solely on file extension.
- Commit private keys, passwords, or cloud credentials.
- Delete uncertain files automatically without maintainer review.
- Create a release tag as a routine byproduct of a normal code change.
- Silently increment version numbers.
- Silently create or push Git tags.
- Force-push or overwrite published tags.
- Fabricate CHANGELOG entries or release notes.
- Publish a release with knowingly outdated `README.md`.
- Publish a release with knowingly misleading technical documentation.

---

## 34. Decision Matrix

| File Classification | Policy Action |
| :--- | :--- |
| **Legitimate project material** (source, docs, tests, CI, examples) | **KEEP** |
| **Accurate but outdated material** (docs, configs, changelog) | **UPDATE** |
| **AI-only, temporary, or local scratch files** | **IGNORE** or **REMOVE** |
| **Reproducible local build artifacts** | **IGNORE** |
| **Confirmed obsolete duplicate files** | **REMOVE** (after reference check) |
| **Uncertain file** | **REVIEW** (do not delete) |
| **Secret or private credential** | **STOP RELEASE IMMEDIATELY** |

---

## 35. Release Report Format

Upon concluding release preparation or publication, generate a factual report adhering strictly to this schema:

```text
Version: vX.Y.Z
Previous version: vA.B.C
Release type: PATCH / MINOR / MAJOR
Tag: vX.Y.Z
Release status: PREPARED / PUBLISHED

Validation:
- Build: PASS / FAIL / NOT RUN
- Unit tests: PASS / FAIL / NOT RUN
- Integration tests: PASS / FAIL / NOT RUN
- Race detector: PASS / FAIL / NOT RUN
- Vet / Lint: PASS / FAIL / NOT RUN

Documentation:
- README: AUDITED & SYNCHRONIZED
- CHANGELOG: AUDITED & READY
- Architecture: AUDITED
- Security: AUDITED
- CLI / Config docs: AUDITED

Release Hygiene:
- AI-only files removed/ignored: [List or None]
- Temporary/generated files ignored: [List or None]
- Redundant files removed: [List or None]
- Files requiring maintainer review: [List or None]
- Secret scan concerns: NONE DETECTED

Artifacts:
- dbvault_vX.Y.Z_linux_amd64.tar.gz
- dbvault_vX.Y.Z_linux_arm64.tar.gz
- dbvault_vX.Y.Z_darwin_amd64.tar.gz
- dbvault_vX.Y.Z_darwin_arm64.tar.gz
- dbvault_vX.Y.Z_windows_amd64.zip
- checksums.txt
```

*Rule:* Report only verifications that were actually performed. Never report `PASS` for an unexecuted check.

---

## 36. Policy Scope & Future Governance

This policy serves as the authoritative source of truth for all DBVault release operations. When the active task is creating or modifying release governance policy:

**DO NOT CREATE A RELEASE.**

Do not execute `git tag`, `git push --tags`, or `gh release create` unless explicitly requested by the maintainer in an independent, dedicated release directive.
