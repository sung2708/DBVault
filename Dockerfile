FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -p 2 -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" -o /dbvault ./cmd/dbvault

FROM mysql:8.4 AS mysql-native-tools
# Preserve the minimal image's clients while adding its matching signed binlog tool.
RUN microdnf --disablerepo='*' --enablerepo=ol9_baseos_latest install -y cpio \
    && microdnf clean all \
    && mkdir /tmp/dbvault-client \
    && cd /tmp/dbvault-client \
    && curl --connect-timeout 15 --max-time 120 -fL \
       "https://repo.mysql.com/yum/mysql-8.4-community/el/9/$(uname -m)/mysql-community-client-${MYSQL_VERSION}.$(uname -m).rpm" \
       -o client.rpm \
    && rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-mysql \
    && rpm --checksig client.rpm \
    && rpm2cpio client.rpm | cpio -id ./usr/bin/mysqlbinlog \
    && install -m 0755 /tmp/dbvault-client/usr/bin/mysqlbinlog /usr/bin/mysqlbinlog \
    && rm -rf /tmp/dbvault-client \
    && mysqlbinlog --version

FROM mysql:8.4 AS mysql
# DBVault runs as mysql directly and uses the classic clients.
RUN microdnf --disablerepo='*' --enablerepo=ol9_baseos_latest upgrade -y \
        glibc glibc-common glibc-minimal-langpack \
    && microdnf clean all \
    && rpm -e mysql-shell \
    && rm -rf /usr/lib/mysqlsh \
    && rm -f /usr/local/bin/gosu
COPY --from=mysql-native-tools /usr/bin/mysqlbinlog /usr/bin/mysqlbinlog
COPY --from=build /dbvault /usr/local/bin/dbvault
USER mysql
WORKDIR /var/lib/mysql
ENTRYPOINT ["dbvault"]

FROM golang:1.26-alpine3.24 AS mongodb-tools
RUN apk add --no-cache build-base krb5-dev curl
WORKDIR /tools
# Preserve the archive format/tool version used by existing backups.
RUN curl --fail --location --retry 3 \
        https://codeload.github.com/mongodb/mongo-tools/tar.gz/7536fd7f3d67f5115de3eb2e271b327b7ce329b9 \
        -o source.tar.gz \
    && echo "cf456aea0f97ed79b52e3f920fba23516a1a458e2416f3a4e1022a5e35b73bfb  source.tar.gz" | sha256sum -c - \
    && tar -xzf source.tar.gz --strip-components=1 \
    && rm source.tar.gz
ENV GOFLAGS=-mod=mod
RUN set -eu; \
    go get golang.org/x/net@v0.62.0 golang.org/x/crypto@v0.58.1-0.20261009181718-2f1f834330b2 \
    && for tool in mongodump mongorestore; do \
        CGO_ENABLED=1 go build -p 2 -trimpath -buildmode=pie -tags=gssapi,failpoints \
            -ldflags "-s -w -X main.VersionStr=100.20.0 -X main.GitCommit=7536fd7f3d67f5115de3eb2e271b327b7ce329b9-dbvault" \
            -o /out/$tool ./$tool/main/$tool.go; \
       done

FROM alpine:3.24 AS mongodb
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates krb5-libs tzdata \
    && delgroup ping \
    && addgroup -g 999 -S mongodb \
    && adduser -u 999 -S -D -H -G mongodb -h /data/db mongodb \
    && mkdir -p /data/db \
    && chown mongodb:mongodb /data/db
COPY --from=mongodb-tools /out/mongodump /out/mongorestore /usr/bin/
COPY --from=mongodb-tools /tools/LICENSE.md /usr/share/licenses/mongodb-tools/LICENSE.md
COPY --from=build /dbvault /usr/local/bin/dbvault
ENV HOME=/data/db
USER mongodb
WORKDIR /data/db
ENTRYPOINT ["dbvault"]

FROM gcr.io/distroless/static:nonroot AS sqlite
COPY --from=build /dbvault /usr/local/bin/dbvault
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/dbvault"]

# Client major must match the backup server major under DBVault's policy.
FROM postgres:16-alpine3.24 AS postgres-tools

FROM alpine:3.24 AS postgres
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates krb5-libs libldap libedit libssl3 libcrypto3 \
        lz4-libs zstd-libs zlib tzdata \
    && delgroup ping \
    && addgroup -g 999 -S postgres \
    && adduser -u 999 -S -D -H -G postgres -h /var/lib/postgresql postgres \
    && mkdir -p /var/lib/postgresql /var/run/postgresql \
    && chown postgres:postgres /var/lib/postgresql /var/run/postgresql
COPY --from=postgres-tools /usr/local/bin/pg_dump /usr/local/bin/pg_restore /usr/local/bin/psql /usr/local/bin/pg_basebackup /usr/local/bin/
COPY --from=postgres-tools /usr/local/lib/libpq.so* /usr/local/lib/
COPY --from=build /dbvault /usr/local/bin/dbvault
ENV HOME=/var/lib/postgresql
USER postgres
WORKDIR /var/lib/postgresql
ENTRYPOINT ["dbvault"]
