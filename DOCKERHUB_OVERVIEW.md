# DBVault

DBVault is a command-line tool for backing up and restoring PostgreSQL, MySQL, MongoDB, and SQLite databases. It supports local and cloud storage, compression, SHA-256 integrity checks, and retention policies.

## Image variants

Choose the tag for your database engine. Images support Linux AMD64 and ARM64.

| Tag | Included database tools |
| --- | --- |
| `postgres16` (also `postgres`) | PostgreSQL 16 client tools |
| `postgres17` | PostgreSQL 17 client tools |
| `postgres18` | PostgreSQL 18 client tools |
| `mysql` | MySQL 8.4 client tools |
| `mongodb` | MongoDB Database Tools 100.20.1 |
| `sqlite` | Embedded SQLite |

## Pull an image

```bash
docker pull sungp2708/dbvault:postgres
```

Replace `postgres` with `mysql`, `mongodb`, or `sqlite` to pull another variant.
Starting with `v0.10.0`, use the numbered PostgreSQL alias matching the
server major. The `postgres` alias continues to point to 16.

Pin a version for repeatable deployments, for example `v0.10.0-postgres16`
or `v0.10.0-mongodb`. The versioned `-postgres` alias remains available for 16.

## Check the CLI

```bash
docker run --rm sungp2708/dbvault:postgres --help
```

## Run a backup

Mount your DBVault configuration file and a backup directory writable by the
container user. Set `DB_PASSWORD` in your shell if your config references it:

```bash
docker run --rm \
  --env DB_PASSWORD \
  -v "$PWD/dbvault.yaml:/config/dbvault.yaml:ro" \
  -v "$PWD/backups:/backups" \
  sungp2708/dbvault:postgres \
  backup --config /config/dbvault.yaml
```

Configure database credentials and storage paths in `dbvault.yaml`. Make sure any paths in the configuration point to locations available inside the container, such as `/backups`. Provide credentials through the environment variables configured in your file, and pass those variables to the container with `-e` or an environment file when needed.

The image runs DBVault; it does not start a database server. Your database must already be reachable from the container.

## Documentation

- [README and setup guide](https://github.com/sung2708/DBVault#readme)
- [Configuration and other documentation](https://github.com/sung2708/DBVault/tree/main/docs)
- [Apache License 2.0](https://github.com/sung2708/DBVault/blob/main/LICENSE)
