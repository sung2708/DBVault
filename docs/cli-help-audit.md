# CLI help audit

The CLI help is the command navigation and invocation reference. The README and
configuration/database/storage guides retain deeper setup and operational details.

## Inventory and changes

Audited nodes: root; help; init; doctor; backup; restore; list; verify; inspect; delete;
cleanup; test; config; version; update; update check; schedule;
schedule add/list/remove/enable/disable.
Configuration is a validation command, with no subcommands. Shell completion
is disabled and is not advertised.

Root help groups real commands into Core Commands, Operations, Configuration
and Other. Every node has a description, usage and examples. Commands link to
the next operation in the workflow. Target commands retain `--target`: it is
the archive name from list, not the manifest ID. Local paths remain confined to
the configured root; cloud targets are flat names.

Compression and retention overrides describe configuration-derived defaults.
Only full backups are advertised. Restore selectors identify supported engines;
`--clean` identifies PostgreSQL and MongoDB. Restore/delete describe confirmation
and dry-run; cleanup explicitly warns that non-dry-run deletion is immediate and
does not require a confirmation flag. Schedule help distinguishes saved definitions
from the foreground daemon and explains startup, timezones and restart behavior.

Unknown commands return nonzero status and offer nearby commands. Input errors
include command usage and a command-specific help hint on stderr. Invalid
compression/type/backend errors name supported values. There is no generic
storage backend override on operational commands: `--output json` (alias `--json`) selects machine output
and YAML selects storage. Help remains human-readable, with subtle headings in
a TTY and plain output when redirected, NO_COLOR or JSON flags are present.
Typed flag errors describe the expected type without echoing the rejected value.

The `update check` command reads official stable release metadata, validates tags
and release URLs, and gives version-specific update instructions without installing.
It has only one command-specific flag, `--force`, for bypassing the 24-hour cache.

The `init` command is in the Configuration group and exposes setup-only
`--database` (engine) and `--storage` flags. Operational `--database` still means
database name. Init help documents required flags, non-interactive execution,
environment password references, overwrite protection and its working-directory
config path. Recursive tests include init; the executed results below record the
earlier audit before init was added.

## Acceptance checks

Recursive semantic tests cover both help routes for all 22 nodes, descriptions,
examples, navigation, required input errors, unsupported options, typo suggestions,
destructive behavior, configuration-derived defaults and secret-free output.
Help is exercised with a missing configuration to establish that it does not
connect or need credentials. Tests avoid whole-output whitespace snapshots.

Run the checks with:

```text
go test ./internal/cli ./internal/config
go test ./...
go vet ./...
go test -race ./internal/cli ./internal/config
```

Manual acceptance uses the built binary to inspect each root command and every
schedule subcommand. Examples require the operator's configuration and, for
archive operations, an actual name from list. They do not contain credentials.
No database or destructive operation is invoked by the help audit.

## Executed results (2026-10-01)

| Check | Result |
|---|---|
| Root help and command discovery | PASS |
| Command/subcommand descriptions and examples | PASS |
| Flag descriptions, required inputs and defaults | PASS |
| Error navigation, typo suggestions and nonzero status | PASS |
| Destructive-operation accuracy | PASS |
| Plain redirected output, NO_COLOR and --json help | PASS |
| Recursive semantic tests, both help routes, all 22 nodes | PASS |
| Windows and Linux binary help, all 18 nodes in the pre-init audit | PASS (historical) |
| gofmt, go vet ./..., go test ./... | PASS |
| Race tests for internal/cli and internal/config | PASS |
| Windows amd64, Linux amd64, macOS arm64 builds | PASS |

Linux acceptance executed the final Linux binary in a non-root container.
macOS was cross-compiled, not executed. The initial typo and shorthand error
test failures were fixed; the final checks above passed. No known CLI help issue
remains within this audit scope.
