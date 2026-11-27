# DBVault

DBVault is a command-line tool for backing up and restoring PostgreSQL, MySQL, MongoDB, and SQLite databases. It supports local and cloud storage, compression, SHA-256 integrity checks, and retention policies.

## Image variants

Choose the tag for your database engine. Images support Linux AMD64 and ARM64.

| Tag | Included database tools | v0.9.2 download (AMD64 / ARM64) |
| --- | --- | ---: |
| `postgres` | PostgreSQL 16 client tools | 28.0 / 26.5 MB |
| `mysql` | MySQL 8.4 client tools | 269.7 / 261.8 MB |
| `mongodb` | MongoDB Database Tools `mongodump` and `mongorestore` | 37.1 / 34.6 MB |
| `sqlite` | SQLite; no external database client required | 21.5 / 19.6 MB |

## Pull an image

```bash
docker pull sungp2708/dbvault:postgres
```

Replace `postgres` with `mysql`, `mongodb`, or `sqlite` to pull another variant.
The published `postgres` tag bundles PostgreSQL 16 clients. The next tagged
build will add `postgres16`, `postgres17` and `postgres18`; `postgres` will
continue to point to 16. Match the client major to the server major.

Pin a published version for repeatable deployments, for example
`v0.9.2-postgres`. The numbered PostgreSQL aliases and their versioned
counterparts start with the first release after `v0.9.2`.

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
