# Developer Guide & Extension Manual

This guide explains how to set up the DBVault development environment, understand internal interfaces, and implement new database adapters, storage providers, and compression engines.

The implementation has four database adapters and four storage providers. The Go module already exists;
use `go mod tidy` only after changing dependencies. Exact adapter contracts are in
`internal/database/database.go`: capabilities, Preflight version information,
Dump, Restore with selectors, and compatibility checks. Interface sketches below
are design context. CLI code lives in `internal/cli` with the entry point in
`cmd/dbvault/main.go`.

Use `make build`, `test`, `test-race`, `vet`, `lint`, `fmt`, `fmt-check`, `coverage`,
`bench`, `integration-test`/`e2e-test`, `docker-up`, `docker-down`, or `clean`.
Make uses a POSIX shell; PowerShell users can run the corresponding Go commands
directly. `clean` runs Go cleanup and does not delete user backup directories.

The integration tag runs PostgreSQL/MySQL/MongoDB and cloud-emulator drills and fails explicitly
when Docker is unavailable. It creates uniquely named test containers and removes
only those containers. MongoDB/cloud emulator ports bind to localhost with
ephemeral host ports; PostgreSQL/MySQL use an isolated network without published
ports. `docker-compose.yml` is an
optional persistent development PostgreSQL service, independent of the drill.
`Dockerfile` has PostgreSQL 16, MySQL 8.4, MongoDB 8.0 and embedded SQLite targets.
Backup server/client versions must match their adapter compatibility policy.

On a Windows host with limited C: space, set GOTMPDIR, TEMP and TMP to a private
directory on another drive for test binaries/linker scratch. In PowerShell,
quote native arguments containing dots: `go test "-coverprofile=coverage.out" ./...`.

---

## 1. Development Prerequisites & Setup

- **Go 1.26+** (Go 1.26+ installed)
- **Git**
- **Docker** (for running database integration containers)

### Repository Setup
```bash
git clone https://github.com/sung2708/DBVault.git
cd DBVault

# Download the existing module dependencies
go mod download
go test ./...
go build ./...
make build
make test
```

`make build` writes `bin/dbvault` (`bin/dbvault.exe` on Windows). Custom builds
use `go build -o bin/dbvault ./cmd/dbvault`; `go install ./cmd/dbvault` installs
the same entrypoint into Go's binary directory. No release-specific implementation
exists. Public installation uses `github.com/sung2708/DBVault/cmd/dbvault@latest`,
because the module root is not a `main` package.

Extension points are `internal/database`, `internal/storage`, `internal/compression`,
`internal/pipeline`, `internal/cli`, `internal/presentation`, `internal/notify`,
`internal/schedule` and `internal/config`. Core events contain actual pipeline
measurements; terminal formatting belongs in presentation. Keep these APIs internal.
Installed binaries need no repository resources: help/version are self-contained;
operations load the user's `dbvault.yaml` in the current directory or `--config`.

### Running Locally
```bash
# Run CLI directly
go run ./cmd/dbvault --help

# Run with custom config
go run ./cmd/dbvault backup --config dbvault.yaml
```

---

## 2. Code Quality & Formatting

All code must pass strict Go formatting and vetting standards before submission:

```bash
# Format source files
gofmt -s -w .

# Static analysis
go vet ./...

# Run test suite with race detector
go test -race ./...
```

---

## 3. Extending DBVault

DBVault uses pluggable Go interfaces to decouple the orchestration engine from specific database engines, storage destinations, and compression codecs.

### 3.1 Adding a New Database Adapter

To add a new database engine (e.g. `redis`, `cockroachdb`), create a package under `internal/database/<engine>` that implements `database.Adapter`:

```go
package database

import (
	"context"
	"io"
)

type Adapter interface {
	Name() string
	Format() string
	Extension() string
	Capabilities() Capabilities
	Preflight(context.Context) (Info, error)

	// Dump streams the database dump to writer
	Dump(ctx context.Context, writer io.Writer) error

	// Restore reads backup data from reader and applies it
	Restore(ctx context.Context, reader io.Reader, options RestoreOptions) error
	Compatible(Info, string, string) error
}
```

#### Steps:
1. Implement `Adapter` in `internal/database/<engine>/adapter.go`.
2. Ensure child processes are executed via `os/exec.CommandContext` using argument slices (NEVER shell strings).
3. Register the adapter in the switch in `internal/cli/root.go`.
4. Add engine-specific integration tests in `test/integration/<engine>_test.go`.

---

### 3.2 Adding a New Storage Provider

To add a new storage destination (e.g. `s3`, `gcs`, `azure`), create a package under `internal/storage/<provider>` implementing `storage.Provider`:

```go
package storage

import (
	"context"
	"io"
	"time"
)

type ObjectMetadata struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

type Provider interface {
	Put(ctx context.Context, key string, reader io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]ObjectMetadata, error)
}
```

#### Steps:
1. Implement streaming upload and download in `internal/storage/<provider>/provider.go`.
2. Ensure streaming uses constant memory (e.g., S3 multipart streaming upload).
3. Register provider in `internal/storage/providers/factory.go`.

---

### 3.3 Adding a Compression Engine

To add a new compression algorithm (e.g. `zstd`, `snappy`), implement `compression.Compressor`:

```go
package compression

import "io"

type Compressor interface {
	Extension() string
	Compress(w io.Writer) (io.WriteCloser, error)
	Decompress(r io.Reader) (io.ReadCloser, error)
}
```

#### Steps:
1. Implement streaming wrappers in `internal/compression/<codec>/codec.go`.
2. Register in `internal/compression/compression.go`.
3. Add round-trip unit test verifying that data compressed and decompressed matches original SHA-256 hash.

---

### 3.4 Adding a CLI Command

DBVault uses [Cobra](https://github.com/spf13/cobra) for command handling.

To add a command:
1. Add a file in `internal/cli/`.
2. Define the `cobra.Command` struct.
3. Wire the command into `New` in `internal/cli/root.go`:
   ```go
   rootCmd.AddCommand(newVerifyCmd())
   ```
4. Update `docs/cli-reference.md`.

---

## 4. Release Governance

For versioning rules, tagging semantics, documentation audits, hygiene checks, and release readiness verification, refer to the [Release Governance Policy](releasing.md). Coding agents and contributors must never publish or tag releases as a side effect of routine development tasks.
