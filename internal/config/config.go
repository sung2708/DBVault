package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/storage"
	"gopkg.in/yaml.v3"
)

type Options struct {
	IncludeTables      []string `yaml:"include_tables"`
	ExcludeTables      []string `yaml:"exclude_tables"`
	ExtraFlags         []string `yaml:"extra_flags"`
	IncludeCollections []string `yaml:"include_collections"`
	ExcludeCollections []string `yaml:"exclude_collections"`
	Quiesced           bool     `yaml:"quiesced"`
}
type Database struct {
	Type         string            `yaml:"type"`
	Host         string            `yaml:"host"`
	Port         int               `yaml:"port"`
	User         string            `yaml:"user"`
	PasswordEnv  string            `yaml:"password_env"`
	Database     string            `yaml:"database"`
	SSLMode      string            `yaml:"ssl_mode"`
	AuthDatabase string            `yaml:"auth_database"`
	Options      Options           `yaml:"options"`
	Tools        map[string]string `yaml:"tools,omitempty"`
}
type Local struct {
	Path        string `yaml:"path"`
	Permissions string `yaml:"permissions"`
}
type Storage struct {
	Type  string `yaml:"type"`
	Local Local  `yaml:"local"`
	S3    S3     `yaml:"s3"`
	GCS   GCS    `yaml:"gcs"`
	Azure Azure  `yaml:"azure"`
}
type S3 struct {
	Bucket       string `yaml:"bucket"`
	Region       string `yaml:"region"`
	Prefix       string `yaml:"prefix"`
	Endpoint     string `yaml:"endpoint"`
	AccessKeyEnv string `yaml:"access_key_env"`
	SecretKeyEnv string `yaml:"secret_key_env"`
	Encryption   string `yaml:"encryption"`
	KMSKeyID     string `yaml:"kms_key_id"`
}
type GCS struct {
	Bucket             string `yaml:"bucket"`
	Prefix             string `yaml:"prefix"`
	CredentialsFileEnv string `yaml:"credentials_file_env"`
	Endpoint           string `yaml:"endpoint"`
}
type Azure struct {
	Container     string `yaml:"container"`
	AccountName   string `yaml:"account_name"`
	AccountKeyEnv string `yaml:"account_key_env"`
	Prefix        string `yaml:"prefix"`
	Endpoint      string `yaml:"endpoint"`
}
type Compression struct {
	Type  string `yaml:"type"`
	Level int    `yaml:"level"`
}
type Retention struct {
	KeepDays  int `yaml:"keep_days"`
	KeepCount int `yaml:"keep_count"`
}
type Health struct {
	MaxBackupAge   string `yaml:"max_backup_age,omitempty"`
	BackupScope    string `yaml:"backup_scope,omitempty"`
	SourceIdentity string `yaml:"source_identity,omitempty"`
}
type Protection struct {
	VerifyAfterBackup bool `yaml:"verify_after_backup,omitempty"`
}
type Slack struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookEnv string `yaml:"webhook_url_env"`
	Channel    string `yaml:"channel"`
}
type Encryption struct {
	KeyID     string                `yaml:"key_id"`
	Keys      map[string]string     `yaml:"keys"`
	Providers map[string]ManagedKey `yaml:"providers,omitempty"`
}

type ManagedKey struct {
	Type      string `yaml:"type"`
	Key       string `yaml:"key"`
	Region    string `yaml:"region,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty"`
	TokenEnv  string `yaml:"token_env,omitempty"`
	Namespace string `yaml:"namespace,omitempty"`
	Mount     string `yaml:"mount,omitempty"`
}

type Config struct {
	PITR    *PITR `yaml:"pitr,omitempty"`
	Metrics struct {
		RecordOperations bool `yaml:"record_operations"`
	} `yaml:"metrics,omitempty"`
	Encryption    *Encryption `yaml:"encryption,omitempty"`
	Version       string      `yaml:"version"`
	Database      Database    `yaml:"database"`
	Storage       Storage     `yaml:"storage"`
	Compression   Compression `yaml:"compression"`
	Retention     Retention   `yaml:"retention"`
	Health        Health      `yaml:"health,omitempty"`
	Protection    *Protection `yaml:"protection,omitempty"`
	Notifications struct {
		Slack Slack `yaml:"slack"`
	} `yaml:"notifications"`
}

type PITR struct {
	ArchiveDirectory string `yaml:"archive_directory,omitempty"`
	Quiesced         bool   `yaml:"quiesced,omitempty"`
}
type Overrides struct {
	Database, OutputDir, Compression *string
	KeepDays, KeepCount              *int
}

func Defaults() Config {
	return Config{Storage: Storage{Type: "local", Local: Local{Permissions: "0700"}}, Database: Database{Host: "127.0.0.1", SSLMode: "prefer"}, Compression: Compression{"gzip", 6}}
}

// DefaultPort is shared by configuration loading and guided setup.
func DefaultPort(engine string) int {
	switch engine {
	case "postgres":
		return 5432
	case "mysql":
		return 3306
	case "mongodb":
		return 27017
	}
	return 0
}
func Load(path string, o Overrides) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if err != nil {
		return c, fault.Wrap(fault.Configuration, "open configuration", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return c, fault.Wrap(fault.Configuration, "inspect configuration", err)
	}
	if !stat.Mode().IsRegular() || stat.Size() > 1<<20 {
		return c, invalid("configuration", "must be a regular file under 1 MiB")
	}
	d := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return c, fault.Wrap(fault.Configuration, "decode YAML", fmt.Errorf("invalid configuration schema (unknown fields, types, or YAML syntax)"))
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return c, fault.Wrap(fault.Configuration, "decode YAML", fmt.Errorf("configuration must contain exactly one YAML document under 1 MiB"))
	}
	env := map[string]*string{"DBVAULT_DB_TYPE": &c.Database.Type, "DBVAULT_DB_HOST": &c.Database.Host, "DBVAULT_DB_USER": &c.Database.User, "DBVAULT_DB_NAME": &c.Database.Database, "DBVAULT_DATABASE_NAME": &c.Database.Database, "DBVAULT_DB_SSLMODE": &c.Database.SSLMode, "DBVAULT_STORAGE_TYPE": &c.Storage.Type, "DBVAULT_STORAGE_LOCAL_PATH": &c.Storage.Local.Path}
	// The documented DBVAULT_DB_NAME wins over the legacy spelling.
	if v, ok := os.LookupEnv("DBVAULT_DATABASE_NAME"); ok {
		c.Database.Database = v
	}
	delete(env, "DBVAULT_DATABASE_NAME")
	for k, p := range env {
		if v, ok := os.LookupEnv(k); ok {
			*p = v
		}
	}
	if s, ok := os.LookupEnv("DBVAULT_DB_PORT"); ok {
		c.Database.Port, err = strconv.Atoi(s)
		if err != nil {
			return c, fault.Wrap(fault.Configuration, "database.port", fmt.Errorf("DBVAULT_DB_PORT must be an integer"))
		}
	}
	if c.Database.Port == 0 {
		c.Database.Port = DefaultPort(c.Database.Type)
	}
	if o.Database != nil {
		c.Database.Database = *o.Database
	}
	if o.OutputDir != nil {
		c.Storage.Local.Path = *o.OutputDir
	}
	if o.Compression != nil {
		c.Compression.Type = *o.Compression
	}
	if o.KeepDays != nil {
		c.Retention.KeepDays = *o.KeepDays
	}
	if o.KeepCount != nil {
		c.Retention.KeepCount = *o.KeepCount
	}
	return c, c.Validate()
}
func invalid(field, reason string) error {
	return fault.Wrap(fault.Configuration, field, fmt.Errorf("%s", reason))
}
func (c Config) Validate() error {
	if c.Encryption != nil {
		if c.Encryption.KeyID == "" || (c.Encryption.Keys[c.Encryption.KeyID] == "" && c.Encryption.Providers[c.Encryption.KeyID].Type == "") {
			return invalid("encryption", "key_id must reference keys or providers")
		}
		for id, env := range c.Encryption.Keys {
			if id == "" || env == "" || len(id) > 128 {
				return invalid("encryption.keys", "key IDs and environment names must be nonempty")
			}
		}
		for id, p := range c.Encryption.Providers {
			if id == "" || len(id) > 128 || c.Encryption.Keys[id] != "" || p.Key == "" || (p.Type != "aws-kms" && p.Type != "vault-transit") {
				return invalid("encryption.providers", "require unique IDs, a key and type aws-kms or vault-transit")
			}
			if p.Type == "vault-transit" && (p.Endpoint == "" || p.TokenEnv == "") {
				return invalid("encryption.providers", "Vault requires endpoint and token_env")
			}
		}
	}
	if c.Health.MaxBackupAge != "" {
		age, err := time.ParseDuration(c.Health.MaxBackupAge)
		if err != nil || age <= 0 {
			return invalid("health.max_backup_age", "must be a positive duration such as 12h or 168h")
		}
	}
	if c.Health.BackupScope != "" && c.Health.BackupScope != "logical" && c.Health.BackupScope != "pitr" {
		return invalid("health.backup_scope", "must be logical or pitr")
	}
	if c.Health.BackupScope == "pitr" {
		if c.Database.Type == "sqlite" {
			return invalid("health.backup_scope", "pitr does not support SQLite")
		}
		if c.Health.SourceIdentity == "" || len(c.Health.SourceIdentity) > 512 || strings.ContainsAny(c.Health.SourceIdentity, "\x00\r\n") {
			return invalid("health.source_identity", "required bounded native source identity for pitr")
		}
	} else if c.Health.SourceIdentity != "" {
		return invalid("health.source_identity", "requires health.backup_scope: pitr")
	}
	if c.Version != "1" {
		return invalid("version", "must be \"1\"")
	}
	switch c.Database.Type {
	case "postgres", "mysql", "mongodb", "sqlite":
	default:
		return invalid("database.type", "must be postgres, mysql, mongodb, or sqlite")
	}
	if c.Database.Database == "" {
		return invalid("database.database", "required")
	}
	if c.Database.Type != "sqlite" {
		if c.Database.User == "" {
			return invalid("database.user", "required")
		}
		if c.Database.PasswordEnv == "" {
			return invalid("database.password_env", "required")
		}
		if c.Database.Host == "" || strings.ContainsAny(c.Database.Host, "=\x00\r\n") || strings.Contains(c.Database.Host, "://") {
			return invalid("database.host", "must be a hostname or IP, not a connection string")
		}
		if c.Database.Port < 1 || c.Database.Port > 65535 {
			return invalid("database.port", "must be between 1 and 65535")
		}
		if strings.ContainsAny(c.Database.Database, "=\x00\r\n") || strings.Contains(c.Database.Database, "://") || strings.HasPrefix(c.Database.Database, "-") {
			return invalid("database.database", "connection strings and option-like database names are forbidden")
		}
	}
	if strings.ContainsAny(c.Database.User, "\x00\r\n") {
		return invalid("database.user", "contains control characters")
	}
	if c.Database.Type == "postgres" {
		switch c.Database.SSLMode {
		case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		default:
			return invalid("database.ssl_mode", "invalid PostgreSQL TLS mode")
		}
	}
	if len(c.Database.Tools) > 0 {
		allowed := map[string]bool{}
		switch c.Database.Type {
		case "postgres":
			allowed = map[string]bool{"pg_dump": true, "pg_restore": true, "psql": true, "pg_basebackup": true}
		case "mysql":
			allowed = map[string]bool{"mysqldump": true, "mysql": true, "mysqlbinlog": true}
		case "mongodb":
			allowed = map[string]bool{"mongodump": true, "mongorestore": true}
		}
		for name, path := range c.Database.Tools {
			if !allowed[name] {
				return invalid("database.tools", "contains a tool that does not apply to the selected engine")
			}
			if path == "" || strings.ContainsAny(path, "\x00\r\n") || strings.TrimSpace(path) != path || strings.HasPrefix(path, "\"") || !filepath.IsAbs(path) {
				return invalid("database.tools."+name, "must be an absolute executable path")
			}
		}
	}
	if c.Database.Type == "mysql" {
		switch c.Database.SSLMode {
		case "disable", "prefer", "require", "verify-ca", "verify-full":
		default:
			return invalid("database.ssl_mode", "invalid MySQL TLS mode")
		}
	}
	if len(c.Database.Options.ExtraFlags) > 0 {
		return invalid("database.options.extra_flags", "arbitrary native flags are unsupported; use explicit supported options")
	}
	if c.Database.Type != "mongodb" && (len(c.Database.Options.IncludeCollections) > 0 || len(c.Database.Options.ExcludeCollections) > 0) {
		return invalid("database.options", "collection filters require MongoDB")
	}
	if c.Database.Type == "mongodb" && (len(c.Database.Options.IncludeTables) > 0 || len(c.Database.Options.ExcludeTables) > 0) {
		return invalid("database.options", "MongoDB uses collection filters rather than tables")
	}
	if c.Database.Type == "mongodb" {
		if c.Database.SSLMode != "disable" && c.Database.SSLMode != "require" && c.Database.SSLMode != "verify-full" {
			return invalid("database.ssl_mode", "MongoDB requires disable, require or verify-full")
		}
		if strings.ContainsAny(c.Database.Database, "/\\.\"$ *?[]") {
			return invalid("database.database", "invalid MongoDB database/namespace name")
		}
		if len(c.Database.Options.IncludeCollections) > 1 {
			return invalid("database.options.include_collections", "one included collection per archive is supported")
		}
		if len(c.Database.Options.IncludeCollections) > 0 && len(c.Database.Options.ExcludeCollections) > 0 {
			return invalid("database.options", "include/exclude collections cannot be combined")
		}
		for _, v := range append(append([]string(nil), c.Database.Options.IncludeCollections...), c.Database.Options.ExcludeCollections...) {
			if v == "" || strings.ContainsAny(v, "*?[]\\\x00\r\n") {
				return invalid("database.options", "collection names must be literal namespaces")
			}
		}
	}
	if c.Database.Type == "sqlite" && (len(c.Database.Options.IncludeTables) > 0 || len(c.Database.Options.ExcludeTables) > 0) {
		return invalid("database.options", "SQLite selective backup is unsupported")
	}
	switch c.Storage.Type {
	case "local":
		if c.Storage.Local.Path == "" {
			return invalid("storage.local.path", "required")
		}
	case "s3":
		if _, err := storage.NewNamespace(c.Storage.S3.Prefix); err != nil {
			return invalid("storage.s3.prefix", err.Error())
		}
		if c.Storage.S3.Bucket == "" || c.Storage.S3.Region == "" {
			return invalid("storage.s3", "bucket and region are required")
		}
		if c.Storage.S3.Encryption != "" && c.Storage.S3.Encryption != "AES256" && c.Storage.S3.Encryption != "aws:kms" {
			return invalid("storage.s3.encryption", "must be AES256 or aws:kms")
		}
		if (c.Storage.S3.AccessKeyEnv == "") != (c.Storage.S3.SecretKeyEnv == "") {
			return invalid("storage.s3", "both custom credential env names must be configured together")
		}
	case "gcs":
		if _, err := storage.NewNamespace(c.Storage.GCS.Prefix); err != nil {
			return invalid("storage.gcs.prefix", err.Error())
		}
		if c.Storage.GCS.Bucket == "" {
			return invalid("storage.gcs.bucket", "required")
		}
	case "azure":
		if _, err := storage.NewNamespace(c.Storage.Azure.Prefix); err != nil {
			return invalid("storage.azure.prefix", err.Error())
		}
		if c.Storage.Azure.Container == "" || c.Storage.Azure.AccountName == "" {
			return invalid("storage.azure", "container and account_name are required")
		}
	default:
		return fault.Wrap(fault.Unsupported, "storage.type", fmt.Errorf("must be local, s3, gcs, or azure"))
	}
	mode, err := strconv.ParseUint(c.Storage.Local.Permissions, 8, 32)
	if err != nil || mode&^0700 != 0 || mode&0700 != 0700 {
		return invalid("storage.local.permissions", "must be 0700")
	}
	switch c.Compression.Type {
	case "none", "gzip", "zstd":
	default:
		return invalid("compression.type", "must be none, gzip, or zstd")
	}
	if c.Compression.Level < 1 || c.Compression.Level > 9 {
		return invalid("compression.level", "must be between 1 and 9")
	}
	if c.Retention.KeepDays < 0 || c.Retention.KeepCount < 0 {
		return invalid("retention", "keep_days and keep_count must be nonnegative")
	}
	if c.Notifications.Slack.Enabled && c.Notifications.Slack.WebhookEnv == "" {
		return invalid("notifications.slack.webhook_url_env", "required when Slack is enabled")
	}
	return nil
}
func (c Config) Password() (string, error) {
	if c.Database.Type == "sqlite" {
		return "", nil
	}
	v := os.Getenv(c.Database.PasswordEnv)
	if v == "" {
		return "", invalid("database.password_env", "referenced environment variable is missing or empty")
	}
	return v, nil
}
