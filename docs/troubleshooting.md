# Troubleshooting Guide

Run `dbvault doctor` for a secret-safe readiness report covering configuration,
database authentication and native tools, storage access, and temporary files.
For cloud storage it checks a narrow list request only; it does not prove write or
delete permissions. Slack delivery is not tested. A missing local storage directory
is reported as a warning when its existing parent is writable; DBVault creates the
configured directory when an operation needs it.

This guide provides diagnostic procedures and remedies for common operational errors encountered when running DBVault.

---

## 1. Native Tool Dependency Errors

### Native tools not found or configured tool has moved
- **Symptom:** DBVault reports that a required native tool is unavailable.
- **Likely Cause:** The client tools are not installed, the installation is outside supported discovery locations, or a saved executable path no longer exists.
- **How to Diagnose:** Run `dbvault doctor`; inspect `database.tools` in the selected YAML. `dbvault init` can discover tools again.
- **How to Fix:** Install the matching client tools and re-run `dbvault init`, or use `--native-tool-dir` with the directory containing the complete engine toolset. Existing configurations without explicit paths continue to use `PATH` and supported known locations.

If a tool path was saved and the tool was moved or uninstalled, DBVault reports that
path and recommends `dbvault doctor` or running `dbvault init` again. DBVault never
updates the system `PATH` automatically.

---

## 2. Connection and Authentication Errors

### `connection refused`
- **Symptom:** `[ERROR] dial tcp 127.0.0.1:5432: connect: connection refused`.
- **Likely Cause:** Target database server is not running, listening on a different port, or blocked by a firewall.
- **How to Diagnose:** Run `nc -zv 127.0.0.1 5432` or `Test-NetConnection -Port 5432 127.0.0.1`.
- **How to Fix:** Verify database service status (`systemctl status postgresql` or Docker container status) and ensure port forwarding is properly configured.

### `password authentication failed`
- **Symptom:** `[ERROR] pq: password authentication failed for user "postgres"`.
- **Likely Cause:** The environment variable specified in `password_env` is empty, unset, or holds an incorrect password.
- **How to Diagnose:** Check if variable is set in current environment: `echo $DB_PASSWORD` or `$env:DB_PASSWORD`.
- **How to Fix:** Export the correct password: `export DB_PASSWORD="correct_secret"`.

---

## 3. Storage and Filesystem Errors

### `permission denied` (Local Storage)
- **Symptom:** `[ERROR] open /var/backups/dbvault/backup.dump.gz.tmp: permission denied`.
- **Likely Cause:** The user running DBVault does not have write permissions to the destination directory.
- **How to Diagnose:** Inspect directory permissions: `ls -ld /var/backups/dbvault`.
- **How to Fix:** Grant directory ownership to the backup user: `sudo chown -R $(whoami) /var/backups/dbvault` or adjust `storage.local.path`.

### `backup destination unavailable` / `no space left on device`
- **Symptom:** `[ERROR] write: no space left on device`.
- **Likely Cause:** Target storage volume has run out of disk space during backup streaming.
- **How to Diagnose:** Check free disk space: `df -h /var/backups/dbvault`.
- **How to Fix:** Expand the disk volume or configure DBVault retention cleanup (`dbvault cleanup`) to remove older archives.

---

## 4. Integrity and Verification Errors

### `checksum mismatch`
- **Symptom:**
  ```text
  [FATAL] Integrity check failed!
  Expected SHA-256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
  Computed SHA-256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
  ```
- **Likely Cause:** The backup archive was altered, truncated, corrupted during transfer, or tampered with.
- **How to Diagnose:** Compute the SHA-256 hash manually: `sha256sum <archive_file>` and compare with the `.meta.json` file.
- **How to Fix:** Do not restore corrupted files. Locate a prior verified backup or re-run the backup if possible.

---

## 5. Execution Lifecycle Errors

### `restore failed` / `missing confirmation flag`
- **Symptom:** `[ERROR] restore aborted: must specify --confirm to execute destructive restore`.
- **Likely Cause:** Restores require safety authorization.
- **How to Fix:** Append `--confirm` to the command.

### `context canceled` / `timeout exceeded`
- **Symptom:** `[ERROR] backup aborted: context deadline exceeded`.
- **Likely Cause:** The database dump took longer than the configured CLI timeout (default: 2 hours).
- **How to Diagnose:** Check the database size and network throughput.
- **How to Fix:** Increase the timeout flag: `dbvault backup --timeout 6h`.
