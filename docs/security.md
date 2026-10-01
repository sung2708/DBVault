# Security Architecture & Threat Model

This document outlines DBVault's security model, threat landscape, mitigation controls, and credential management policies.

## Implemented controls and limits

The binary uses direct argv process execution,
child-scoped PG/MYSQL credentials, centralized resolved-secret/credential-URL redaction, confined
flat storage keys, strict metadata, mandatory stored-byte SHA-256, private restore
snapshots, and `--confirm` guards. Nonempty `extra_flags`, connection-string
database names and unsupported strategies fail explicitly.

PostgreSQL children remove inherited PGSERVICE, PGSERVICEFILE and PGHOSTADDR so
libpq cannot redirect a configured host through an inherited service/address.
Configured connection fields and PGOPTIONS are set explicitly. Tests inject a
wrong PGHOSTADDR and verify that native operations still use the intended target.

Existing directories must be private; POSIX mode bits are not equivalent to
Windows ACL protection. Symlink escape tests require Windows symlink privileges
and are included in Linux CI. Native processes start in private Unix process
groups or Windows kill-on-close Job Objects. Windows starts children suspended
until job assignment, preventing a startup fork from escaping supervision.
Cancellation stops the owned process tree; trusted vendor tools must not
deliberately detach into other Unix sessions. A trusted operator controls executables and
storage. Hashes are integrity checks, not signatures or encryption; attackers
controlling both archive and manifest can replace a backup. Cloud credentials
use official SDK chains or named env variables. MongoDB credentials use a
temporary private YAML file, removed on return; process termination by force or
host failure can leave this file. Secure the operator's temp directory and Windows
ACLs. Slack requires HTTPS and redacts resolved secrets. Client-side encryption
remains unsupported. See ADRs 0007–0009 for implementation choices.

---

## 1. Threat Landscape & Mitigations

| Threat | Risk Level | Mitigation Strategy | Implementation Status |
|---|:---:|---|:---:|
| **Command Injection** | Critical | Execute native binaries via `exec.CommandContext` with structured argument vectors; zero shell invocation (`sh -c`). | Enforced by Architecture |
| **Credential Leakage in Logs** | High | Redact passwords and tokens from logging outputs, errors, and metadata sidecars. | Implemented |
| **Credential Exposure in Process List** | High | Pass passwords via environment variables or temporary restricted credential files; never via CLI flags. | Enforced by Architecture |
| **Path Traversal Attacks** | High | Sanitize and resolve all user-provided filepaths against strict directory boundaries (`filepath.Clean`). | Implemented |
| **Backup Tampering & Corruption** | High | Compute streaming SHA-256 digests on write; strictly verify digests prior to restore. | Implemented |
| **Overly Permissive File Permissions** | Medium | Enforce `0700` for created directories and `0600` for generated backup archives. | Implemented |
| **Accidental Database Overwrite** | High | Mandate the explicit `--confirm` flag for all destructive restore operations. | Implemented |
| **Cloud Credential Exposure** | High | Official SDK credential chains or named env variables, with redaction. | Implemented |
| **Data-at-Rest Exposure** | High | Client-side envelope encryption using AES-256-GCM. | Planned |

---

## 2. Process Security & Command Execution

### Zero Shell Invocation
Traditional backup scripts often construct command strings dynamically:
```bash
# VULNERABLE INSECURE PATTERN:
sh -c "pg_dump -d " + userSuppliedDatabaseName
```
If `userSuppliedDatabaseName` contains metacharacters (e.g., `prod; rm -rf /`), arbitrary commands are executed with the permissions of the backup runner.

### DBVault's Secure Execution Pattern
DBVault invokes native database tools strictly using Go's `os/exec.CommandContext`:
```go
// SECURE PATTERN: Structured argument vector
args := []string{
    "-h", host,
    "-p", strconv.Itoa(port),
    "-U", username,
    "-d", databaseName,
}
cmd := exec.CommandContext(ctx, "pg_dump", args...)
```
Operating system kernels treat elements of the argument vector as literal strings, completely bypassing shell interpretation and rendering command injection impossible.

---

## 3. Secret Management & Credential Isolation

DBVault strictly enforces the following credential rules:
1. **Never Accept Passwords in Flags:** CLI commands do not include flags such as `--password`. Command-line arguments are visible to any unprivileged user on the host system via `ps aux`, `/proc`, or Windows Task Manager.
2. **Environment Variable Redirection:** Configuration files point to environment variable names (`password_env: DB_PASSWORD`) rather than holding raw strings.
3. **No Secrets in Metadata Sidecars:** Manifest files (`.meta.json`) record database names, timestamps, and hashes, but never credentials, host passwords, or connection strings.
4. **Log Redaction:** All error messages and logs sanitize connection strings (e.g. replacing `postgres://user:pass@host` with `postgres://user:***@host`).

---

## 4. Cryptographic Integrity: SHA-256

Every backup stream computes a SHA-256 digest on the fly. During restore:
- The sidecar metadata file is consulted for the trusted hash.
- The target archive is hashed.
- If the computed hash differs by even a single bit, the operation aborts with exit code `4`.

---

## 5. File System Permissions

On POSIX systems:
- Backup directories created by DBVault default to `0700` (`drwx------`), restricting read, write, and traverse permissions to the owning user.
- Backup archive files are created with `0600` (`-rw-------`), preventing unauthorized local users from reading sensitive database contents.

---

## 6. Vulnerability Disclosure

If you discover a security vulnerability in DBVault, please refer to our responsible disclosure policy in [SECURITY.md](../SECURITY.md).
