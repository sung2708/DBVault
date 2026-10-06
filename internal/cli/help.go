package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/doctor"
	"github.com/sung2708/DBVault/internal/fault"
)

const targetHelp = "Required backup name from list; local paths must stay inside storage root"

var invalidFlagValue = regexp.MustCompile(`for "(--?[^"]+)" flag`)

// pflag's parse errors echo rejected values. Describe the expected type instead
// so accidentally pasted passwords are not repeated on stderr.
func flagError(c *cobra.Command, err error) error {
	match := invalidFlagValue.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return err
	}
	flagName := match[1]
	if comma := strings.LastIndex(flagName, ", "); comma >= 0 {
		flagName = flagName[comma+2:]
	}
	name := strings.TrimLeft(flagName, "-")
	flag := c.Flags().Lookup(name)
	if flag == nil && len(name) == 1 {
		flag = c.Flags().ShorthandLookup(name)
	}
	if flag == nil {
		return fmt.Errorf("invalid value for %s", match[1])
	}
	expected := flag.Value.Type()
	switch expected {
	case "duration":
		expected = "a duration such as 30s, 2h, or 0"
	case "int":
		expected = "an integer"
	case "bool":
		expected = "true or false"
	case "stringSlice":
		expected = "comma-separated values or repeated flags"
	}
	return fmt.Errorf("invalid value for --%s: expected %s", flag.Name, expected)
}

type commandHelp struct{ short, long, examples, group string }

func applyCommandHelp(c *cobra.Command) {
	h := map[string]commandHelp{
		"export":  {"Export a verified backup to a new local file", "Download and verify the exact stored archive before exporting.\nExisting files are refused. --decompress removes outer gzip/zstd compression;\nthe output remains the engine's native dump/archive or SQLite image.\nNo database credentials or native tools are needed.", "  dbvault export --target backup.dump.gz --file ./copy.dump.gz\n  dbvault export --target backup.dump.gz --file ./copy.dump --decompress", "core"},
		"history": {"Show completed, failed and cancelled restore attempts", "Read separate restore history records from configured storage.\nHistory includes backup identity, destination, UTC times, selectors and safety backup.\nDry runs produce no records. This does not replace isolated recovery drill evidence.", "  dbvault history\n  dbvault history --limit 10 --json", "operations"},
		"backup":  {"Create a full database backup", "Back up the configured database to the configured storage backend.\nCompression comes from configuration unless overridden. Full and logical delta incremental backups are supported; table/collection backup filters are configured in YAML.\n\nUse --dry-run to check tools and connectivity without creating an archive.\nSet protection.verify_after_backup: true to stream-read the stored artifact and\nverify its SHA-256 after upload; remote providers incur a full read and bandwidth cost.\nThis verifies bytes, not recoverability. Otherwise use verify after backup as needed.", "  dbvault backup --dry-run\n  dbvault backup\n  dbvault backup --config production.yaml --compression zstd", "core"},
		"restore": {"Restore a verified backup into a database", "Restore the backup selected by --target into the configured destination.\nGet the backup name from dbvault list (use the name, not the manifest ID).\nLocal storage also accepts paths inside its root; cloud storage accepts names.\n\nRestore can overwrite data. Stop application writes and preview with --dry-run;\nactual restore requires --confirm. Stored bytes are verified before any writes.\nSelected restore is available for PostgreSQL and MongoDB only.", "  dbvault list\n  # Replace backup.dump.gz with a backup name returned by list.\n  dbvault restore --target backup.dump.gz --dry-run\n  dbvault restore --target backup.dump.gz --confirm\n  dbvault restore --target backup.dump.gz --database recovery --confirm", "core"},
		"verify":  {"Check and record stored backup integrity", "Read the selected archive and verify its size and SHA-256 against its manifest.\nAn immutable verification evidence record is appended for health/status. Use a\nbackup name from dbvault list for --target. No database connection or database\nwrites occur. To check destination compatibility, use restore --dry-run.", "  dbvault list\n  dbvault verify --target backup.dump.gz\n  dbvault verify --target backup.dump.gz --json", "core"},
		"inspect": {"Show backup metadata without reading the archive", "Show validated metadata for a backup name from dbvault list.\nThis does not verify archive bytes; run verify for an integrity check.", "  dbvault inspect --target backup.dump.gz\n  dbvault inspect --target backup.dump.gz --json", "core"},
		"list":    {"List completed backups in configured storage", "List registered backups, including their names and metadata.\nUse a returned name with --target for inspect, verify, restore or delete.\nThe configured storage backend determines which backups are listed.", "  dbvault list\n  dbvault list --limit 10\n  dbvault list --prefix postgres --json", "core"},
		"delete":  {"Delete one backup archive and its metadata", "Permanently delete a backup selected by --target from configured storage.\nUse a name from dbvault list. Preview with --dry-run; deletion requires --confirm.\nThis removes the archive and manifest, not data in the source database.", "  dbvault delete --target backup.dump.gz --dry-run\n  dbvault delete --target backup.dump.gz --confirm", "operations"},
		"cleanup": {"Delete backups outside retention protections", "Apply configured retention to backups in the storage backend. Backups protected\nby either age or count are kept; the newest per database is always protected.\nBoth policies set to zero disable deletion.\n\nPreview with --dry-run. Without it, cleanup deletes candidates immediately:\nthere is no confirmation flag. All registered archives are verified first.", "  dbvault cleanup --dry-run\n  dbvault cleanup --keep-days 30 --keep-count 7 --dry-run\n  dbvault cleanup --keep-days 30 --keep-count 7", "operations"},
		"test":    {"Check tools and database connectivity", "Check database connectivity and version compatibility, including native tools\nwhere required. SQLite checks the configured database file. Credentials come\nfrom the environment variables named in configuration.\n\nNo backup or storage connection is created. Next, try backup --dry-run.", "  dbvault test\n  dbvault test --config production.yaml --timeout 1m", "operations"},
		"config":  {"Validate a YAML configuration file", "Validate configuration schema and supported option values without connecting\nto a database or storage. This does not check credentials or print configuration.\nChoose local, s3, gcs or azure with storage.type in YAML.\n\nTo create a configuration, use init. Next, run test to check database connectivity.\nThis command has no subcommands.", "  dbvault config\n  dbvault config --config production.yaml", "configuration"},
	}[c.Name()]
	c.Short, c.Long, c.Example, c.GroupID = h.short, h.long, h.examples, h.group
	if c.Name() == "restore" {
		c.Long += "\n\nUse --new-database to create a destination with a UTC date/time name, or supply\n--database to choose its name. Existing destinations are refused in this mode.\nUse --backup-before-restore for a full, verified existing-destination backup.\nIn a terminal, omit --target or use --interactive for inline selection.\nJSON, quiet, non-terminal and --non-interactive invocations never prompt.\nUse history to review restore attempts; export downloads verified backup files."
		c.Example += "\n  dbvault restore --interactive\n  dbvault restore --target backup.dump.gz --new-database --dry-run\n  dbvault restore --target backup.dump.gz --new-database --confirm\n  dbvault restore --target backup.dump.gz --backup-before-restore --confirm"
	}
	switch c.Name() {
	case "restore", "export", "verify", "inspect", "delete":
		c.Use += " --target <backup-name>"
	}
}

func applyScheduleHelp(c *cobra.Command) {
	h := map[string]commandHelp{
		"add":     {short: "Save an enabled backup schedule", long: "Save a schedule with a unique --id, a quoted --cron expression and --config.\nThe configuration is validated and its absolute path is saved; credentials are\nnot saved. Adding a job does not start backups: next run dbvault schedule.\nRestart an already running daemon to load this job.", examples: "  dbvault schedule add --id nightly --cron \"0 2 * * *\" --config production.yaml\n  dbvault schedule add --id hourly --cron \"0 * * * *\" --config production.yaml\n  dbvault schedule list"},
		"list":    {short: "List saved schedules and their enabled state", long: "Show saved schedule IDs, cron expressions, configuration paths and enabled state.\nThis reads definitions, not the status of a running daemon. Use an ID with\nenable, disable or remove; run dbvault schedule to start enabled jobs.", examples: "  dbvault schedule list\n  dbvault schedule list --state jobs.json --json"},
		"remove":  {short: "Remove a saved schedule definition", long: "Remove the saved schedule selected by --id. Backups are not deleted.\nUse schedule list to find IDs. This takes effect immediately in the state file\nwithout confirmation; restart a running daemon to stop scheduling this job.", examples: "  dbvault schedule list\n  dbvault schedule remove --id nightly"},
		"enable":  {short: "Enable a saved schedule", long: "Mark the schedule selected by --id as enabled in the state file.\nThis does not start a daemon. Start or restart dbvault schedule to apply it.", examples: "  dbvault schedule enable --id nightly\n  dbvault schedule list"},
		"disable": {short: "Disable a saved schedule", long: "Mark the schedule selected by --id as disabled in the state file.\nRestart a running daemon to apply it; this command does not cancel active work.\nSaved backups and the schedule definition remain available.", examples: "  dbvault schedule disable --id nightly\n  dbvault schedule list"},
	}[c.Name()]
	c.Short, c.Long, c.Example = h.short, h.long, h.examples
	if c.Name() != "list" {
		c.Use += " --id <schedule-id>"
	}
	if c.Name() == "add" {
		c.Use += " --cron <expression>"
	}
}

func noPositionalArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if c.HasAvailableSubCommands() {
		message := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
		if suggestions := c.SuggestionsFor(args[0]); len(suggestions) > 0 {
			message += "\n\nDid you mean?\n  " + strings.Join(suggestions, "\n  ")
		}
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s accepts flags, not positional arguments; see its usage below", c.CommandPath())
}

func validateCLIValues(c *cobra.Command, _ []string) error {
	if c.Flags().Changed("compression") {
		v, _ := c.Flags().GetString("compression")
		if v != "none" && v != "gzip" && v != "zstd" {
			return fmt.Errorf("--compression must be none, gzip, or zstd")
		}
	}
	if c.Flags().Changed("type") {
		v, _ := c.Flags().GetString("type")
		if v != "full" && v != "incremental" {
			return fault.Wrap(fault.Unsupported, "--type", fmt.Errorf("backup type must be full or incremental"))
		}
	}
	return nil
}

// WriteError retains the typed error for exit status; only presentation changes.
func WriteError(w io.Writer, c *cobra.Command, err error) {
	var health *app.HealthFailure
	if errors.As(err, &health) {
		return
	} // The health result already explains the monitoring status.
	var readiness *doctor.ReadinessError
	if errors.As(err, &readiness) && !jsonMode(c) {
		return
	}
	if c.Name() == "init" && errors.Is(err, context.Canceled) && !jsonMode(c) {
		fmt.Fprintln(w, "Setup cancelled.")
		return
	}
	// Main and embedded callers can supply a distinct diagnostic destination.
	c.SetErr(w)
	r := errorDisplay(c)
	r.ErrorTo(w, c.Name(), c.UseLine(), c.CommandPath()+" --help", err)
}
