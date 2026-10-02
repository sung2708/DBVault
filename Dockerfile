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

FROM mysql:8.4 AS mysql
COPY --from=build /dbvault /usr/local/bin/dbvault
USER mysql
WORKDIR /var/lib/mysql
ENTRYPOINT ["dbvault"]

FROM mongo:8.0 AS mongodb
COPY --from=build /dbvault /usr/local/bin/dbvault
USER mongodb
WORKDIR /data/db
ENTRYPOINT ["dbvault"]

FROM gcr.io/distroless/static:nonroot AS sqlite
COPY --from=build /dbvault /usr/local/bin/dbvault
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/dbvault"]

# Client major must match the backup server major under DBVault's policy.
FROM postgres:16-bookworm AS postgres
COPY --from=build /dbvault /usr/local/bin/dbvault
USER postgres
WORKDIR /var/lib/postgresql
ENTRYPOINT ["dbvault"]
