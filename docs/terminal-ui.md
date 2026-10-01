# Terminal output

The same CLI and business pipeline serve human and machine callers. Cobra owns
command discovery; `internal/presentation` uses Lip Gloss for restrained cyan
headings and semantic status colors. Core `internal/app` emits neutral events
without importing terminal libraries. No backup/restore semantics are changed.

`init` adds optional inline forms through Charm Huh in the presentation layer.
The onboarding workflow uses an injected Prompter interface, without terminal
dependencies. Questions support arrows/Enter, Esc and Ctrl+C, with masked secret
input for temporary connection tests. No alternate-screen dashboard or mouse UI
is used. `TERM=dumb` falls back to numbered choices, yes/no and plain input.
Prompt/review messages go to stderr; final results go to stdout. Existing commands
keep their flags and destructive-operation guards; no restore selection is added.

Both stdin and stderr must be terminals to prompt. Complete required flags,
`--non-interactive`, JSON and quiet mode bypass prompts. `--no-color` and
`NO_COLOR` suppress prompt colors; keyboard controls still function. A setup
cancellation restores terminal state and leaves the destination unchanged.

`update check` renders stable release status through the same renderer. It uses
the semantic success/warning/error colors, shows a versioned Go install command
only for validated official stable tags, and never animates redirected or JSON
output. Failures stay concise and nonzero; `dev` builds skip network access.

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
