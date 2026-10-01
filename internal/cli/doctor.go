package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/doctor"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/storage/providers"
	"github.com/sung2708/DBVault/internal/toolresolve"
)

func (o *options) doctorCommand() *cobra.Command {
	var timeout time.Duration
	c := &cobra.Command{
		Use: "doctor", Short: "Check whether this installation is ready for backups",
		Long:    "Run safe readiness checks for configuration, database authentication and tools, storage access, temporary disk space and notification configuration. Doctor does not create backups or database objects, does not write/delete cloud objects, and never sends Slack notifications.",
		Example: "  dbvault doctor\n  dbvault doctor --config production.yaml --json\n  dbvault doctor --timeout 1m",
		Args:    noPositionalArgs,
		RunE:    func(c *cobra.Command, _ []string) error { return o.runDoctor(c, timeout) },
	}
	c.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Overall readiness-check deadline")
	c.PreRunE = func(_ *cobra.Command, _ []string) error {
		if timeout <= 0 {
			return fmt.Errorf("--timeout must be positive")
		}
		return nil
	}
	return c
}

func (o *options) runDoctor(c *cobra.Command, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(c.Context(), timeout)
	defer cancel()
	var r doctor.Report
	add := func(name, category string, status doctor.Status, summary, detail, fix string) {
		r.Add(doctor.Check{Name: name, Category: category, Status: status, Summary: summary, Detail: detail, Remediation: fix})
	}
	cfg, err := o.load(c)
	if err != nil {
		add("configuration", "configuration", doctor.Fail, "Configuration could not be loaded", o.redactor.Text(err.Error()), "Create or repair the file with dbvault init, then run dbvault config.")
		add("database", "database", doctor.Skip, "Database check skipped", "Configuration is not valid", "")
		add("storage", "storage", doctor.Skip, "Storage check skipped", "Configuration is not valid", "")
	} else {
		add("configuration", "configuration", doctor.Pass, "Configuration is valid", "Schema and supported option values passed validation", "")
		password, passErr := cfg.Password()
		if passErr != nil {
			add("database", "database", doctor.Fail, "Database credentials are unavailable", "The configured password environment variable is unset or empty", "Set the environment variable named in database.password_env.")
			add("native_tools", "database", doctor.Skip, "Native tool check skipped", "Database credentials are unavailable", "")
		} else if db, dbErr := databaseAdapter(cfg, password, o.redactor); dbErr != nil {
			add("database", "database", doctor.Fail, "Database adapter could not be initialized", o.redactor.Text(dbErr.Error()), "Review database.type and the installation documentation.")
			add("native_tools", "database", doctor.Skip, "Native tool check skipped", "Database adapter could not be initialized", "")
		} else {
			info, preErr := db.Preflight(ctx)
			if preErr != nil {
				if errors.Is(preErr, context.Canceled) || errors.Is(preErr, context.DeadlineExceeded) {
					add("database", "database", doctor.Fail, "Database preflight did not complete", "The check was canceled or timed out", "Retry with a larger --timeout.")
				} else {
					add("database", "database", doctor.Fail, "Authenticated database preflight failed", o.redactor.Text(preErr.Error()), "Check that the server is reachable and credentials are correct; see dbvault test --help.")
				}
				var fe *fault.Error
				if errors.As(preErr, &fe) && fe.Kind == fault.Dependency {
					add("native_tools", "database", doctor.Fail, "Required native database tools are unavailable", o.redactor.Text(preErr.Error()), "Install the database vendor's client tools, then run dbvault init or configure database.tools.")
				} else {
					add("native_tools", "database", doctor.Skip, "Tool compatibility could not be confirmed", "Database preflight did not pass", "")
				}
			} else {
				add("database", "database", doctor.Pass, "Authenticated database preflight passed", fmt.Sprintf("%s server %s", db.Name(), info.ServerVersion), "")
				detail := "Required native tools were found and version compatibility passed"
				if db.Name() == "sqlite" {
					detail = "SQLite uses its embedded driver; no external database tools are required"
				} else if resolved, resolveErr := toolresolve.ResolveAll(cfg.Database.Type, cfg.Database.Tools, nil); resolveErr == nil {
					detail += "; resolved executable paths:"
					for _, name := range nativeToolOrder(cfg.Database.Type) {
						detail += "\n  " + name + "  " + resolved[name]
					}
				}
				add("native_tools", "database", doctor.Pass, "Database tooling is ready", detail, "")
			}
		}
		if cfg.Storage.Type == "local" {
			doctorLocal(&r, cfg.Storage.Local.Path)
		} else {
			store, closeStore, openErr := providers.Open(ctx, cfg.Storage)
			if openErr != nil {
				add("storage", "storage", doctor.Fail, "Cloud storage could not be initialized", o.redactor.Text(openErr.Error()), "Check cloud credentials and provider configuration.")
			} else {
				defer closeStore()
				var nonce [12]byte
				_, nonceErr := rand.Read(nonce[:])
				var listErr error
				if nonceErr != nil {
					listErr = nonceErr
				} else {
					_, listErr = store.List(ctx, "dbvault-doctor-"+hex.EncodeToString(nonce[:]))
				}
				if listErr != nil {
					add("storage", "storage", doctor.Fail, "Cloud storage read/list check failed", o.redactor.Text(listErr.Error()), "Grant list/read access and check the bucket/container and network.")
				} else {
					add("storage", "storage", doctor.Pass, "Cloud storage is reachable", "A narrow, random-prefix list request succeeded; no objects were created", "")
					add("storage_write", "storage", doctor.Warn, "Cloud write/delete permissions were not tested", "Doctor never creates or removes cloud objects", "Confirm required write and delete permissions through your cloud IAM policy.")
				}
			}
		}
	}
	doctorTemp(&r)
	if !cfg.Notifications.Slack.Enabled {
		add("slack", "notifications", doctor.Skip, "Slack notifications are disabled", "No delivery check was performed", "")
	} else if os.Getenv(cfg.Notifications.Slack.WebhookEnv) == "" {
		add("slack", "notifications", doctor.Fail, "Slack webhook is not configured", "The configured webhook environment variable is unset or empty", "Set the variable named in notifications.slack.webhook_url_env.")
	} else {
		add("slack", "notifications", doctor.Warn, "Slack webhook is configured", "Delivery was not tested; doctor never sends messages", "")
	}
	r.Finalize()
	if err := o.output(c, r); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !r.Ready {
		return &doctor.ReadinessError{Report: r}
	}
	return nil
}

func nativeToolOrder(engine string) []string {
	switch engine {
	case "postgres":
		return []string{"pg_dump", "pg_restore", "psql"}
	case "mysql":
		return []string{"mysqldump", "mysql"}
	case "mongodb":
		return []string{"mongodump", "mongorestore"}
	}
	return nil
}

func doctorLocal(r *doctor.Report, path string) {
	add := func(status doctor.Status, summary, detail, fix string) {
		r.Add(doctor.Check{Name: "storage", Category: "storage", Status: status, Summary: summary, Detail: detail, Remediation: fix})
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		add(doctor.Fail, "Local storage path is invalid", "Could not resolve configured path", "Correct storage.local.path.")
		return
	}
	info, err := os.Stat(abs)
	if err == nil && !info.IsDir() {
		add(doctor.Fail, "Local storage path is not a directory", "Configured path points to a file", "Choose a directory in storage.local.path.")
		return
	}
	if err == nil {
		if _, readErr := os.ReadDir(abs); readErr != nil {
			add(doctor.Fail, "Local storage is not readable", "Directory entries could not be read", "Check read and execute permissions for the storage directory.")
			return
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		add(doctor.Fail, "Local storage path cannot be inspected", "Filesystem denied access", "Check path and permissions.")
		return
	}
	probeDir := abs
	if errors.Is(err, os.ErrNotExist) {
		probeDir = filepath.Dir(abs)
		for {
			if _, statErr := os.Stat(probeDir); statErr == nil {
				break
			}
			parent := filepath.Dir(probeDir)
			if parent == probeDir {
				break
			}
			probeDir = parent
		}
	}
	f, createErr := os.CreateTemp(probeDir, ".dbvault-doctor-*")
	if createErr != nil {
		add(doctor.Fail, "Local storage is not writable", "A temporary permission probe could not be created", "Check write permissions for the storage directory.")
		return
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil || removeErr != nil {
		add(doctor.Fail, "Local storage probe cleanup failed", "Temporary probe could not be fully removed", "Check filesystem permissions and remove any .dbvault-doctor-* probe file.")
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		add(doctor.Warn, "Local storage directory does not exist yet", "Its existing parent is writable; DBVault creates the directory when needed", "Create it before backup if the parent is not the intended storage volume.")
	} else {
		add(doctor.Pass, "Local storage is readable and writable", "Temporary probe was created and removed", "")
	}
}

func doctorTemp(r *doctor.Report) {
	dir := os.TempDir()
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		r.Add(doctor.Check{Name: "temporary_space", Category: "runtime", Status: doctor.Fail, Summary: "Temporary directory is unavailable", Detail: "The system temporary path could not be inspected", Remediation: "Set a writable temporary directory and retry."})
		return
	}
	f, err := os.CreateTemp(dir, ".dbvault-doctor-*")
	if err != nil {
		r.Add(doctor.Check{Name: "temporary_space", Category: "runtime", Status: doctor.Fail, Summary: "Temporary directory is not writable", Detail: "A temporary permission probe could not be created", Remediation: "Check temporary-directory permissions and free space."})
		return
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil || removeErr != nil {
		r.Add(doctor.Check{Name: "temporary_space", Category: "runtime", Status: doctor.Fail, Summary: "Temporary directory probe cleanup failed", Remediation: "Check temporary-directory permissions."})
		return
	}
	detail := dir
	if available, spaceErr := availableBytes(dir); spaceErr == nil {
		detail = fmt.Sprintf("%s; %d bytes available", dir, available)
	}
	r.Add(doctor.Check{Name: "temporary_space", Category: "runtime", Status: doctor.Pass, Summary: "Temporary directory is writable", Detail: detail})
}
