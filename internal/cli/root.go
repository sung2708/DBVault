package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/notify"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/retention"
	"github.com/sung2708/DBVault/internal/security"
	"github.com/sung2708/DBVault/internal/storage"
	"github.com/sung2708/DBVault/internal/storage/providers"
	"github.com/sung2708/DBVault/internal/update"
)

type Build struct{ Version, Commit, Date string }
type options struct {
	configPath     string
	verbose, json  bool
	outputMode     string
	quiet, noColor bool
	build          Build
	redactor       *security.Redactor
	updates        update.Service
}

func New(b Build, out, errOut io.Writer) *cobra.Command {
	return newWithUpdateService(b, out, errOut, nil)
}

func newWithUpdateService(b Build, out, errOut io.Writer, updates update.Service) *cobra.Command {
	if updates == nil {
		updates = &update.Checker{}
	}
	o := &options{build: b, redactor: security.New(), updates: updates}
	root := &cobra.Command{Use: "dbvault", Short: "Database backup, verification and restore", Long: "DBVault creates and restores verified database backups.\nSupports PostgreSQL, MySQL, MongoDB and SQLite with local, S3, GCS or Azure storage.\n\nOperations use dbvault.yaml in the current directory; select another YAML file\nwith --config. Start with init to create it, doctor to check readiness, then\nbackup --dry-run before the first backup.", Args: noPositionalArgs, SilenceUsage: true, SilenceErrors: true, Version: b.Version, Example: "  dbvault init\n  dbvault doctor\n  dbvault backup --dry-run\n  dbvault backup --help\n  dbvault schedule --help", RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	root.SuggestionsMinimumDistance = 2
	streamLock := &sync.Mutex{}
	root.SetOut(&presentation.LockedWriter{Writer: out, Mutex: streamLock})
	root.SetErr(&presentation.LockedWriter{Writer: errOut, Mutex: streamLock})
	root.SetFlagErrorFunc(flagError)
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVarP(&o.configPath, "config", "c", "dbvault.yaml", "YAML configuration file (database, storage and policies)")
	root.PersistentFlags().BoolVarP(&o.verbose, "verbose", "v", false, "Enable debug logging")
	root.PersistentFlags().BoolVar(&o.json, "json", false, "Emit JSON results/logs (alias for --output json)")
	root.PersistentFlags().StringVar(&o.outputMode, "output", "text", "Result format: text or json (help remains human-readable)")
	root.PersistentFlags().BoolVar(&o.noColor, "no-color", false, "Disable colors and animations (also respects NO_COLOR)")
	root.PersistentFlags().BoolVarP(&o.quiet, "quiet", "q", false, "Suppress progress and decoration; keep results and errors")
	root.PersistentPreRunE = validateOutput
	root.InitDefaultVersionFlag()
	if versionFlag := root.Flags().Lookup("version"); versionFlag != nil {
		versionFlag.Usage = "Show DBVault version; use version for build details"
	}
	installHelp(root, o)
	root.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"}, &cobra.Group{ID: "operations", Title: "Operations:"}, &cobra.Group{ID: "configuration", Title: "Configuration:"}, &cobra.Group{ID: "other", Title: "Other:"})
	root.SetHelpCommandGroupID("other")
	for _, name := range []string{"backup", "restore", "export", "history", "test", "list", "verify", "inspect", "delete", "cleanup", "config"} {
		root.AddCommand(o.command(name))
	}
	root.AddCommand(&cobra.Command{Use: "version", Short: "Show build and runtime information", Long: "Show the DBVault version, Git commit, build time and Go runtime.\nNo configuration file or database connection is needed.", GroupID: "other", Args: noPositionalArgs, Example: "  dbvault version\n  dbvault version --json", RunE: func(c *cobra.Command, _ []string) error {
		return o.output(c, map[string]string{"version": b.Version, "commit": b.Commit, "built": b.Date, "runtime": runtime.Version()})
	}})
	root.AddCommand(o.scheduleCommand())
	root.AddCommand(o.initCommand())
	root.AddCommand(o.updateCommand())
	root.AddCommand(o.doctorCommand())
	root.AddCommand(o.healthCommand(time.Now))
	root.AddCommand(o.statusCommand(time.Now))
	root.AddCommand(o.recoveryCommand())
	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Long = "Show usage, flags and examples for a command or subcommand.\nHelp needs no configuration, credentials or database connection."
			command.Example = "  dbvault help backup\n  dbvault help schedule add"
			break
		}
	}
	// Final presentation is centralized so native and filesystem errors cannot
	// expose resolved secrets, while typed causes remain available for exit codes.
	return root
}
func (o *options) output(c *cobra.Command, v any) error {
	if jsonMode(c) {
		return json.NewEncoder(o.redactor.Writer(c.OutOrStdout())).Encode(v)
	}
	return o.display(c).Result(c.Name(), v)
}
func (o *options) load(c *cobra.Command) (config.Config, error) {
	overrides := config.Overrides{}
	if c.Flags().Changed("database") {
		v, _ := c.Flags().GetString("database")
		overrides.Database = &v
	}
	if c.Flags().Changed("output-dir") {
		v, _ := c.Flags().GetString("output-dir")
		overrides.OutputDir = &v
	}
	if c.Flags().Changed("compression") {
		v, _ := c.Flags().GetString("compression")
		overrides.Compression = &v
	}
	if c.Flags().Changed("keep-days") {
		v, _ := c.Flags().GetInt("keep-days")
		overrides.KeepDays = &v
	}
	if c.Flags().Changed("keep-count") {
		v, _ := c.Flags().GetInt("keep-count")
		overrides.KeepCount = &v
	}
	cfg, err := config.Load(o.configPath, overrides)
	o.redactor = security.New(os.Getenv(cfg.Database.PasswordEnv), os.Getenv(cfg.Notifications.Slack.WebhookEnv), os.Getenv(cfg.Storage.S3.AccessKeyEnv), os.Getenv(cfg.Storage.S3.SecretKeyEnv), os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN"), os.Getenv(cfg.Storage.Azure.AccountKeyEnv), os.Getenv("AZURE_CLIENT_SECRET"))
	return cfg, o.redactor.Error(err)
}
func (o *options) command(name string) *cobra.Command {
	c := &cobra.Command{Use: name, Args: noPositionalArgs}
	applyCommandHelp(c)
	f := c.Flags()
	switch name {
	case "backup":
		f.StringP("database", "d", "", "Override configured database name (SQLite: file path)")
		f.StringP("output-dir", "o", "", "Override local storage directory; does not select a backend")
		f.String("compression", "", "Compression: none, gzip, zstd (unset: configuration)")
		f.String("type", "full", "Backup type: full; incremental/differential are unsupported")
		f.Bool("dry-run", false, "Validate tools and connectivity without writing a backup")
		f.Duration("timeout", 2*time.Hour, "Operation limit; 0 disables timeout")
	case "restore":
		f.Bool("new-database", false, "Create a new destination; default name includes UTC date/time and a unique suffix")
		f.Bool("backup-before-restore", false, "Create and verify a full backup of the existing destination before writing")
		f.Bool("interactive", false, "Select backup and destination using inline keyboard controls (TTY only)")
		f.Bool("non-interactive", false, "Never prompt; require explicit flags")
		f.StringP("database", "d", "", "Override destination database name (SQLite: existing file)")
		f.StringP("target", "t", "", targetHelp)
		f.Bool("confirm", false, "Authorize destructive restore")
		f.Bool("clean", false, "Drop restored objects first (PostgreSQL/MongoDB only)")
		f.Bool("dry-run", false, "Verify artifact and target without database writes")
		f.StringSlice("table", nil, "PostgreSQL tables to restore (repeat or comma-separate)")
		f.StringSlice("schema", nil, "PostgreSQL schemas to restore (repeat or comma-separate)")
		f.StringSlice("collection", nil, "MongoDB collections to restore (repeat or comma-separate)")
		f.Duration("timeout", 4*time.Hour, "Operation limit; 0 disables timeout")
	case "export":
		f.StringP("target", "t", "", targetHelp)
		f.String("file", "", "New output file path; existing files are refused")
		f.Bool("decompress", false, "Export native dump/image without outer gzip/zstd compression")
		f.Duration("timeout", 2*time.Hour, "Operation limit; 0 disables timeout")
	case "history":
		f.IntP("limit", "n", 50, "Maximum restore records to display")
	case "verify", "inspect", "delete":
		f.StringP("target", "t", "", targetHelp)
		if name == "delete" {
			f.Bool("confirm", false, "Authorize deletion")
			f.Bool("dry-run", false, "Validate selection without deleting")
		}
	case "list":
		f.String("prefix", "", "Only show backup names starting with this prefix")
		f.IntP("limit", "n", 50, "Maximum backups to display")
	case "cleanup":
		f.Bool("dry-run", false, "Show candidates without deleting")
		f.Int("keep-days", 0, "Protect backups within this many days (unset: config; 0: off)")
		f.Int("keep-count", 0, "Protect recent backups per database (unset: config; 0: off)")
	case "test":
		f.Duration("timeout", 30*time.Second, "Preflight deadline")
	}
	c.PreRunE = validateCLIValues
	c.RunE = func(c *cobra.Command, _ []string) error { err := o.run(c, name); return o.redactor.Error(err) }
	return c
}
func (o *options) run(c *cobra.Command, name string) error {
	if name == "restore" {
		if err := o.selectRestore(c); err != nil {
			return err
		}
	}
	target, _ := c.Flags().GetString("target")
	confirm, _ := c.Flags().GetBool("confirm")
	dry, _ := c.Flags().GetBool("dry-run")
	switch name {
	case "restore", "export", "verify", "inspect", "delete":
		if target == "" {
			return fmt.Errorf("--target is required; use dbvault list to find a backup name")
		}
	}
	if (name == "restore" || name == "delete") && !confirm && !dry {
		return fmt.Errorf("--confirm is required for %s; use --dry-run to preview", name)
	}
	limit, _ := c.Flags().GetInt("limit")
	if (name == "list" || name == "history") && limit < 1 {
		return fmt.Errorf("--limit must be positive")
	}
	ctx := c.Context()
	if c.Flags().Lookup("timeout") != nil {
		duration, _ := c.Flags().GetDuration("timeout")
		if duration < 0 {
			return fmt.Errorf("--timeout must be nonnegative")
		}
		if duration > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, duration)
			defer cancel()
		}
	}
	display := o.display(c)
	stopDisplay := display.Start(ctx)
	defer stopDisplay()
	details := presentation.Details{Operation: name, Target: target, ConfigPath: o.configPath, DryRun: dry, Started: time.Now()}
	display.Configure(details, o.redactor)
	display.Step("configuration")
	cfg, err := o.load(c)
	if err == nil && name == "restore" {
		newDB, _ := c.Flags().GetBool("new-database")
		if newDB && !c.Flags().Changed("database") {
			cfg.Database.Database, err = app.NewDestination(cfg.Database.Type, cfg.Database.Database, time.Now())
		}
	}
	details.Config = cfg
	display.Configure(details, o.redactor)
	if err != nil {
		return err
	}
	display.Done("configuration")
	if name == "config" {
		return o.output(c, "Configuration valid")
	}
	needsDB := name == "backup" || name == "restore" || name == "test"
	var adapter database.Adapter
	if needsDB {
		password, err := cfg.Password()
		if err != nil {
			return err
		}
		adapter, err = databaseAdapter(cfg, password, o.redactor)
		if err != nil {
			return fault.Wrap(fault.Unsupported, "database.type", err)
		}
	}
	// Preflight and backup dry-runs never initialize or mutate storage.
	var store storage.Provider
	if name != "test" && !(name == "backup" && dry) {
		var closeStore func() error
		store, closeStore, err = providers.Open(ctx, cfg.Storage)
		if err != nil {
			return fault.Wrap(fault.Storage, "initialize storage", err)
		}
		defer closeStore()
	}
	level := slog.LevelInfo
	if o.verbose {
		level = slog.LevelDebug
	}
	handlerOptions := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if globalBool(c, "quiet") {
		handlerOptions.Level = slog.LevelError
	}
	if jsonMode(c) {
		handler = slog.NewJSONHandler(o.redactor.Writer(c.ErrOrStderr()), handlerOptions)
	} else {
		handler = &presentation.HumanHandler{Renderer: display}
	}
	svc := &app.Service{Config: cfg, DB: adapter, Store: store, Logger: slog.New(handler), Version: o.build.Version}
	if name != "list" && name != "cleanup" {
		svc.Observe = display.Observe
	}
	if cfg.Notifications.Slack.Enabled && (name == "backup" || name == "restore") {
		hook := os.Getenv(cfg.Notifications.Slack.WebhookEnv)
		if hook == "" {
			return fmt.Errorf("notifications.slack webhook environment variable is missing")
		}
		svc.Notifier = &notify.Slack{URL: hook, Channel: cfg.Notifications.Slack.Channel, Redactor: o.redactor}
	}
	var key string
	if target != "" {
		key, err = providers.Key(store, target)
		if err != nil {
			return err
		}
	}
	switch name {
	case "test":
		info, err := svc.Test(ctx)
		if err != nil {
			return err
		}
		return o.output(c, info)
	case "backup":
		kind, _ := c.Flags().GetString("type")
		result, err := svc.BackupWithResult(ctx, kind, dry)
		if err != nil {
			if result.Manifest.Name != "" {
				if outputErr := o.output(c, result); outputErr != nil {
					return errors.Join(err, outputErr)
				}
			}
			return err
		}
		if dry {
			return o.output(c, "Preflight passed; no backup created")
		}
		return o.output(c, result)
	case "restore":
		if !dry {
			display.Warning("Restore may overwrite database data. Authorized by --confirm; stop application writes.")
		}
		clean, _ := c.Flags().GetBool("clean")
		tables, _ := c.Flags().GetStringSlice("table")
		schemas, _ := c.Flags().GetStringSlice("schema")
		collections, _ := c.Flags().GetStringSlice("collection")
		newDB, _ := c.Flags().GetBool("new-database")
		backupBefore, _ := c.Flags().GetBool("backup-before-restore")
		m, err := svc.ReadManifest(ctx, key)
		if err != nil {
			return err
		}
		display.RestorePreview(m, cfg.Database, newDB, backupBefore, clean, tables, schemas, collections)
		result, restoreErr := svc.RestoreWithResult(ctx, key, confirm, dry, app.RestoreRequest{NewDatabase: newDB, BackupBefore: backupBefore, Options: database.RestoreOptions{Clean: clean, Tables: tables, Schemas: schemas, Collections: collections}})
		if result.Backup.ID != "" {
			return errors.Join(restoreErr, o.output(c, result))
		}
		return restoreErr
	case "export":
		path, _ := c.Flags().GetString("file")
		decompress, _ := c.Flags().GetBool("decompress")
		result, err := svc.Export(ctx, key, path, decompress)
		if err != nil {
			return err
		}
		return o.output(c, result)
	case "history":
		items, err := svc.RestoreHistory(ctx)
		if err != nil {
			return err
		}
		if len(items) > limit {
			items = items[:limit]
		}
		return o.output(c, items)
	case "verify":
		m, err := svc.Verify(ctx, key)
		if err != nil {
			return err
		}
		return o.output(c, m)
	case "inspect":
		display.Step("manifest")
		m, err := svc.ReadManifest(ctx, key)
		if err != nil {
			return err
		}
		display.Done("manifest")
		return o.output(c, m)
	case "delete":
		if !dry {
			display.Warning("Deletion is permanent. Authorized by --confirm.")
		}
		if err := svc.Delete(ctx, key, confirm, dry); err != nil {
			return err
		}
		return o.output(c, "Delete checks passed")
	case "list":
		display.Step("list")
		prefix, _ := c.Flags().GetString("prefix")
		items, err := svc.List(ctx, prefix)
		if err != nil {
			return err
		}
		display.Done("list")
		if len(items) > limit {
			items = items[:limit]
		}
		return o.output(c, items)
	case "cleanup":
		display.Step("cleanup.select")
		items, err := svc.List(ctx, "")
		if err != nil {
			return err
		}
		candidates, err := retention.Select(items, cfg.Retention, time.Now())
		if err != nil {
			return err
		}
		display.Done("cleanup.select")
		if !dry {
			if len(candidates) > 0 {
				display.Warning(fmt.Sprintf("Cleanup will delete %d backups immediately; no confirmation flag is required.", len(candidates)))
			}
			// Verify protected backups as well: cleanup cannot rely on corrupt newest
			// artifacts as the only recovery point.
			display.Step("cleanup.verify")
			for _, m := range items {
				if _, err := svc.Verify(ctx, m.Name); err != nil {
					return err
				}
			}
			display.Done("cleanup.verify")
			display.Step("cleanup.delete")
			for _, m := range candidates {
				if err := svc.Delete(ctx, m.Name, true, false); err != nil {
					return err
				}
			}
			display.Done("cleanup.delete")
		}
		return o.output(c, candidates)
	}
	return fmt.Errorf("unknown operation")
}
