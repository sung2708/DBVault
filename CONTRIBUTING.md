# Contributing to DBVault

Thank you for your interest in contributing to DBVault! We welcome pull requests, bug reports, and feature proposals.

---

## 1. Code of Conduct

Please maintain a respectful, constructive, and inclusive tone in all interactions within issues, discussions, and pull requests.

---

## 2. Development Setup

### Prerequisites
- **Go 1.26+**
- **Git**
- **Docker** (optional, for local integration testing against PostgreSQL and MySQL containers)
- **Native client tools** (`pg_dump`, `mysqldump`) if testing against local database instances.

### Getting Started
```bash
# Fork and clone the repository
git clone https://github.com/sung2708/DBVault.git
cd DBVault

# Verify local Go toolchain
go version
go mod download
go test ./...
go build -o bin/dbvault ./cmd/dbvault
go run ./cmd/dbvault --help
```

On Windows use `bin/dbvault.exe` as the build output. With GNU Make and a POSIX
shell, `make build`, `make test` and `make lint` are equivalent entry points;
`make build` chooses the Windows executable suffix automatically.
See [development](docs/development.md) for extension points and Docker drills.

---

## 3. Development Workflow & Standards

### Branching Strategy
- `main`: Protected production branch. All code must arrive via pull request.
- Feature branches: Use descriptive prefixes, e.g.:
  - `feat/postgres-custom-format`
  - `fix/s3-multipart-boundary`
  - `docs/troubleshooting-update`

### Code Formatting & Quality
Before committing, ensure your code complies with Go standards:
```bash
# Format code
gofmt -s -w .

# Run static analysis
go vet ./...

# Run test suite with race condition detection
go test -race ./...
```

### Commit Message Conventions
We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
- `feat: add MySQL adapter streaming dump`
- `fix: prevent race condition in pipeline TeeReader`
- `docs: update configuration reference for S3 storage`
- `test: add integration drill for postgres restore`
- `refactor: extract storage interface to internal/storage`

---

## 4. Pull Request Process

1. Open an issue first to discuss substantial architectural changes or new database adapters.
2. Ensure new features include unit and/or integration tests.
3. Update relevant documentation under `docs/` whenever CLI flags, configuration schemas, or behaviors change.
4. Ensure PR branches are rebased against the latest `main`.
5. Keep pull requests focused on a single logical change.

---

## 5. Adding External Dependencies

- Favor the Go standard library wherever possible.
- Avoid heavy or unmaintained third-party packages.
- Always run `go mod tidy` and verify `go.sum` changes.

---

## 6. Definition of Done (DoD)

A contribution is considered complete and eligible for merge only when:
1. **Tests pass:** All unit tests and race detection passes (`go test -race ./...`).
2. **Static analysis passes:** `go vet ./...` reports zero warnings.
3. **Documentation is updated:** CLI references, configuration specs, and README reflect the actual implementation.
4. **Security vetted:** No command injection vulnerabilities (all subprocesses use `os/exec` argument slices) and no credential exposure in logs or process listings.

---

## 7. Release Governance

DBVault follows a strict **Release Governance & Release Hygiene Policy**:
- Releases are intentional decisions by maintainers, never automated by routine PR merges or coding agents.
- See [docs/releasing.md](docs/releasing.md) for SemVer rules, documentation gates, release readiness checklists, and hygiene standards.
