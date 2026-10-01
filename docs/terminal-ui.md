# Terminal output

The same CLI and business pipeline serve human and machine callers. Cobra owns
command discovery; `internal/presentation` uses Lip Gloss for restrained cyan
headings and semantic status colors. Core `internal/app` emits neutral events
without importing terminal libraries. No backup/restore semantics are changed.

Results and requested data go to stdout. Progress, warnings and errors go to
stderr. Human terminals show status icons and borderless tables; narrow terminals
use wrapped labeled records so backup names and paths remain available. Redirecting
either stream disables colors and cursor updates; static stage lines are stable.
`--no-color` or the presence of `NO_COLOR` also disables animation. `--quiet`
keeps results/errors, omits hints/progress and prints only the name after a backup.

Unknown-size backup streams use a spinner with actual consumed bytes, never a
percentage. Verify/snapshot progress uses the registered stored-byte total;
restore progress uses the registered raw-byte total. These measure bytes consumed
by the pipeline, not rows committed or network acknowledgments. Stage completion
appears only after the relevant operation returns successfully. Short stages do
not animate; cancellation stops and clears the one owned transient stderr line.

`--output json` and legacy `--json` bypass human rendering. Stdout remains the
existing JSON result schema; stderr contains JSON logs/errors only, with no ANSI,
banner or spinner. `--quiet --output json` suppresses informational diagnostics.
Help is an explicit human interface even with a JSON flag, and prints plain text.
Errors and user-controlled values are redacted; human values also strip terminal
control characters. `--verbose` exposes more redacted context.

Confirmation, destructive warnings, checksum verification and dry-run behavior
remain mandatory. Restore/delete still require `--confirm` for writes; cleanup
retains its existing immediate retention semantics and should be previewed with
`--dry-run`.

Tests inject terminal capabilities and a clock without spinner sleeps, cover
narrow output, real counters, redaction, cleanup and TTY/non-TTY/JSON policies.
CLI acceptance creates a real private SQLite database, runs backups/lists and
failures, and checks confirmation/dry-run behavior with separate stream capture.
