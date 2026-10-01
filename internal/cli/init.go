package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/mongodb"
	"github.com/sung2708/DBVault/internal/database/mysql"
	"github.com/sung2708/DBVault/internal/database/postgres"
	"github.com/sung2708/DBVault/internal/database/sqlite"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/onboarding"
	"github.com/sung2708/DBVault/internal/presentation"
	"github.com/sung2708/DBVault/internal/security"
)

// databaseAdapter is shared by operational commands and setup's connection test.
func databaseAdapter(cfg config.Config, password string, redactor *security.Redactor) (database.Adapter, error) {
	switch cfg.Database.Type {
	case "postgres":
		return &postgres.Adapter{Config: cfg.Database, Password: password, Runner: runner.Native{Redactor: redactor}}, nil
	case "mysql":
		return &mysql.Adapter{Config: cfg.Database, Password: password, Runner: runner.Native{Redactor: redactor}}, nil
	case "mongodb":
		return &mongodb.Adapter{Config: cfg.Database, Password: password, Runner: runner.Native{Redactor: redactor}}, nil
	case "sqlite":
		return &sqlite.Adapter{Config: cfg.Database}, nil
	}
	return nil, fmt.Errorf("configured engine is not implemented")
}

func (o *options) initCommand() *cobra.Command {
	c := &cobra.Command{
		Use: "init", Short: "Create a configuration with guided setup or flags", GroupID: "configuration", Args: noPositionalArgs,
		Long:    "Initialize DBVault configuration using the runtime YAML schema.\nOn a terminal, missing values are prompted with inline keyboard controls.\nComplete flags skip the wizard; --non-interactive, JSON and non-TTY input never prompt.\nPasswords are referenced by environment variable and never saved.\nExisting files require explicit overwrite approval or --force.\nThe default destination is dbvault.yaml in the current directory.\nUse config to validate an existing file; test checks database tools/connectivity.",
		Example: "  dbvault init\n  dbvault init --database postgres --storage local\n  dbvault init --non-interactive --database postgres --database-name production --user dbvault --storage local\n  dbvault init --non-interactive --database sqlite --database-name ./app.db --storage local --config ./dbvault.yaml",
	}
	f := c.Flags()
	for _, flag := range []struct{ name, help string }{
		{"database", "Database engine: postgres, mysql, mongodb, sqlite"}, {"database-name", "Database name (SQLite: database file path)"},
		{"storage", "Storage backend: local, s3, gcs, azure"}, {"host", "Database hostname (default: configuration default)"},
		{"user", "Database username; required for network databases"}, {"password-env", "Password environment variable name (default: DBVAULT_DB_PASSWORD); never its value"},
		{"ssl-mode", "TLS mode (default: prefer; MongoDB: require)"}, {"auth-database", "MongoDB authentication database (default: admin)"},
		{"output-dir", "Local backup directory (default: ./backups)"}, {"bucket", "S3/GCS bucket; must already exist"}, {"region", "AWS region; required for S3"},
		{"container", "Azure container; must already exist"}, {"account-name", "Azure storage account name"}, {"prefix", "Cloud object prefix (default: empty)"},
		{"compression", "Compression: gzip, zstd, none (default: configuration default)"},
	} {
		f.String(flag.name, "", flag.help)
	}
	f.Int("port", 0, "Database port (default: 5432/3306/27017 for selected engine)")
	f.Bool("quiesced", false, "Acknowledge stopping writes during MongoDB backups; does not stop writes")
	f.Bool("non-interactive", false, "Never prompt; fail if required flags are missing")
	f.Bool("force", false, "Explicitly authorize replacing an existing regular configuration file")
	f.Bool("test", false, "Test database tools/connectivity before writing (requires credentials); does not test storage")
	f.Duration("timeout", 30*time.Second, "Connection test deadline; must be positive")
	c.RunE = func(c *cobra.Command, _ []string) error {
		values := map[string]string{}
		f.Visit(func(flag *pflag.Flag) {
			if flag.Value.Type() == "string" {
				values[flag.Name] = flag.Value.String()
			}
		})
		if f.Changed("port") {
			v, _ := f.GetInt("port")
			values["port"] = strconv.Itoa(v)
		}
		if f.Changed("quiesced") {
			v, _ := f.GetBool("quiesced")
			values["quiesced"] = strconv.FormatBool(v)
		}
		nonInteractive, _ := f.GetBool("non-interactive")
		force, _ := f.GetBool("force")
		test, _ := f.GetBool("test")
		timeout, _ := f.GetDuration("timeout")
		if timeout <= 0 {
			return fmt.Errorf("--timeout must be positive")
		}
		o.redactor = setupRedactor(values["password-env"])
		var prompt onboarding.Prompter
		if len(onboarding.Required(values)) > 0 && presentation.CanPrompt(c.InOrStdin(), c.ErrOrStderr(), nonInteractive, jsonMode(c), globalBool(c, "quiet")) {
			prompt = &presentation.TerminalPrompter{In: c.InOrStdin(), Out: c.ErrOrStderr(), NoColor: o.noColor || !presentation.Detect(c.OutOrStdout()).TTY, Redactor: o.redactor}
		}
		result, err := onboarding.Run(c.Context(), onboarding.Options{
			Values: values, Path: o.configPath, PathProvided: c.Flags().Changed("config"), Force: force, Test: test, TestProvided: f.Changed("test"), Prompt: prompt,
			TestConnection: func(ctx context.Context, cfg config.Config, password string) error {
				redactor := setupRedactor(cfg.Database.PasswordEnv, password)
				adapter, err := databaseAdapter(cfg, password, redactor)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				_, err = (&app.Service{Config: cfg, DB: adapter}).Test(ctx)
				return redactor.Error(err)
			},
		})
		if err != nil {
			return o.redactor.Error(err)
		}
		return o.output(c, result)
	}
	return c
}

func setupRedactor(env string, additional ...string) *security.Redactor {
	if env == "" {
		env = "DBVAULT_DB_PASSWORD"
	}
	return security.New(append(additional, os.Getenv(env), os.Getenv("DB_PASSWORD"), os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN"), os.Getenv("AZURE_CLIENT_SECRET"), os.Getenv("AZURE_STORAGE_KEY"), os.Getenv("SLACK_WEBHOOK_URL"))...)
}
