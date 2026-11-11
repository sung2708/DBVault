package config

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden configuration files")

// TestMarshalGolden pins the exact bytes written by `dbvault init` and other
// configuration writers so YAML library changes cannot silently reformat
// operator files.
func TestMarshalGolden(t *testing.T) {
	postgres := Defaults()
	postgres.Version = "1"
	postgres.Database.Type = "postgres"
	postgres.Database.Host = "db.internal"
	postgres.Database.Port = 5432
	postgres.Database.User = "backup"
	postgres.Database.PasswordEnv = "PGPASSWORD"
	postgres.Database.Database = "orders: prod"
	postgres.Database.SSLMode = "require"
	postgres.Database.Options.IncludeTables = []string{"public.orders", "public.items"}
	postgres.Database.Tools = map[string]string{"pg_dump": "/usr/lib/postgresql/16/bin/pg_dump", "pg_restore": "/usr/lib/postgresql/16/bin/pg_restore"}
	postgres.Storage.Type = "s3"
	postgres.Storage.S3 = S3{Bucket: "backups", Region: "eu-west-1", Prefix: "prod/orders", AccessKeyEnv: "AWS_ACCESS_KEY_ID", SecretKeyEnv: "AWS_SECRET_ACCESS_KEY", Encryption: "aws:kms", KMSKeyID: "alias/dbvault"}
	postgres.Compression = Compression{"zstd", 3}
	postgres.Retention = Retention{KeepDays: 30, KeepCount: 10}
	postgres.Health = Health{MaxBackupAge: "26h"}
	postgres.Encryption = &Encryption{KeyID: "primary", Keys: map[string]string{"primary": "DBVAULT_KEY_PRIMARY", "old": "DBVAULT_KEY_OLD"}}
	postgres.Protection = &Protection{VerifyAfterBackup: true}
	postgres.Notifications.Slack = Slack{Enabled: true, WebhookEnv: "SLACK_WEBHOOK_URL", Channel: "#ops"}

	for name, c := range map[string]Config{"sqlite-local": initConfig(), "postgres-s3": postgres} {
		t.Run(name, func(t *testing.T) {
			got, err := Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".golden.yaml")
			if *updateGolden {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("configuration bytes changed\n--- got\n%s\n--- want\n%s", got, want)
			}
			if _, err := Load(writeTemp(t, got), Overrides{}); err != nil {
				t.Fatal("golden configuration does not load:", err)
			}
		})
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
