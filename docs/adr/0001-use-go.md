# ADR-0001: Use Go as the Primary Implementation Language

## Status

Proposed

## Context

DBVault is designed to be a production-grade database backup and restore CLI utility that operates across Linux, macOS, and Windows environments, as well as within containerized environments (Kubernetes, Docker).

The project requires:
1. Fast startup times with minimal runtime overhead.
2. Low memory footprint during long-running streaming operations.
3. Easy distribution as a single, statically linked binary without runtime interpreter dependencies (unlike Python, Node.js, or Ruby).
4. Robust primitives for concurrency, I/O streaming (`io.Reader`, `io.Writer`, `io.Pipe`), and OS signal handling.
5. Cross-compilation capabilities.

## Decision

We propose using Go (version 1.22+) as the primary implementation language for DBVault.

## Alternatives Considered

- **Rust:** Offers strong memory safety and performance. However, Go's standard library provides superior native I/O streaming abstractions, faster compile times, simpler concurrency models for CLI orchestration, and lower barrier to open-source contributions.
- **Python:** Offers rapid prototyping and rich database libraries, but requires an external runtime, virtual environments, and complex dependency management when packaged as a CLI tool.
- **Bash / Shell Scripts:** Ubiquitous, but notoriously difficult to test, prone to command injection, lack structured error handling, and have poor cross-platform support on Windows.

## Consequences

### Positive
- Produces a single, self-contained, statically linked binary for any target OS/architecture (`GOOS`/`GOARCH`).
- Rich standard library support for streaming (`io`), compression (`compress/gzip`), cryptography (`crypto/sha256`), and process execution (`os/exec`).
- Strong ecosystem of production-grade libraries, such as Cobra for CLI commands and AWS SDK for Go.

### Negative
- Go applications have a lightweight garbage collector, which requires careful stream management to prevent GC pressure during high-throughput data transfers.
