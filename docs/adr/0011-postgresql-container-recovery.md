# ADR-0011: PostgreSQL recovery in an owned network-isolated Docker server

Status: Accepted — 2026-10-02 (`v0.4.0`)

## Decision

Extend ADR-0010 with a PostgreSQL-specific isolation mechanism. Arbitrary recovery
hosts, source credentials and existing databases remain unavailable to drills.
After manifest and stored-byte verification, select the preloaded official
`postgres:<source-major>-bookworm` image, resolve its content ID, and create a
randomly named container with a per-run ownership label. Start only the returned
container ID. Use new random credentials and a new named database, network `none`,
no published ports, host binds or shared volumes. Anonymous database volumes are
owned by this container. The Docker daemon and preloaded image are trusted.

Delegate native PostgreSQL operations to the existing adapter through Docker exec;
unset inherited PostgreSQL connection overrides, and check container identity,
image, ownership label, network and mounts before native commands and cleanup.
Reuse the verified immutable snapshot, decompression and ordinary restore pipeline.
The configured production server is never contacted by the drill.

Validate with a read-only transaction that scans ordinary tables/populated
materialized views and reads the catalog. Unpopulated views are valid catalog
objects and are not scanned. This is readability/restore evidence, not expected
row equality, application correctness, hardware-equivalent RTO or PITR evidence.
Record elapsed time and stages in separate exact-backup evidence; health accepts
both SQLite and PostgreSQL records without conflating freshness and integrity.

Dry-run checks the verified artifact, format, image availability and source major
consistency only. It starts no server, skips live compatibility/validation and
creates no evidence. Targets are preserved unless successful validation is followed
by explicit owned-container cleanup, including anonymous volumes. Failure retains
the target for inspection, stopped to terminate any continuing Docker exec.
Stopping uses an independent bounded context after cancellation and failure to
stop is surfaced. No automatic image pull, custom image or shell hook.

## Consequences

Docker is optional for normal backup/restore and SQLite drills, mandatory for
PostgreSQL drills. Official images may lack application extensions; such drills
fail. Operators preload the matching image and need disk/resources on the Docker
host plus storage write permissions for evidence. Docker is an isolation boundary
under the existing trusted-operator model, not protection against a compromised
daemon or container escape. Containers retained for inspection require manual
lifecycle management. MySQL and MongoDB drills remain unsupported.
