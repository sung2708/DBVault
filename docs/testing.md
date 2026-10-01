# Testing Strategy & Quality Assurance

This document details the testing architecture, validation commands, integration testing harness, and disaster recovery drill procedures for DBVault.

## Implemented test harness

Recovery drill tests use real embedded SQLite databases with none/gzip/zstd,
compare restored fixture values, assert production data and immutable manifests
stay unchanged, and exercise new-file isolation, hardlinks/symlinks (host privilege
permitting), Windows path normalization, dry-run, owned cleanup and exact-backup
health association. Failure injection covers corrupted/missing archives, restore
and validation failure, cancellation, unavailable temporary space, record-write
failure and replacement-file cleanup refusal. Native engines must fail closed.
CLI coverage includes JSON purity, non-TTY, quiet/no-color, source-file loss and
required confirmation/input guards.

The real SQLite integration drill requires no Docker and can run independently:

```bash
go test -v -tags=integration ./test/integration -run TestSQLiteRecoveryDrill
```

It restores fixtures to an isolated target and checks exact values for all three
codecs, verifies production is untouched, rejects the production target and checks
owned cleanup. Native database/cloud integration tests remain separate and require
Docker; they are not evidence that the new native recovery-drill CLI is supported.

The Docker suite also checks health against real registered PostgreSQL/MySQL/
MongoDB backups and SQLite backups on local/S3/GCS/Azure storage for all codecs.
Injected clocks cover fresh default/active verification, exact age limit, stale,
missing policy and no matching backup. Cloud fixtures perform new SQLite recovery
drills and check saved exact-backup health evidence; native fixtures assert that
the new drill fails closed before mutation, alongside existing full/selected
restore dataset checks.

Setup tests inject a fake Prompter for all database/storage combinations, partial
flags, existing-file approval/cancel, transient password redaction and failed tests.
Config writer tests cover concurrent no-overwrite publication, private permissions,
cancel cleanup and rejection of nonregular destinations. CLI acceptance creates
a real SQLite database, generates YAML non-interactively, loads it with the runtime
loader, and reuses the ordinary connection test. Non-TTY/JSON/quiet paths must not
read stdin or leak secrets. These tests require no real keyboard or cloud credentials.

Update-check tests use a local HTTP server: SemVer ordering, dev/unknown versions,
stable-only release validation, malformed/rate-limited responses, timeout and
cancellation, plus cache hits, expiry, force refresh and corrupted cache recovery.
They never depend on live GitHub availability.

`doctor` tests cover warning-versus-failure readiness aggregation and CLI JSON
reports with a real SQLite database, safe local/temp-file probes and structured
failure output. It never requires cloud credentials or sends Slack notifications.

`go test ./...` covers configuration precedence/schema, capabilities, secure
command vectors, secret redaction, metadata/versioning/naming, compression,
SHA-256, local storage/traversal, retention, CLI guards and pipeline failures.
The application fake-adapter round trips test orchestration, not database
restore reliability. `go test -race ./...` and `go vet ./...` are required.

CLI help tests recursively inspect every public command through both `--help`
and `help <command>`, without credentials or an existing configuration. They
check navigation, examples, required inputs, destructive warnings, defaults,
plain redirected output, and actionable errors without stdout pollution.
See [CLI help audit](cli-help-audit.md) for the command inventory and acceptance.

Presentation tests inject terminal capabilities and a clock to validate color,
NO_COLOR, narrow layouts, control-character/secret redaction and measured progress
without sleeps. CLI tests use real SQLite backups to check JSON alias/schema,
separate stdout/stderr, quiet/redirect output, failure reporting and unchanged
confirmation/dry-run guards. Build metadata tests distinguish module installation,
source development and explicit release linker flags. CI installs the local CLI
and runs it outside the checkout, then builds all five distribution targets.

`go test -v -timeout=20m -tags=integration ./test/integration/...` creates an isolated
`postgres:16-alpine` container and executes native tools through docker exec.
It seeds 1,000 rows, records a deterministic hash of all IDs/names, backs up,
drops the table, restores, and compares the complete dataset hash for none/gzip/
zstd. It also mutates data and tests selected-table clean restore. Readiness
uses deadline-bound polling; missing Docker infrastructure causes explicit
failure rather than a silent skip. Host pg_dump installation is unnecessary
for this test; production CLI resolves native tools through configured paths,
PATH and supported platform locations.

The older manual container procedure below is optional; the automated drill
does not need that container. CI runs the drill on Linux. A Windows symlink test
is explicitly skipped if the host does not grant symlink creation privileges;
Linux CI runs it. The same integration command also runs a real `mysql:8.4`
container drill for none/gzip/zstd, exact dataset comparison and non-InnoDB
rejection. MongoDB 8.0 drills check 1,000 complete documents with all codecs,
destroy/restore and selected-collection remapping. SQLite unit tests exercise
committed WAL pages, live online restore, corruption and cancellation on a
locked target. Cloud drills use LocalStack 4.7.0, fake-gcs-server 1.56.1 and
Azurite 3.35.0 with official SDKs; they check multipart/chunked upload, absent-object
conditions, lifecycle, cancellation and SQLite restore for all codecs. These
emulators do not validate live IAM/KMS policies. TLS Slack tests use an in-process
test server and no actual webhook. Native cancellation tests spawn descendants
and check that they terminate. Scheduler tests cover state/CRUD/validation.
Benchmark
commands are in `make bench`; report only actual runs.

---

## 1. Testing Pyramid Overview

DBVault employs a multi-tiered testing strategy to guarantee that data streaming, compression, and restore procedures operate without corruption:

```text
       ▲
      / \     E2E / Disaster Recovery Drills (Seed -> Backup -> Corrupt -> Restore -> Assert)
     /   \
    /     \   Integration Tests (Dockerized PostgreSQL & MySQL test containers)
   /       \
  /         \ Unit Tests (Config parsing, stream hashing, flag sanitization, mock pipes)
 /___________\
```

---

## 2. Unit Testing

Unit tests focus on fast, deterministic validation of internal logic without requiring external database servers or network storage:

- **Configuration Parser:** Validates YAML structure, precedence rules, default fallbacks, and environment variable expansion.
- **Streaming Pipeline:** Tests `io.Pipe` mechanics, stream backpressure, byte counting, and hash calculation.
- **Flag Construction:** Verifies that database adapters construct clean, unquoted argument slices for `os/exec.CommandContext`.
- **Integrity Validation:** Verifies that bit flips in backup archives trigger hash mismatch errors.

### Running Unit Tests
```bash
go test ./...
```

---

## 3. Data Race Detection

Concurrent streaming operations (e.g. streaming through `io.Pipe` while calculating checksums and writing to storage) must remain race-free:

```bash
go test -race ./...
```

---

## 4. Test Coverage Reporting

Generate and inspect code coverage profiles:

```bash
# Run tests and generate coverage profile
go test -coverprofile=coverage.out ./...

# View coverage percentage per function
go tool cover -func=coverage.out

# View interactive HTML coverage report in browser
go tool cover -html=coverage.out -o coverage.html
```

---

## 5. Static Analysis & Linting

Verify code quality and adherence to idiomatic Go:

```bash
# Standard Go static analysis
go vet ./...

# Run golangci-lint (if installed)
golangci-lint run
```

---

## 6. Integration Testing with Docker

Integration tests spin up ephemeral database containers, execute real backups, drop tables, execute restores, and verify table record counts.

### Running PostgreSQL Integration Tests
```bash
# 1. Start test container
docker run -d --name dbvault-test-pg \
  -e POSTGRES_PASSWORD=testsecret \
  -e POSTGRES_DB=testdb \
  -p 5432:5432 \
  postgres:16-alpine

# 2. Run integration suite
go test -v -timeout=20m -tags=integration ./test/integration/...

# 3. Clean up container
docker rm -f dbvault-test-pg
```

---

## 7. Manual Restore Verification Drill

To manually verify the end-to-end backup and restore lifecycle against a live database:

```bash
# 1. Seed database with test table and records
psql -h 127.0.0.1 -U postgres -d testdb -c "CREATE TABLE items (id SERIAL PRIMARY KEY, name TEXT);"
psql -h 127.0.0.1 -U postgres -d testdb -c "INSERT INTO items (name) SELECT 'item_' || generate_series(1, 1000);"

# 2. Execute backup
dbvault backup --config test-config.yaml

# 3. Corrupt data (simulate disaster)
psql -h 127.0.0.1 -U postgres -d testdb -c "DROP TABLE items;"

# 4. Restore from backup
dbvault restore --config test-config.yaml --target ./backups/testdb_latest.sql.gz --confirm

# 5. Verify records restored
psql -h 127.0.0.1 -U postgres -d testdb -c "SELECT COUNT(*) FROM items;"
# Expected count: 1000
```
