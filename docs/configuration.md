# Configuration Reference

This document provides a comprehensive reference for the DBVault configuration schema, environment variable overrides, and precedence rules.

## Implemented schema

Use `dbvault init` to generate a small configuration using these same types and
validation rules, or create YAML manually. `dbvault config` remains a validation
command; it does not create a file. The default path for both commands is
`dbvault.yaml` in the working directory; `--config` selects another path.

Setup uses explicit flags, then interactive answers (when available), then runtime
defaults. It does not copy runtime environment overrides into the generated YAML;
those overrides still apply when loading the file for operations. The default
password reference is `DBVAULT_DB_PASSWORD`. No plaintext secret is generated.
The default compression remains gzip level 6. Only full backup is offered.
For PostgreSQL, MySQL and MongoDB, `init` discovers a complete native toolset
from `PATH` or supported platform locations and stores its absolute paths under
`database.tools`. Existing YAML without that map stays valid and resolves tools
from `PATH`/known locations at runtime. `--native-tool-dir` selects one complete
installation explicitly; DBVault does not modify the system `PATH`.

Config creation writes a private `0600` temporary file, flushes/closes it, and
publishes a complete file with a no-overwrite hard link. Explicit `--force` or
interactive overwrite uses replacement by rename. Hard-link-capable filesystems
are required for creation. Parent directories must exist; symlink/directory
destinations are rejected. Existing Windows directory ACLs remain operator-managed.

The binary implements PostgreSQL, MySQL, MongoDB and SQLite; local, S3, GCS and
Azure storage; none/gzip/zstd, retention and optional Slack. YAML decoding rejects
unknown fields, multiple documents and invalid types. Missing passwords are
reported before operations, without their values. `config` validates the schema
without resolving passwords or contacting a database.

Nonempty `database.options.extra_flags` is rejected because arbitrary flags can
override format/security. Cloud settings are described in [storage](storage.md).
Permissions must be
`"0700"`; existing directories and Windows ACLs must be secured by the operator.

`DBVAULT_DB_NAME` is the canonical database-name override;
`DBVAULT_DATABASE_NAME` is a legacy alias with lower priority. Port defaults are
engine-specific and CLI values override the environment only when supplied.
No general `${ENV}` YAML interpolation is performed; secrets use `password_env`.

`compression.level` accepts 1–9 for gzip/zstd, and is ignored for `none`.
For retention, both policies protect backups by union, per engine/database;
the newest valid backup is always protected and age-boundary backups are kept.
See [ADR-0007](adr/0007-verified-local-foundation.md).

MongoDB adds `database.auth_database` (defaults to `admin`),
`database.options.quiesced` (must be true during backup), `include_collections`
(at most one) and `exclude_collections`. MongoDB TLS mode must be explicitly
`disable`, `require` or `verify-full`. SQLite uses `database.database` as a
filesystem path and ignores network/password settings. Table filters only apply
to PostgreSQL/MySQL; collection filters only apply to MongoDB.

AWS SDK credential-chain variables, Google ADC and Azure default identity are
resolved by their SDKs; cloud YAML properties are not overridden by arbitrary
`AWS_S3_BUCKET` or `AWS_ENDPOINT_URL` environment variables. Custom S3 credential
env names must be supplied as a pair. Optional S3 `encryption` accepts `AES256`
or `aws:kms`, with `kms_key_id` for the latter. Azure accepts `prefix` and optional
`endpoint`; GCS accepts optional `endpoint`. Buckets/containers must exist.

Slack uses HTTPS and `notifications.slack.webhook_url_env`. It runs only after
backup/restore (success or failure), never on dry runs. Delivery errors do not
change the database operation's exit status. Modern Slack webhooks may ignore
the optional channel override; routing is controlled by the installed webhook.

---

## 1. Configuration Precedence

### Backup freshness policy

```yaml
health:
  max_backup_age: 12h
```

Optional `health.max_backup_age` is a positive Go duration, such as `30m`, `12h`,
`168h` or `1h30m`; `d` suffixes, zero, negative and overflowing durations are
rejected. Omitted/empty means no freshness expectation: health reports unknown
unless concrete missing/corrupt backup evidence already makes it critical.
The policy applies to the single configured database and has no environment or
CLI override. Cron and retention settings do not supply an implicit policy.
See [health](cli-reference.md#dbvault-health) for exact boundaries and exit codes.

### Stored-artifact verification policy

```yaml
protection:
  verify_after_backup: true
```

When enabled, every completed backup is read back from configured storage and
stream-verified against the manifest's stored-byte count and SHA-256 before the
command reports success. This does not restore the database. It appends an
immutable verification record; the backup manifest remains unchanged. If reading
or verification fails, DBVault returns failure while preserving the registered
artifact and reports the verification outcome. The default is disabled. For S3,
GCS and Azure, this reads the entire object after upload, adding time, read/API
charges and potentially egress; enable it only when that tradeoff is intended.

DBVault resolves configuration values using the following strict hierarchy (highest priority first):

```text
1. CLI Flags (e.g., --database, --output-dir)
       ↓
2. Environment Variables (e.g., DBVAULT_DATABASE_NAME, DB_PASSWORD)
       ↓
3. Configuration File (YAML format, passed via --config)
       ↓
4. Hardcoded Defaults
```

---

## 2. Configuration File Schema

A complete `dbvault.yaml` file is structured into top-level sections: `version`, `database`, `storage`, `compression`, `retention`, optional `health`, optional `protection`, and `notifications`.

```yaml
version: "1"

database:
  type: postgres              # string, required: postgres | mysql | mongodb | sqlite
  host: 127.0.0.1             # string, optional, default: 127.0.0.1
  port: 5432                  # integer, optional, default: 5432 (or 3306 for mysql)
  user: postgres              # string, required
  password_env: DB_PASSWORD   # string, required: name of env var containing password
  database: production_db     # string, required: name of database to back up
  ssl_mode: prefer            # string, optional, default: prefer (postgres)
  options:
    exclude_tables:           # list of strings, optional
      - audit_log_temp
      - sessions
    include_tables: []        # list of strings, optional (empty means all)
    extra_flags: []           # must be empty; arbitrary native flags are rejected

storage:
  type: local                 # string, required: local | s3 | gcs | azure
  local:
    path: /var/backups/dbvault # string, required if type=local
    permissions: "0700"        # string, optional, default: "0700"

compression:
  type: gzip                  # string, optional, default: gzip (none | gzip | zstd)
  level: 6                    # integer, optional, default: 6 (1 = fastest, 9 = best)

retention:
  keep_days: 30               # integer, optional, default: 0 (disabled)
  keep_count: 14              # integer, optional, default: 0 (disabled)

notifications:
  slack:
    enabled: false            # boolean, optional, default: false
    webhook_url_env: SLACK_WEBHOOK_URL # string, optional
    channel: "#db-alerts"     # string, optional
```

---

## 3. Property Reference Table

### 3.1 Root Section

| Property | Type | Required | Default | Description |
|---|---|---|---|---|
| `version` | string | Yes | `"1"` | Configuration schema version specification. |

---

### 3.2 `database` Section

| Property | Type | Required | Default | Environment Override | Description |
|---|---|---|---|---|---|
| `database.type` | string | Yes | None | `DBVAULT_DB_TYPE` | Database engine type: `postgres`, `mysql`, `mongodb`, `sqlite`. |
| `database.host` | string | No | `127.0.0.1` | `DBVAULT_DB_HOST` | Hostname or IP address of the target database server. |
| `database.port` | integer | No | `5432` / `3306` | `DBVAULT_DB_PORT` | TCP port for database connection. |
| `database.user` | string | Yes | None | `DBVAULT_DB_USER` | Database username for authentication and dumping. |
| `database.password_env` | string | Yes | None | — | Environment variable name holding the database password. |
| `database.database` | string | Yes | None | `DBVAULT_DB_NAME` | Name of the database to back up or restore. |
| `database.ssl_mode` | string | No | `"prefer"` | `DBVAULT_DB_SSLMODE` | SSL/TLS connection mode: `disable`, `require`, `verify-ca`, `verify-full`. |
| `database.options.exclude_tables` | `[]string` | No | `[]` | — | Specific tables to omit from the dump. |
| `database.options.include_tables` | `[]string` | No | `[]` | — | Whitelist of tables to dump (all tables if empty). |
| `database.options.extra_flags` | `[]string` | No | `[]` | — | Must be empty; arbitrary flags are rejected. |

---

### 3.3 `storage` Section

| Property | Type | Required | Default | Environment Override | Description |
|---|---|---|---|---|---|
| `storage.type` | string | Yes | `local` | `DBVAULT_STORAGE_TYPE` | Destination backend: `local`, `s3`, `gcs`, `azure`. |
| `storage.local.path` | string | Conditional | None | `DBVAULT_STORAGE_LOCAL_PATH` | Filesystem directory path for storing backups (required if `type=local`). |
| `storage.local.permissions` | string | No | `"0700"` | — | POSIX directory permission mask. |
| `storage.s3.bucket` | string | Conditional | None | — | Destination S3 bucket name. |
| `storage.s3.region` | string | Conditional | None | — | AWS region. |
| `storage.s3.prefix` | string | No | `""` | — | Normalized object prefix. |
| `storage.s3.endpoint` | string | No | `""` | — | Explicit S3-compatible endpoint. |

---

### 3.4 `compression` Section

| Property | Type | Required | Default | Description |
|---|---|---|---|---|
| `compression.type` | string | No | `"gzip"` | Compression algorithm: `none`, `gzip`, `zstd`. |
| `compression.level` | integer | No | `6` | Compression level: 1 (fastest) to 9 (maximum compression). |

---

### 3.5 `retention` Section

| Property | Type | Required | Default | Description |
|---|---|---|---|---|
| `retention.keep_days` | integer | No | `0` | Delete backups older than this number of days (0 = disabled). |
| `retention.keep_count` | integer | No | `0` | Retain only the most recent N backups (0 = disabled). |

---

### 3.6 `notifications` Section

| Property | Type | Required | Default | Description |
|---|---|---|---|---|
| `notifications.slack.enabled` | boolean | No | `false` | Enable or disable Slack webhook notifications. |
| `notifications.slack.webhook_url_env` | string | Conditional | None | Environment variable holding Slack webhook URL. |
| `notifications.slack.channel` | string | No | `""` | Override channel name. |

---

## 4. Secure Configuration Patterns

### Safe Secret Loading via Environment Variables

Never hardcode secrets inside YAML:

```yaml
# Good: Points to environment variable
database:
  user: db_backup_user
  password_env: PRODUCTION_DB_PASSWORD
```

Run with secrets passed via process environment:

```bash
export PRODUCTION_DB_PASSWORD="p@ssw0rd_from_vault"
dbvault backup --config dbvault.yaml
```
