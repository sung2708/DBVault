# Installation Guide

Version `v0.9.2` packages updated database clients and runtime libraries. Install this version explicitly:

```bash
go install github.com/sung2708/DBVault/cmd/dbvault@v0.9.2
```

Binary/container assets become available after the release workflow succeeds.
For a monitoring host, follow [independent monitoring](independent-monitoring.md):
database tools and producer encryption keys are unnecessary for default health
checks against shared storage. The example Python freshness probe requires
Python 3 and is shipped in the source tree under `scripts/monitor-backups.py`.

This document describes how to install DBVault, configure native database dependencies across supported operating systems, and verify your environment.

The current source requires Go 1.26.0 or later and specifies Go 1.26.11 as its
preferred toolchain. CI uses that toolchain for `v0.9.0`.
Published releases include binary archives. The tag-triggered workflow builds
Linux amd64/arm64, macOS amd64/arm64 and Windows amd64, injecting the real tag,
commit and UTC build time into `dbvault version`. It publishes archives only when
maintainers push a release tag. Native database tools are not included in those
archives. Docker runtime targets provide PostgreSQL client 16, MySQL 8.4 clients,
MongoDB Database Tools, or embedded SQLite. Choose the target for your engine.
The MySQL image ships `mysql`, `mysqldump` and `mysqlbinlog` for DBVault's
workflows. As of `v0.9.1`, it omits MySQL Shell (`mysqlsh`). The MongoDB image
uses `mongodump` and `mongorestore` for backup and restore.

## Install with Go

```bash
go install github.com/sung2708/DBVault/cmd/dbvault@latest
dbvault --help
dbvault version
dbvault update check
```

The `/cmd/dbvault` package is intentional: the module root is not an executable.
Pin a published version with `@vX.Y.Z`. `go install` places `dbvault` (Windows:
`dbvault.exe`) in `GOBIN`, or `GOPATH/bin` if unset; add that directory to `PATH`.
Unpublished checkout changes cannot be obtained via `@latest`. For local changes
use `go install ./cmd/dbvault` inside the checkout.

`dbvault update check` compares the installed version with GitHub's latest
published stable DBVault release and prints the exact versioned `go install`
command plus the official release page. Prebuilt binary users can use that page
to choose an asset for their platform. The checker reads release metadata only;
it never downloads or replaces a binary. A successful result is cached in the
user cache directory for 24 hours; pass `--force` to check again immediately.

## Prebuilt archives

Published assets are listed at [GitHub Releases](https://github.com/sung2708/DBVault/releases).
The prepared workflow produces `dbvault_vX.Y.Z_OS_ARCH.tar.gz` (Windows `.zip`),
each containing the executable, README.md and LICENSE. Download `checksums.txt`
alongside your archive; from that directory run
`sha256sum -c checksums.txt --ignore-missing`. PowerShell users can compare
`Get-FileHash ARCHIVE -Algorithm SHA256` with the matching manifest entry.
Prebuilt binaries do not require Go; native database tools still apply.

`version` uses explicit release linker metadata when supplied. Module installs
report Go's module version. Source builds report `dev` and available VCS revision,
including `-dirty` for modified checkouts. Without a linker build date, Built is
`unknown`; a commit timestamp is not presented as a build timestamp.

---

## 1. Overview & Architecture Dependency Note

> [!IMPORTANT]
> **Native Tool Dependency:**
> Standalone DBVault binaries use native logical dump/restore tools for PostgreSQL, MySQL and MongoDB. `dbvault init` discovers tools in `PATH` and supported installation locations and saves their absolute paths. SQLite uses the embedded driver and needs no client executable.

---

## 2. System Requirements

- **Supported Operating Systems:**
  - Linux (Ubuntu, Debian, RHEL, CentOS, Alpine)
  - macOS (Apple Silicon & Intel)
  - Windows (Windows 10, 11, Windows Server 2019/2022)
- **Go Version:** Go 1.26 or higher (to compile from source).

---

## 3. Installing Native Database Tools

### 3.1 PostgreSQL Tools (`pg_dump`, `pg_restore`, `psql`)

#### Linux (Debian / Ubuntu):
```bash
sudo apt-get update
sudo apt-get install -y postgresql-client
```

#### Linux (RHEL / Fedora / AlmaLinux):
```bash
sudo dnf install -y postgresql
```

#### macOS (Homebrew):
```bash
brew install libpq
# Add libpq to PATH if keg-only:
echo 'export PATH="/opt/homebrew/opt/libpq/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

#### Windows:
1. Download the official PostgreSQL installer from [postgresql.org](https://www.postgresql.org/download/windows/).
2. During installation, ensure the **Command Line Tools** component is selected.
3. Add the PostgreSQL `bin` directory to your System `Path` environment variable:
   ```text
   C:\Program Files\PostgreSQL\16\bin
   ```
4. Verify in PowerShell:
   ```powershell
   pg_dump --version
   psql --version
   ```

---

### 3.2 MySQL Tools (`mysqldump`, `mysql`)

#### Linux (Debian / Ubuntu):
```bash
sudo apt-get update
sudo apt-get install -y default-mysql-client
```

#### Linux (RHEL / Fedora):
```bash
sudo dnf install -y mysql
```

#### macOS (Homebrew):
```bash
brew install mysql-client
echo 'export PATH="/opt/homebrew/opt/mysql-client/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

#### Windows:
1. Download MySQL Community Server or MySQL Shell from [dev.mysql.com](https://dev.mysql.com/downloads/installer/).
2. Add the MySQL `bin` directory to System `Path`:
   ```text
   C:\Program Files\MySQL\MySQL Server 8.0\bin
   ```
3. Verify in PowerShell:
   ```powershell
   mysqldump --version
   mysql --version
   ```

---

### 3.3 MongoDB Tools (`mongodump`, `mongorestore`)

Install Database Tools 100.x; use the same tools version for dump and restore:
- **Ubuntu/Debian:** Install the official Database Tools package using
  [MongoDB's installation instructions](https://www.mongodb.com/docs/database-tools/installation/installation/).
- **macOS:** `brew install mongodb-database-tools`
- **Windows:** Download MongoDB Database Tools MSI from mongodb.com.

---

### 3.4 SQLite

The binary embeds a pure Go SQLite driver. No external `sqlite3` installation
is required. Both source and restore destination files must already exist.

### 3.5 Docker targets

`docker build --target postgres -t dbvault:postgres .` builds the default image.
Use `--target mysql`, `mongodb` or `sqlite` for other engines. The current
Dockerfile builds the PostgreSQL 16 and MongoDB client images on Alpine 3.24.
PostgreSQL clients come from the official PostgreSQL 16 Alpine image.
MongoDB uses a DBVault rebuild of Database Tools 100.20.0 with patched
x/net and x/crypto dependencies. MySQL uses Oracle MySQL 8.4 clients.
For an existing MongoDB backup, use a restore tool matching the exact tools
version recorded in its manifest. Every runtime uses a non-root user. Mount
a config, private backup directory and any SQLite database with permissions
for that user.
Set credentials by environment variable name/secret injection. The new release
workflow publishes Linux amd64/arm64 images to GHCR after binary release success.
The first container release is `v0.4.0`; images become available only after its
release workflow succeeds. For builds, `VERSION`, `COMMIT` and `BUILD_DATE` are optional
build arguments; their defaults identify development builds.

To package an already cross-compiled Linux binary, Buildx can override the build
stage with a named context:
`docker build --build-context build=bin/linux-amd64 --target postgres -t dbvault:postgres .`
The context directory must contain the executable named `dbvault` for the image's
architecture. This route was used to smoke-test all four final runtime targets.

#### Container security updates

The current Dockerfile retains UID/GID 999 for PostgreSQL and MongoDB and
includes the four PostgreSQL clients (`pg_dump`, `pg_restore`, `psql`,
`pg_basebackup`) and two MongoDB clients (`mongodump`, `mongorestore`).
PostgreSQL clients keep TLS, LDAP, GSSAPI, gzip, LZ4 and Zstandard support.
MongoDB clients are built with CGO and the upstream `gssapi,failpoints` tags.
Both runtimes include CA certificates and timezone data.

The MongoDB Tools rebuild starts from upstream source commit
`7536fd7f3d67f5115de3eb2e271b327b7ce329b9` (100.20.0), verified by SHA-256
in the Dockerfile. Its Go dependencies use x/net 0.62.0 and x/crypto
`v0.58.1-0.20261009181718-2f1f834330b2`. The reported source commit ends
with `-dbvault` to identify the rebuild. The archive tool version stays
100.20.0. CI tests archive restore in both directions with upstream tools.
The runtime includes the Kerberos libraries required by GSSAPI.

Docker Scout may report CVE-2026-107891 against musl 1.2.6-r3 by applying
the edge branch's r6 threshold. Alpine's security tracker confirms that
the 3.24 branch is fixed in 1.2.6-r3. CI checks this branch's minimum
version. See the [Alpine advisory](https://security.alpinelinux.org/vuln/CVE-2026-107891).
Scan each final image digest because advisory data changes over time.
These client runtime changes start with `v0.9.2`.

#### Versioned registry images (`v0.4.0` and later)

Tags include the DBVault version and bundled engine/client version:

| Engine | Image tag pattern |
|---|---|
| PostgreSQL 16 | `ghcr.io/sung2708/dbvault:vX.Y.Z-postgres16` |
| MySQL 8.4 | `ghcr.io/sung2708/dbvault:vX.Y.Z-mysql8.4` |
| MongoDB 8 | `ghcr.io/sung2708/dbvault:vX.Y.Z-mongodb8` |
| SQLite | `ghcr.io/sung2708/dbvault:vX.Y.Z-sqlite` |

Replace `vX.Y.Z` with an actual published tag. Pin the returned image digest for
repeatable deployments. Maintainers must verify GHCR package visibility/access
after the first push; workflow publication does not itself make a package public.

For a local build, this Bash example mounts a reviewed config and private backup
directory, and passes a password by environment-variable name:

```bash
docker build --target postgres -t dbvault:postgres .
# Set DB_PASSWORD securely in the current environment first.
docker run --rm --env DB_PASSWORD \
  --mount type=bind,src="$PWD/dbvault.yaml",dst=/config/dbvault.yaml,readonly \
  --mount type=bind,src="$PWD/backups",dst=/backups \
  dbvault:postgres backup --config /config/dbvault.yaml
```

Use `/backups` as the local storage path in the mounted config and ensure the
directory is writable by the image's non-root user. The database host must be
reachable from the container; `localhost` refers to the container itself.
For scheduling, use the [CronJob example](scheduling.md#4-kubernetes-cronjob).

#### PostgreSQL recovery drills

Release `v0.4.0` supports drills on the CLI host using a trusted Docker daemon and
a preloaded official `postgres:<source-major>-bookworm` image. No host database
tools or production credentials are used by the drill. Run it on the host;
the published backup images do not include Docker or a mounted Docker socket.

```bash
docker pull postgres:16-bookworm
dbvault recovery drill --target ACTUAL-BACKUP-NAME --recovery-database recovery_check --dry-run
dbvault recovery drill --target ACTUAL-BACKUP-NAME --recovery-database recovery_check --confirm --cleanup
```

The example is for a PostgreSQL 16 backup. Extensions must be present in the
official target image; an unavailable extension makes the drill fail rather than
claiming recoverability. Custom images/hooks and existing-server targets are unsupported.

---

## 4. Building DBVault from Source

Clone the repository and compile using Go:

```bash
# Clone the repository
git clone https://github.com/sung2708/DBVault.git
cd DBVault

# Build the executable
go mod download
go test ./...
go build -ldflags="-s -w" -o bin/dbvault ./cmd/dbvault
```

### Installing Binary into System PATH

#### Linux / macOS:
```bash
sudo install -m 0755 bin/dbvault /usr/local/bin/dbvault
```

#### Windows:
Place `dbvault.exe` into a directory that is in your system `Path` (such as `C:\Windows\System32` or a dedicated `C:\Tools\bin` directory).

---

## 5. Verification

Verify that DBVault is correctly installed and all dependencies are registered:

```bash
# Check version
dbvault version

# Run self-check on database tools
dbvault test --config dbvault.yaml
```
