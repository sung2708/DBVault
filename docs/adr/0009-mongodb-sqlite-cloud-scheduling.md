# ADR-0009: MongoDB, SQLite, cloud storage, notifications and scheduling

Status: Accepted — 2026-10-01

## Decisions

- The selected dependency versions require Go 1.26 or newer. Builds embed
  time-zone data so cron expressions work without host zoneinfo installation.

- MongoDB uses native Database Tools 100.x archives. Database-scoped dumps
  require `database.options.quiesced: true`: the operator must stop writes for
  the entire dump. This is an acknowledgement, not an automatic write lock.
  `--oplog` cannot make a database-scoped dump consistent and is not simulated.
  Restore requires the same server major and exact Database Tools version.
  Literal collection selection and source-to-target database remapping are
  supported. Credentials use an ephemeral private YAML file, removed on return.
- SQLite uses the pure Go modernc driver with SQLite's `VACUUM INTO` for a
  consistent snapshot including committed WAL pages, then streams the snapshot
  through the common pipeline. Restore checks the image and uses the online
  backup API against an existing destination database. Copying a live database
  file or replacing it with a filesystem rename is forbidden. Selective restore
  and `--clean` are rejected. SQLite therefore needs temporary disk space for a
  snapshot; it does not provide direct streaming from the live database.
- S3, GCS and Azure use official SDKs and the existing provider interface.
  Publication uses S3 `If-None-Match: *`, GCS `DoesNotExist` and Azure
  `If-None-Match: *`. Flat backup names are mapped into a confined object prefix.
  Completed metadata remains the registration point. Uploads are bounded by
  one worker: S3 128 MiB parts, GCS/Azure 8 MiB chunks. S3 unknown-size uploads
  are limited to 10,000 parts (about 1.22 TiB). SDK retries do not constitute
  resumability across process crashes. S3 aborts use an independent ten-second
  context after upload cancellation; cleanup failures join the original error.
  Operators configure lifecycle cleanup
  for abandoned upload sessions; no client-side encryption is claimed.
- AWS default credentials, Google ADC and Azure default identity are preferred;
  explicitly named credential environment variables are supported. Secrets are
  resolved at runtime and redacted from output. Custom endpoints are explicit
  configuration, principally for private services and test emulators.
- Slack sends bounded HTTPS completion/failure notifications for backup and
  restore. Delivery failure is logged separately and never changes the database
  operation's result. Redirects are refused by the production client; notification
  cancellation has an independent ten-second timeout. No real webhook is needed
  for tests.
- External schedulers remain preferred. The optional foreground `schedule`
  daemon uses robfig/cron's five-field parser and UTC by default, with `CRON_TZ`
  supported. Definitions contain IDs, expressions and absolute config paths,
  never credentials. CRUD uses an exclusive writer lock and atomic replacement.
  The daemon loads definitions at startup, skips overlapping jobs, does not
  replay missed runs, and cancels active work on shutdown. It is not a distributed
  scheduler: multiple processes require an external singleton supervisor.

## Consequences

Logical full dumps are not incremental or differential chains. Those strategies
remain explicitly unsupported until an engine-specific recovery chain, retention
and restore implementation is accepted and tested. This resolves capability
claims without generating mislabeled full backups.

Cloud emulator tests validate streaming, listing, conditional publication,
cancellation, deletion and actual SQLite backup/destroy/restore through each
provider. They do not prove cloud IAM, region policy, KMS or live-provider behavior.
Native MongoDB tests validate full restores and collection remapping against
MongoDB 8.0. Packaging and test evidence are recorded in implementation-status.

## Sources

- [MongoDB tool configuration](https://www.mongodb.com/docs/database-tools/mongodump/#config)
- [SQLite VACUUM INTO](https://www.sqlite.org/lang_vacuum.html#vacuuminto)
- [SQLite online backup API](https://www.sqlite.org/backup.html)
- [S3 conditional writes](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html)
- [GCS request preconditions](https://cloud.google.com/storage/docs/request-preconditions)
- [Azure conditional headers](https://learn.microsoft.com/rest/api/storageservices/specifying-conditional-headers-for-blob-service-operations)
- [robfig/cron](https://github.com/robfig/cron)
