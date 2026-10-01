# ADR-0006: External OS/Orchestrator Scheduling with Future In-Process Daemon

## Status

Proposed

## Context

Periodic backup execution is a core requirement for database operations. Backup scheduling can be approached in two primary ways:
1. **Rely on external schedulers:** Operating system and platform tools (`cron`, `systemd` timers, Kubernetes `CronJob`, Windows Task Scheduler) execute DBVault as a one-shot process.
2. **Build an in-process daemon:** An internal scheduler runs DBVault as a persistent background daemon executing backups based on cron expressions.

Long-running in-process backup daemons can suffer from memory leaks, zombie process accumulation if child processes hang, and require complex supervisory infrastructure to guarantee high availability.

## Decision

We propose designing DBVault primarily as an idempotent, one-shot CLI utility optimized for execution by battle-tested external schedulers (`systemd` timers, Kubernetes `CronJobs`, and Linux `cron`).

In a future phase, an optional `dbvault schedule` command will be provided for containerized environments where external cron schedulers are unavailable.

## Alternatives Considered

- **Daemon-Only Tool:** Requiring DBVault to run permanently in the background. Rejected because standard infrastructure tooling already provides robust process supervision, logging, and retry mechanisms.

## Consequences

### Positive
- Stateless execution: Each backup runs in a clean process space with zero memory leak accumulation.
- Native integration with platform observability tools (systemd journal, Kubernetes Pod logs, Datadog/CloudWatch).
- Simplifies operational lifecycle and disaster recovery.

### Negative
- Environments lacking native scheduling tools (such as minimal scratch Docker containers) must wait for the in-process daemon implementation or rely on external orchestrators.
