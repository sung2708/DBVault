//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	sqliteadapter "github.com/sung2708/DBVault/internal/database/sqlite"
	"github.com/sung2708/DBVault/internal/storage/local"
)

func TestManagedKeysDocker(t *testing.T) {
	for _, kind := range []string{"aws-kms", "vault-transit"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			p := config.ManagedKey{Type: kind, Region: "us-east-1", Key: "backup", TokenEnv: "VAULT_INTEGRATION_TOKEN"}
			var rotate func()
			if kind == "aws-kms" {
				endpoint, _ := containerEndpoint(t, ctx, "localstack/localstack:4.7.0", 4566, map[string]string{"SERVICES": "kms"})
				p.Endpoint = endpoint
				t.Setenv("AWS_ACCESS_KEY_ID", "test")
				t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
				t.Setenv("AWS_SESSION_TOKEN", "")
				t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
				c, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
				if e != nil {
					t.Fatal(e)
				}
				client := kms.NewFromConfig(c, func(o *kms.Options) { o.BaseEndpoint = aws.String(endpoint) })
				waitReady(t, ctx, func() error {
					result, e := client.CreateKey(ctx, &kms.CreateKeyInput{})
					if e == nil {
						p.Key = aws.ToString(result.KeyMetadata.Arn)
					}
					return e
				})
				rotate = func() {
					if _, e := client.EnableKeyRotation(ctx, &kms.EnableKeyRotationInput{KeyId: aws.String(p.Key)}); e != nil {
						t.Fatal(e)
					}
				}
			} else {
				token := "dbvault-vault-test-token"
				t.Setenv("VAULT_INTEGRATION_TOKEN", token)
				endpoint, _ := containerEndpoint(t, ctx, "hashicorp/vault:1.21", 8200, map[string]string{"VAULT_DEV_ROOT_TOKEN_ID": token, "VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200"}, "server", "-dev")
				p.Endpoint = endpoint
				post := func(path string, body any) error {
					data, _ := json.Marshal(body)
					req, e := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/"+path, bytes.NewReader(data))
					if e != nil {
						return e
					}
					req.Header.Set("X-Vault-Token", token)
					req.Header.Set("Content-Type", "application/json")
					resp, e := http.DefaultClient.Do(req)
					if e != nil {
						return e
					}
					defer resp.Body.Close()
					if resp.StatusCode < 200 || resp.StatusCode >= 300 {
						return fmt.Errorf("Vault setup HTTP %d", resp.StatusCode)
					}
					return nil
				}
				waitReady(t, ctx, func() error { return post("sys/mounts/transit", map[string]string{"type": "transit"}) })
				if e := post("transit/keys/backup", map[string]string{"type": "aes256-gcm96"}); e != nil {
					t.Fatal(e)
				}
				rotate = func() {
					if e := post("transit/keys/backup/rotate", map[string]string{}); e != nil {
						t.Fatal(e)
					}
				}
			}
			file := filepath.Join(t.TempDir(), "source.sqlite")
			db, e := sql.Open("sqlite", file)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec("CREATE TABLE items(id INTEGER PRIMARY KEY); INSERT INTO items VALUES(1)"); e != nil {
				t.Fatal(e)
			}
			db.Close()
			cfg := config.Defaults()
			cfg.Version = "1"
			cfg.Database = config.Database{Type: "sqlite", Database: file}
			cfg.Encryption = &config.Encryption{KeyID: "remote", Providers: map[string]config.ManagedKey{"remote": p}}
			store, e := local.New(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer store.Close()
			svc := &app.Service{Config: cfg, Store: store, DB: &sqliteadapter.Adapter{Config: cfg.Database}}
			full, e := svc.Backup(ctx, "full", false)
			if e != nil {
				t.Fatal(e)
			}
			if full.Encryption.Algorithm != "aes256-gcm-stream-v2" {
				t.Fatal("managed provider did not use v2")
			}
			rotate()
			db, e = sql.Open("sqlite", file)
			if e != nil {
				t.Fatal(e)
			}
			db.Exec("INSERT INTO items VALUES(2)")
			db.Close()
			increment, e := svc.Backup(ctx, "incremental", false)
			if e != nil {
				t.Fatal(e)
			}
			db, e = sql.Open("sqlite", file)
			if e != nil {
				t.Fatal(e)
			}
			db.Exec("DELETE FROM items")
			db.Close()
			if e = svc.Restore(ctx, increment.Name, true, false, database.RestoreOptions{}); e != nil {
				t.Fatal(e)
			}
			db, e = sql.Open("sqlite", file)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			var count int
			if e = db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); e != nil || count != 2 {
				t.Fatal("managed key chain restore", count, e)
			}
		})
	}
}
