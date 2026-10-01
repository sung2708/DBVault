# ADR-0008: Oracle MySQL full logical strategy

## Status

Accepted, 2026-10-01.

## Decision

Use mysqldump/mysql from Oracle MySQL 8.x. Require the dump/server release series
(e.g. 8.4) to match; restore only into the same series with a matching mysql
client. MariaDB, MySQL 5.x and cross-series restore are explicitly unsupported.
Use native MYSQL_PWD within the child environment, as proposed in ADR-0005;
passwords are never command arguments. This environment is still sensitive to
same-user/admin process inspection; protect the host and native executables.
Ignore default option files/login paths to prevent hidden configuration overrides.

Dump SQL with --single-transaction, --quick, routines/triggers/events, hex blobs,
no tablespaces, and GTID purging disabled. Preflight rejects non-InnoDB base
tables. Operators must prevent concurrent DDL during dumps; --single-transaction
does not protect against table/schema changes. Include/exclude table selectors
are native dump options. Stored SQL is compressed/hashed by the common pipeline.

Restore the complete SQL artifact using mysql --binary-mode --batch. Without
--force, SQL errors stop execution. SQL DDL is not transactional; failed restores
can leave partially restored data. The dump may contain DROP TABLE statements;
--confirm is mandatory. Selective restore and a separate --clean option are
unsupported, as are incremental/differential/PITR strategies. Use an isolated
database for restore drills.

Preflight engine validation is conservative and also applies to the restore
destination. Native TLS modes map from the documented ssl_mode values. Native
certificate configuration remains the operator's responsibility.

Reference: [MySQL mysqldump](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html).
