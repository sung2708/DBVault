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

FROM mongo:8.0 AS mongodb
# Disable APT download retries while upgrading the vulnerable APT client.
RUN apt-get -o Acquire::Retries=0 -o APT::Update::Error-Mode=any update \
    && apt-get -o Acquire::Retries=0 install -y --no-install-recommends --only-upgrade \
        apt libapt-pkg6.0t64 libpcre2-8-0 \
    && rm -rf /var/lib/apt/lists/* /opt/js-yaml \
    && rm -f /usr/local/bin/gosu /js-yaml.js /usr/bin/bsondump
COPY --from=build /dbvault /usr/local/bin/dbvault
USER mongodb
WORKDIR /data/db
ENTRYPOINT ["dbvault"]

FROM gcr.io/distroless/static:nonroot AS sqlite
COPY --from=build /dbvault /usr/local/bin/dbvault
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/dbvault"]

# Client major must match the backup server major under DBVault's policy.
FROM postgres:16-trixie AS postgres
RUN apt-get -o Acquire::Retries=0 -o APT::Update::Error-Mode=any update \
    && apt-get -o Acquire::Retries=0 install -y --no-install-recommends --only-upgrade apt zlib1g \
    && rm -rf /var/lib/apt/lists/* \
    && rm -f /usr/local/bin/gosu
COPY --from=build /dbvault /usr/local/bin/dbvault
USER postgres
WORKDIR /var/lib/postgresql
ENTRYPOINT ["dbvault"]
