# Getting Started with DBVault

This tutorial guides you through setting up DBVault, configuring your first database connection, creating a verified backup, and executing a safe restore test.

The current binary supports PostgreSQL, MySQL, MongoDB and SQLite with
local/S3/GCS/Azure storage. This tutorial uses PostgreSQL and local storage. Output below is
illustrative; actual command results are metadata records and stderr logs.
Backups have unique names such as `demo_db_20261001_020000_<random-id>.dump.gz`.
Use the actual `backup_name` printed by backup or `dbvault list`. PostgreSQL
restore uses pg_restore custom archives, not plaintext SQL/psql. Before restore,
DBVault verifies a private snapshot of stored bytes; ensure space for one
compressed artifact in the OS temporary directory. See ADR-0007.

---

## 1. Prerequisites

Before using DBVault, ensure the following requirements are met on your host machine:

1. **Go 1.26+** (if compiling from source):
   ```bash
   go version
   ```
2. **Native Database Client Binaries** for PostgreSQL, MySQL or MongoDB. `dbvault init`
   discovers them in `PATH` and supported known installation locations and saves
   their paths. You can also provide a complete installation with `--native-tool-dir`:
   - For PostgreSQL: `pg_dump`, `pg_restore`, `psql`
   - For MySQL: `mysqldump`, `mysql`

To inspect tools manually (optional):
```bash
# Check PostgreSQL client tools
pg_dump --version
psql --version

# Check MySQL client tools
mysqldump --version
mysql --version
```

---

## 2. Installation

Clone and compile DBVault locally:

```bash
# Clone the repository
git clone https://github.com/sung2708/DBVault.git
cd DBVault

# Build the executable
go build -o bin/dbvault ./cmd/dbvault

# Add to your PATH or copy to a system binary location
# Linux/macOS:
sudo cp bin/dbvault /usr/local/bin/

# Windows (PowerShell as Administrator):
# Copy-Item bin\dbvault.exe C:\Windows\System32\
```

Verify the binary runs:
```bash
dbvault --help
```

---

## 3. Configuration Setup

DBVault relies on declarative YAML configuration files combined with environment variables for authentication secrets.

### Guided

```bash
dbvault init
```

Choose the database, enter its connection details, choose storage/compression,
review the summary and confirm creation. Use arrows and Enter; Esc or Ctrl+C
cancels without writing a partial configuration. `TERM=dumb` uses numbered
choices and plain input instead. SQLite asks only for the database file.

The default path is `dbvault.yaml` in your current directory. Supply `--config`
to choose another file; its parent directory must exist. Relative database and
backup paths keep the existing runtime meaning: relative to the working directory.
The wizard does not create a database or cloud bucket/container.

Passwords are referenced by environment variable (default `DBVAULT_DB_PASSWORD`).
An optional connection test can ask for a masked temporary password when the
variable is unset; that password is not saved or exported. Set the variable
before using the generated config later. Cloud credentials use native SDK chains.

For MongoDB, stop application writes throughout backup and explicitly acknowledge
`quiesced`; setup does not stop writes. Native tools are discovered during setup;
`--test` additionally checks database connectivity and server/tool compatibility.
If testing fails, interactive setup can still save the config with explicit approval.

Flags skip corresponding questions. Complete required flags skip the wizard:

```bash
dbvault init --non-interactive --database postgres --database-name demo_db --user postgres --storage local --password-env DB_PASSWORD
```

Non-TTY input, JSON, `--quiet` and `--non-interactive` never prompt. Missing
required values fail with instructions. Existing files require interactive
overwrite approval or `--force`; no automatic merge is performed.

After creation, run `dbvault doctor` to check the authenticated database
connection, required client tools and storage access. Cloud checks verify list/read
access only; doctor never writes or deletes cloud objects and never sends a Slack
message. Use `dbvault doctor --json` for automation.

Use `dbvault status` for a fast overview of recent backup metadata, freshness,
recovery evidence, storage and saved schedules. It does not verify archive bytes
or run a recovery drill. Use `dbvault health --verify` for an active checksum
check, and `dbvault recovery drill` when you want to test an isolated SQLite
recovery.

### Manual

Create a working directory and an initial configuration file:

```bash
mkdir -p ~/dbvault-demo && cd ~/dbvault-demo
```

Save the following file as `dbvault.yaml`:

```yaml
version: "1"

database:
  type: postgres
  host: 127.0.0.1
  port: 5432
  user: postgres
  password_env: DB_PASSWORD
  database: demo_db
  ssl_mode: prefer

storage:
  type: local
  local:
    path: ./backups
    permissions: "0700"

compression:
  type: gzip
  level: 6
```

---

## 4. Setting Authentication Secrets

`dbvault init` displays the right command for the selected operating system. Set
the password variable named by `password_env` in the same terminal where DBVault
will run:

```bash
# On Linux/macOS (hidden input, current shell):
read -s DB_PASSWORD; export DB_PASSWORD; echo

# On Windows PowerShell (masked input, current session):
$secure = Read-Host 'Database password' -AsSecureString
$env:DB_PASSWORD = [System.Net.NetworkCredential]::new('', $secure).Password
Remove-Variable secure
```

> [!SECURITY]
> Never write plain passwords directly into the `dbvault.yaml` file. Always use environment variable expansion via `password_env`.

---

## 5. Testing the Connection

Before scheduling or executing backups, run the connection test command. This verifies that:
- The required native tools (`pg_dump`, `psql`) exist in `PATH`.
- Network connectivity to the database host and port succeeds.
- Database credentials authenticate properly.

```bash
dbvault test --config dbvault.yaml
```

**Expected Output:**
```text
[INFO] Validating environment for postgres...
[INFO] Found pg_dump at /usr/bin/pg_dump (PostgreSQL 16.1)
[INFO] Found psql at /usr/bin/psql (PostgreSQL 16.1)
[INFO] Attempting connection to 127.0.0.1:5432 (database: demo_db, user: postgres)...
[INFO] Connection established successfully. Target version: PostgreSQL 16.1.
[SUCCESS] All pre-flight checks passed.
```

---

## 6. Creating Your First Backup

Run the `backup` command:

```bash
dbvault backup --config dbvault.yaml
```

**Execution Output:**
```text
[INFO] Starting backup for database 'demo_db' (engine: postgres)
[INFO] Storage target: local (./backups)
[INFO] Compression: gzip (level 6)
[INFO] Streaming pg_dump stdout through compression and SHA-256 calculation...
[INFO] Backup written: demo_db_20261001_020000.dump.gz (14.2 MB)
[INFO] Checksum (SHA-256): 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
[INFO] Manifest written: demo_db_20261001_020000.dump.gz.meta.json
[SUCCESS] Backup completed in 4.3 seconds.
```

### Inspecting Created Files

Check the backup directory:
```bash
ls -la ./backups
```

You will see two files:
1. `demo_db_20261001_020000.dump.gz` — The compressed backup archive.
2. `demo_db_20261001_020000.dump.gz.meta.json` — The sidecar metadata manifest with file size, database version, and SHA-256 checksum.

---

## 7. Listing and Verifying Backups

List existing backups stored in the configured backend:

```bash
dbvault list --config dbvault.yaml
```

**Output:**
```text
NAME                                 SIZE      CREATED AT             CHECKSUM
demo_db_20261001_020000.dump.gz       14.2 MB   2026-10-01 02:00:00    9f86d081884c...
```

---

## 8. Restoring a Backup

Restoring data is a destructive operation. DBVault requires explicit confirmation via the `--confirm` flag to prevent accidental overwrites.

```bash
dbvault restore \
  --config dbvault.yaml \
  --target ./backups/demo_db_20261001_020000.dump.gz \
  --confirm
```

**Execution Flow:**
1. DBVault reads the sidecar manifest (`.meta.json`).
2. Computes the SHA-256 checksum of the target archive and matches it against the manifest.
3. Streams the decompressed SQL statements directly to `psql` (or `mysql`).
4. Reports restore success.

---

## 9. Common First-Run Errors and Fixes

### Error 1: PostgreSQL tools unavailable
- **Symptom:** DBVault cannot resolve a required PostgreSQL executable.
- **Cause:** Client utilities are missing or installed in a location DBVault does not discover automatically.
- **Fix:** Install PostgreSQL client tools and re-run `dbvault init`, or pass `--native-tool-dir` with the installation's `bin` directory.
  - Ubuntu/Debian: `sudo apt-get install postgresql-client`
  - macOS (Homebrew): `brew install libpq && brew link --force libpq`
  - Windows: Add `C:\Program Files\PostgreSQL\<version>\bin` to your System Environment `Path`.

### Error 2: `password authentication failed for user "postgres"`
- **Symptom:** `[ERROR] ping failed: pq: password authentication failed for user "postgres"`.
- **Cause:** The environment variable specified in `password_env` is unset or contains an incorrect password.
- **Fix:** Re-export the variable: `export DB_PASSWORD="correct_password"`.

### Error 3: `connection refused`
- **Symptom:** `[ERROR] dial tcp 127.0.0.1:5432: connect: connection refused`.
- **Cause:** Database server is stopped or firewall is blocking the port.
- **Fix:** Verify database service status: `sudo systemctl status postgresql` or verify Docker container port mappings.
