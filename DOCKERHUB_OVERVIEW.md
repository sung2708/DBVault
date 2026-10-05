# DBVault

DBVault is a command-line tool for backing up and restoring PostgreSQL, MySQL, MongoDB, and SQLite databases. It supports local and cloud storage, compression, SHA-256 integrity checks, and retention policies.

## Image variants

Choose the tag for your database engine:

| Tag | Included database tools |
| --- | --- |
| `postgres` | PostgreSQL 16 |
| `mysql` | MySQL 8.4 |
| `mongodb` | MongoDB 8.0 and Database Tools |
| `sqlite` | SQLite; no external database client required |

## Pull an image

```bash
docker pull sungp2708/dbvault:postgres
```

Replace `postgres` with `mysql`, `mongodb`, or `sqlite` to pull another variant.
These variant tags track the latest successful release. Version-specific tags
are also published in the format `vX.Y.Z-postgres`, `vX.Y.Z-mysql`,
`vX.Y.Z-mongodb`, and `vX.Y.Z-sqlite`.

## Check the CLI

```bash
docker run --rm sungp2708/dbvault:postgres --help
```

## Run a backup

Mount your DBVault configuration file and backup directory into the container:

```bash
docker run --rm \
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


