// Package onboarding coordinates setup without depending on terminal libraries.
package onboarding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/security"
)

type Choice struct{ Label, Value string }
type Prompter interface {
	Select(context.Context, string, []Choice, string) (string, error)
	Input(context.Context, string, string) (string, error)
	Secret(context.Context, string) (string, error)
	Confirm(context.Context, string, bool) (bool, error)
	Message(string)
}
type Options struct {
	Values                                  map[string]string // only explicitly supplied flags
	Path                                    string
	PathProvided, Force, Test, TestProvided bool
	Prompt                                  Prompter // nil means no terminal interaction is permitted
	TestConnection                          func(context.Context, config.Config, string) error
}
type Result struct {
	Path            string `json:"config"`
	Database        string `json:"database"`
	DatabaseName    string `json:"database_name"`
	Storage         string `json:"storage"`
	StorageLocation string `json:"storage_location"`
	Compression     string `json:"compression"`
	PasswordEnv     string `json:"password_env,omitempty"`
	Tested          bool   `json:"connection_verified"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Required(v map[string]string) []string {
	names := []string{"database", "storage", "database-name"}
	if v["database"] != "sqlite" {
		names = append(names, "user")
	}
	switch v["storage"] {
	case "s3":
		names = append(names, "bucket", "region")
	case "gcs":
		names = append(names, "bucket")
	case "azure":
		names = append(names, "container", "account-name")
	}
	var missing []string
	for _, n := range names {
		if v[n] == "" {
			missing = append(missing, "--"+n)
		}
	}
	return missing
}

func Run(ctx context.Context, o Options) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	p := o.Prompt
	if p == nil && len(Required(o.Values)) > 0 {
		return result, fmt.Errorf("required values missing without interactive input: %s; provide these flags or run dbvault init in a terminal", strings.Join(Required(o.Values), ", "))
	}
	if p != nil {
		p.Message("DBVault Setup\nLet's create your DBVault configuration. Esc / Ctrl+C cancels setup.")
	}
	c := config.Defaults()
	c.Version = "1"
	get := func(key, title, fallback string, choices []Choice) (string, error) {
		if v, ok := o.Values[key]; ok {
			return v, nil
		}
		if p == nil {
			return fallback, nil
		}
		if len(choices) > 0 {
			return p.Select(ctx, title, choices, fallback)
		}
		for {
			value, err := p.Input(ctx, title, fallback)
			if err != nil {
				return "", err
			}
			required := key == "database-name" || key == "user" || key == "bucket" || key == "region" || key == "container" || key == "account-name"
			if !required || value != "" {
				return value, nil
			}
			p.Message("This value is required.")
		}
	}
	var err error
	c.Database.Type, err = get("database", "Database", "postgres", []Choice{{"PostgreSQL", "postgres"}, {"MySQL", "mysql"}, {"MongoDB", "mongodb"}, {"SQLite", "sqlite"}})
	if err != nil {
		return result, err
	}
	if config.DefaultPort(c.Database.Type) == 0 && c.Database.Type != "sqlite" {
		return result, fmt.Errorf("--database must be postgres, mysql, mongodb or sqlite")
	}
	nameTitle := "Database name"
	if c.Database.Type == "sqlite" {
		nameTitle = "SQLite database file"
	}
	c.Database.Database, err = get("database-name", nameTitle, "", nil)
	if err != nil {
		return result, err
	}
	if c.Database.Type != "sqlite" {
		c.Database.Host, err = get("host", "Host", c.Database.Host, nil)
		if err != nil {
			return result, err
		}
		port, e := get("port", "Port", strconv.Itoa(config.DefaultPort(c.Database.Type)), nil)
		if e != nil {
			return result, e
		}
		c.Database.Port, err = strconv.Atoi(port)
		if err != nil {
			return result, fmt.Errorf("--port must be an integer between 1 and 65535")
		}
		c.Database.User, err = get("user", "Username", "", nil)
		if err != nil {
			return result, err
		}
		c.Database.PasswordEnv, err = get("password-env", "Password environment variable (not its value)", "DBVAULT_DB_PASSWORD", nil)
		if err != nil {
			return result, err
		}
		if !envName.MatchString(c.Database.PasswordEnv) {
			return result, fmt.Errorf("--password-env must be an environment variable name, not a password")
		}
		modes := []Choice{{"Prefer TLS", "prefer"}, {"Require TLS", "require"}, {"Verify TLS certificate and hostname", "verify-full"}, {"Disable TLS", "disable"}}
		if c.Database.Type == "mongodb" {
			c.Database.SSLMode = "require"
			modes = modes[1:]
		}
		c.Database.SSLMode, err = get("ssl-mode", "TLS mode", c.Database.SSLMode, modes)
		if err != nil {
			return result, err
		}
		if c.Database.Type == "mongodb" {
			c.Database.AuthDatabase, err = get("auth-database", "Authentication database", "admin", nil)
			if err != nil {
				return result, err
			}
			if value, ok := o.Values["quiesced"]; ok {
				c.Database.Options.Quiesced, err = strconv.ParseBool(value)
			} else if p != nil {
				c.Database.Options.Quiesced, err = p.Confirm(ctx, "Will you stop application writes for the entire MongoDB backup?", false)
			}
			if err != nil {
				return result, err
			}
			if !c.Database.Options.Quiesced && p != nil {
				p.Message("MongoDB backups require stopped writes and options.quiesced: true. Setup does not stop writes for you.")
			}
		}
	} else {
		c.Database.Host = ""
		c.Database.SSLMode = ""
	}
	c.Storage.Type, err = get("storage", "Where should backups be stored?", c.Storage.Type, []Choice{{"Local filesystem", "local"}, {"Amazon S3", "s3"}, {"Google Cloud Storage", "gcs"}, {"Azure Blob Storage", "azure"}})
	if err != nil {
		return result, err
	}
	switch c.Storage.Type {
	case "local":
		c.Storage.Local.Path, err = get("output-dir", "Backup directory", "./backups", nil)
		if err == nil {
			if strings.ContainsAny(c.Storage.Local.Path, "\x00\r\n") {
				return result, fmt.Errorf("backup directory contains control characters")
			}
			if stat, e := os.Stat(c.Storage.Local.Path); e == nil && !stat.IsDir() {
				return result, fmt.Errorf("backup directory points to a file")
			} else if e != nil && !os.IsNotExist(e) {
				return result, e
			}
		}
	case "s3", "gcs", "azure":
		if p != nil {
			messages := map[string]string{"s3": "Credentials use the default AWS credential chain.", "gcs": "Credentials use Google Application Default Credentials.", "azure": "Credentials use Azure DefaultAzureCredential."}
			p.Message(messages[c.Storage.Type] + " The bucket/container must already exist; setup does not test storage permissions.")
		}
		if c.Storage.Type == "azure" {
			c.Storage.Azure.Container, err = get("container", "Azure container", "", nil)
			if err != nil {
				return result, err
			}
			c.Storage.Azure.AccountName, err = get("account-name", "Azure account name", "", nil)
		} else {
			bucket, e := get("bucket", "Bucket", "", nil)
			if e != nil {
				return result, e
			}
			if c.Storage.Type == "s3" {
				c.Storage.S3.Bucket = bucket
				c.Storage.S3.Region, err = get("region", "AWS region", "", nil)
			} else {
				c.Storage.GCS.Bucket = bucket
			}
		}
		if err != nil {
			return result, err
		}
		prefix, e := get("prefix", "Object prefix (optional)", "", nil)
		if e != nil {
			return result, e
		}
		switch c.Storage.Type {
		case "s3":
			c.Storage.S3.Prefix = prefix
		case "gcs":
			c.Storage.GCS.Prefix = prefix
		case "azure":
			c.Storage.Azure.Prefix = prefix
		}
	default:
		return result, fmt.Errorf("--storage must be local, s3, gcs or azure")
	}
	if err != nil {
		return result, err
	}
	c.Compression.Type, err = get("compression", "Compression (gzip is the default)", c.Compression.Type, []Choice{{"gzip (default)", "gzip"}, {"zstd", "zstd"}, {"none", "none"}})
	if err != nil {
		return result, err
	}
	if p != nil && !o.PathProvided {
		o.Path, err = p.Input(ctx, "Configuration file", o.Path)
		if err != nil {
			return result, err
		}
	}
	if o.Path == "" {
		return result, fmt.Errorf("--config must not be empty")
	}
	o.Path, err = filepath.Abs(o.Path)
	if err != nil {
		return result, err
	}
	if err = c.Validate(); err != nil {
		return result, err
	}
	if err = applicableFlags(o.Values, c); err != nil {
		return result, err
	}
	if stat, e := os.Lstat(o.Path); e == nil {
		if !stat.Mode().IsRegular() {
			return result, fmt.Errorf("configuration destination must be a regular file, not a directory or symlink")
		}
		if !o.Force {
			if p == nil {
				return result, fmt.Errorf("configuration already exists; choose another --config path or use --force")
			}
			for !o.Force {
				choice, e := p.Select(ctx, "Configuration already exists", []Choice{{"Cancel", "cancel"}, {"Review existing configuration", "review"}, {"Overwrite configuration", "overwrite"}}, "cancel")
				if e != nil {
					return result, e
				}
				switch choice {
				case "review":
					existing, e := config.Load(o.Path, config.Overrides{})
					if e != nil {
						p.Message("Existing configuration could not be validated. Its contents will not be displayed.")
					} else {
						p.Message(Summary(existing, o.Path))
					}
				case "overwrite":
					o.Force = true
				default:
					return result, context.Canceled
				}
			}
		}
	} else if !os.IsNotExist(e) {
		return result, e
	}
	if p != nil {
		p.Message(Summary(c, o.Path))
		ok, e := p.Confirm(ctx, "Create this configuration?", true)
		if e != nil {
			return result, e
		}
		if !ok {
			return result, context.Canceled
		}
		if !o.TestProvided {
			o.Test, err = p.Confirm(ctx, "Test database connection and native tools now?", false)
			if err != nil {
				return result, err
			}
		}
	}
	if o.Test {
		password := os.Getenv(c.Database.PasswordEnv)
		if c.Database.Type != "sqlite" && password == "" {
			if p == nil {
				return result, fmt.Errorf("set the password environment variable before using --test")
			}
			password, err = p.Secret(ctx, "Password for this connection test only (not saved)")
			if err != nil {
				return result, err
			}
			if password == "" {
				return result, fmt.Errorf("a nonempty password is required for the connection test; set the referenced environment variable or skip testing")
			}
		}
		if o.TestConnection == nil {
			return result, fmt.Errorf("connection tester is unavailable")
		}
		err = o.TestConnection(ctx, c, password)
		err = security.New(password).Error(err)
		password = ""
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			if p == nil {
				return result, err
			}
			p.Message("Connection test failed: " + err.Error())
			ok, e := p.Confirm(ctx, "Create configuration anyway?", false)
			if e != nil {
				return result, e
			}
			if !ok {
				return result, context.Canceled
			}
		} else {
			result.Tested = true
		}
	}
	if err = config.Write(ctx, o.Path, c, o.Force); err != nil {
		return result, err
	}
	result.Path, result.Database, result.Storage, result.PasswordEnv = o.Path, c.Database.Type, c.Storage.Type, c.Database.PasswordEnv
	result.DatabaseName, result.Compression = c.Database.Database, c.Compression.Type
	switch c.Storage.Type {
	case "local":
		result.StorageLocation = c.Storage.Local.Path
	case "s3":
		result.StorageLocation = c.Storage.S3.Bucket + "/" + c.Storage.S3.Prefix
	case "gcs":
		result.StorageLocation = c.Storage.GCS.Bucket + "/" + c.Storage.GCS.Prefix
	case "azure":
		result.StorageLocation = c.Storage.Azure.AccountName + "/" + c.Storage.Azure.Container + "/" + c.Storage.Azure.Prefix
	}
	return result, nil
}

func applicableFlags(values map[string]string, c config.Config) error {
	for _, key := range []string{"host", "port", "user", "password-env", "ssl-mode"} {
		if _, supplied := values[key]; supplied && c.Database.Type == "sqlite" {
			return fmt.Errorf("--%s does not apply to SQLite", key)
		}
	}
	for _, key := range []string{"auth-database", "quiesced"} {
		if _, supplied := values[key]; supplied && c.Database.Type != "mongodb" {
			return fmt.Errorf("--%s requires MongoDB", key)
		}
	}
	for _, key := range []string{"output-dir", "bucket", "region", "container", "account-name", "prefix"} {
		_, supplied := values[key]
		allowed := (key == "output-dir" && c.Storage.Type == "local") || (key == "bucket" && (c.Storage.Type == "s3" || c.Storage.Type == "gcs")) || (key == "region" && c.Storage.Type == "s3") || ((key == "container" || key == "account-name") && c.Storage.Type == "azure") || (key == "prefix" && c.Storage.Type != "local")
		if supplied && !allowed {
			return fmt.Errorf("--%s does not apply to the selected storage backend", key)
		}
	}
	return nil
}

// Summary never resolves a secret or dumps arbitrary YAML fields.
func Summary(c config.Config, path string) string {
	s := fmt.Sprintf("Configuration Summary\n  Database: %s\n  Database name/file: %s\n", c.Database.Type, c.Database.Database)
	if c.Database.Type != "sqlite" {
		s += fmt.Sprintf("  Host: %s\n  Port: %d\n  Username: %s\n  Password: environment variable %s\n", c.Database.Host, c.Database.Port, c.Database.User, c.Database.PasswordEnv)
	}
	s += fmt.Sprintf("  Storage: %s\n", c.Storage.Type)
	switch c.Storage.Type {
	case "local":
		s += fmt.Sprintf("  Directory: %s\n", c.Storage.Local.Path)
	case "s3":
		s += fmt.Sprintf("  Bucket: %s\n  Region: %s\n  Prefix: %s\n", c.Storage.S3.Bucket, c.Storage.S3.Region, c.Storage.S3.Prefix)
	case "gcs":
		s += fmt.Sprintf("  Bucket: %s\n  Prefix: %s\n", c.Storage.GCS.Bucket, c.Storage.GCS.Prefix)
	case "azure":
		s += fmt.Sprintf("  Account: %s\n  Container: %s\n  Prefix: %s\n", c.Storage.Azure.AccountName, c.Storage.Azure.Container, c.Storage.Azure.Prefix)
	}
	return s + fmt.Sprintf("  Backup type: Full\n  Compression: %s\n  Config: %s", c.Compression.Type, path)
}
