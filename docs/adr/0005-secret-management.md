# ADR-0005: Environment-Driven Secret Management and Process Isolation

## Status

Proposed

## Context

Database backup tools require administrative or privileged read access to target databases. Exposing database passwords and cloud credentials presents major security risks:
1. Passing passwords via command-line flags (e.g. `--password secret`) leaks credentials to all users on the host system via `ps aux`, `/proc`, and process monitoring tools.
2. Storing plain passwords in configuration files risks accidental commits to version control repositories.
3. Unsanitized error logs can print connection strings containing embedded credentials.

## Decision

We propose establishing strict secret management principles across DBVault:
1. **Never support passwords as CLI flags.**
2. **Environment Variable Redirection:** Configuration files declare the name of an environment variable (`password_env: DB_PASSWORD`) rather than holding the raw secret string.
3. **Process Isolation:** Secrets are passed to native child processes using dedicated environment variables (`PGPASSWORD`, `MYSQL_PWD`) within the specific child process environment scope (`cmd.Env`), never exposed to the host process table.
4. **Sanitized Logging:** All log outputs, manifests, and error messages strip credentials from connection URLs.

## Alternatives Considered

- **Interactive Prompts (`terminal.ReadPassword`):** Safe for manual CLI runs, but unusable for automated cron/orchestrator jobs.
- **Encrypted Password Files:** Requires local encryption key management and introduces key distribution bootstrapping problems.

## Consequences

### Positive
- Fully prevents credential exposure in system process tables (`ps aux`, `/proc`).
- Compatible with secret management solutions (Kubernetes Secrets, AWS Secrets Manager, HashiCorp Vault) which inject secrets as environment variables.
- Configuration files can be safely checked into version control without risk of leaking credentials.

### Negative
- Requires operators to configure environment variables in their shell or orchestration units (systemd `EnvironmentFile` or Kubernetes `envFrom`).
