#!/usr/bin/env bash
set -euo pipefail

engine=${1:?engine required}
image=${2:?client image required}
server="dbvault-client-test-${engine}-$$"
cleanup() {
  local status=$?
  if [ "$status" != 0 ]; then docker logs "$server" >&2 || true; fi
  docker rm -fv "$server" >/dev/null 2>&1 || true
}
trap cleanup EXIT

client() {
  local tool=$1
  shift
  docker run --rm -i --network "container:$server" \
    -e PGPASSWORD=dbvault-test --entrypoint "$tool" "$image" "$@"
}

case "$engine" in
  postgres)
    docker run --detach --name "$server" --network none \
      -e POSTGRES_PASSWORD=dbvault-test postgres:16-bookworm >/dev/null
    for attempt in {1..60}; do
      if docker exec "$server" pg_isready -h 127.0.0.1 -U postgres; then break; fi
      sleep 1
    done
    client psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 -d postgres \
      -c "CREATE TABLE records (id integer PRIMARY KEY); INSERT INTO records VALUES (42);" \
      -c "CREATE DATABASE restored;" \
      >/dev/null
    ;;
  mongodb)
    docker run --detach --name "$server" --network none mongo:8.0 >/dev/null
    for attempt in {1..60}; do
      if docker exec "$server" mongosh --quiet --eval 'db.adminCommand({ping:1})'; then break; fi
      sleep 1
    done
    docker exec "$server" mongosh --quiet --eval \
      'db.getSiblingDB("source").records.insertOne({_id:42,value:"Việt Nam"})' >/dev/null
    docker exec "$server" mongodump --quiet --db=source --archive \
      | client mongorestore --quiet --archive --nsFrom='source.*' --nsTo='restored.*'
    client mongodump --quiet --db=restored --archive \
      | docker exec -i "$server" mongorestore --quiet --archive \
          --nsFrom='restored.*' --nsTo='roundtrip.*'
    test "$(docker exec "$server" mongosh --quiet --eval \
      'db.getSiblingDB("roundtrip").records.countDocuments({_id:42,value:"Việt Nam"})')" = 1
    ;;
  *)
    echo "Unsupported engine: $engine" >&2
    exit 1
    ;;
esac

if [ "$engine" = postgres ]; then
  docker exec "$server" pg_dump -U postgres -Fc -d postgres \
    | client pg_restore -h 127.0.0.1 -U postgres -e -d restored
  client psql -h 127.0.0.1 -U postgres -v ON_ERROR_STOP=1 -d postgres \
    -c "CREATE DATABASE roundtrip;" >/dev/null
  client pg_dump -h 127.0.0.1 -U postgres -Fc -d restored \
    | docker exec -i "$server" pg_restore -U postgres -e -d roundtrip
  test "$(client psql -h 127.0.0.1 -U postgres -At -d roundtrip \
    -c 'SELECT id FROM records')" = 42
  client pg_basebackup -h 127.0.0.1 -U postgres --pgdata=/tmp/base \
    --format=plain --wal-method=stream --checkpoint=fast
fi
echo "$engine: upstream and DBVault clients restored each other's archives"
