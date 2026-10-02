//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	sqliteadapter "github.com/sung2708/DBVault/internal/database/sqlite"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/storage"
	azureprovider "github.com/sung2708/DBVault/internal/storage/azure"
	gcsprovider "github.com/sung2708/DBVault/internal/storage/gcs"
	"github.com/sung2708/DBVault/internal/storage/providers"
	s3provider "github.com/sung2708/DBVault/internal/storage/s3"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func containerEndpoint(t *testing.T, ctx context.Context, image string, port int, env map[string]string, args ...string) (string, containerRunner) {
	t.Helper()
	name := fmt.Sprintf("dbvault-integration-%d", time.Now().UnixNano())
	native := runner.Native{}
	command := []string{"run", "--detach", "--name", name, "-p", fmt.Sprintf("127.0.0.1::%d", port)}
	for k := range env {
		command = append(command, "--env", k)
	}
	command = append(command, image)
	command = append(command, args...)
	if e := native.Run(ctx, runner.Spec{Executable: "docker", Args: command, Env: env, Stdout: io.Discard}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if t.Failed() {
			var diagnostics bytes.Buffer
			native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"logs", "--tail", "30", name}, Stdout: &diagnostics})
			t.Log(diagnostics.String())
		}
		if e := native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"rm", "--force", name}, Stdout: io.Discard}); e != nil {
			t.Error(e)
		}
	})
	var b bytes.Buffer
	if e := native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"port", name, strconv.Itoa(port) + "/tcp"}, Stdout: &b}); e != nil {
		t.Fatal(e)
	}
	address := strings.TrimSpace(b.String())
	if _, _, e := net.SplitHostPort(address); e != nil {
		t.Fatal(address, e)
	}
	return "http://" + address, containerRunner{native: native, container: name}
}
func waitReady(t *testing.T, ctx context.Context, fn func() error) {
	t.Helper()
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		if e := fn(); e == nil {
			return
		} else {
			last = e
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatal("emulator readiness", last)
		case <-ticker.C:
		}
	}
}
func TestCloudProvidersAndSQLiteRestore(t *testing.T) {
	for _, kind := range []string{"s3", "gcs", "azure"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			cfg := config.Defaults()
			cfg.Storage.Type = kind
			t.Setenv("TEST_CLOUD_ACCESS", "dbvaultdev")
			t.Setenv("TEST_CLOUD_SECRET", "development-only-test-secret")
			switch kind {
			case "s3":
				endpoint, _ := containerEndpoint(t, ctx, "localstack/localstack:4.7.0", 4566, map[string]string{"SERVICES": "s3"})
				cfg.Storage.S3 = config.S3{Bucket: "dbvault-test", Region: "us-east-1", Prefix: "tests/scoped", Endpoint: endpoint, AccessKeyEnv: "TEST_CLOUD_ACCESS", SecretKeyEnv: "TEST_CLOUD_SECRET"}
			case "gcs":
				endpoint, _ := containerEndpoint(t, ctx, "fsouza/fake-gcs-server:1.56.1", 4443, nil, "-scheme", "http")
				t.Setenv("STORAGE_EMULATOR_HOST", endpoint)
				cfg.Storage.GCS = config.GCS{Bucket: "dbvault-test", Prefix: "tests/scoped"}
			case "azure":
				key := base64.StdEncoding.EncodeToString([]byte("development-only-azurite-account-key"))
				t.Setenv("TEST_AZURE_KEY", key)
				endpoint, _ := containerEndpoint(t, ctx, "mcr.microsoft.com/azure-storage/azurite:3.35.0", 10000, map[string]string{"AZURITE_ACCOUNTS": "dbvaultdev:" + key}, "azurite-blob", "--blobHost", "0.0.0.0", "--skipApiVersionCheck")
				cfg.Storage.Azure = config.Azure{Container: "dbvault-test", AccountName: "dbvaultdev", AccountKeyEnv: "TEST_AZURE_KEY", Endpoint: endpoint + "/dbvaultdev", Prefix: "tests/scoped"}
			}
			provider, closeFn, e := providers.Open(ctx, cfg.Storage)
			if e != nil {
				t.Fatal(e)
			}
			defer closeFn()
			switch p := provider.(type) {
			case *s3provider.Provider:
				p.PartSize = 5 << 20
				waitReady(t, ctx, func() error {
					_, e := p.Client.CreateBucket(ctx, &sdk.CreateBucketInput{Bucket: aws.String(p.Bucket)})
					return e
				})
			case *gcsprovider.Provider:
				waitReady(t, ctx, func() error { return p.Client.Bucket(p.Bucket).Create(ctx, "test-project", nil) })
			case *azureprovider.Provider:
				waitReady(t, ctx, func() error { _, e := p.Client.CreateContainer(ctx, p.Container, nil); return e })
			}
			lifecycle(t, ctx, provider)
			path := filepath.Join(t.TempDir(), "source.sqlite")
			db, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			if _, e = db.Exec("CREATE TABLE records(id INTEGER PRIMARY KEY,value TEXT); INSERT INTO records VALUES(1,'cloud Việt Nam')"); e != nil {
				t.Fatal(e)
			}
			cfg.Database = config.Database{Type: "sqlite", Database: path}
			cfg.Protection = &config.Protection{VerifyAfterBackup: true}
			adapter := &sqliteadapter.Adapter{Config: cfg.Database}
			svc := &app.Service{Config: cfg, DB: adapter, Store: provider, Version: "integration"}
			for _, codec := range []string{"none", "gzip", "zstd"} {
				cfg.Compression.Type = codec
				svc.Config = cfg
				m, e := svc.Backup(ctx, "full", false)
				if e != nil {
					t.Fatal(e)
				}
				verificationHealth, readErr := svc.Health(ctx, false)
				if readErr != nil || verificationHealth.Databases[0].Integrity != "verified" {
					t.Fatal("cloud post-upload verification evidence", verificationHealth, readErr)
				}
				assertBackupHealth(t, ctx, svc, m)
				if codec == "none" {
					assertCloudRestoreOperations(t, ctx, svc, m)
				}
				drillTarget := filepath.Join(t.TempDir(), "cloud-recovery.sqlite")
				drill, err := svc.RecoveryDrill(ctx, app.DrillOptions{Target: m.Name, RecoveryDatabase: drillTarget, Confirm: true, Cleanup: true})
				if err != nil || drill.Status != "passed" || drill.TargetState != "removed" {
					t.Fatal("cloud recovery drill", drill, err)
				}
				savedNow := svc.Now
				svc.Now = func() time.Time { return drill.CompletedAt.Add(time.Second) }
				health, err := svc.Health(ctx, false)
				svc.Now = savedNow
				if err != nil || health.Databases[0].RestoreTest != "passed" || health.Databases[0].RecoveryRecord != drill.RecordKey || health.Databases[0].Integrity != "verified" {
					t.Fatal("cloud drill health evidence", health, err)
				}
				if _, e = db.Exec("DELETE FROM records"); e != nil {
					t.Fatal(e)
				}
				if e = svc.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); e != nil {
					t.Fatal(e)
				}
				var got string
				if e = db.QueryRow("SELECT value FROM records WHERE id=1").Scan(&got); e != nil || got != "cloud Việt Nam" {
					t.Fatal(got, e)
				}
				if e = svc.Delete(ctx, m.Name, true, false); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
func lifecycle(t *testing.T, ctx context.Context, p storage.Provider) {
	t.Helper()
	data := bytes.Repeat([]byte("cloudfixture"), 900000)
	key := "large.archive"
	if e := p.Put(ctx, key, bytes.NewReader(data), -1); e != nil {
		t.Fatal(e)
	}
	if e := p.Put(ctx, key, bytes.NewReader(data), -1); e == nil {
		t.Fatal("overwrite accepted")
	}
	r, e := p.Get(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(r)
	r.Close()
	if e != nil || !bytes.Equal(data, got) {
		t.Fatal("roundtrip mismatch", e)
	}
	exists, e := p.Exists(ctx, key)
	if e != nil || !exists {
		t.Fatal(exists, e)
	}
	list, e := p.List(ctx, "large")
	if e != nil || len(list) != 1 || list[0].Key != key || list[0].Size != int64(len(data)) {
		t.Fatal(list, e)
	}
	if e = p.Put(ctx, "../escape", bytes.NewReader(data), -1); e == nil {
		t.Fatal("traversal accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if e = p.Put(canceled, "canceled.archive", bytes.NewReader(data), -1); e == nil {
		t.Fatal("cancellation ignored")
	}
	if e = p.Delete(ctx, key); e != nil {
		t.Fatal(e)
	}
	exists, e = p.Exists(ctx, key)
	if e != nil || exists {
		t.Fatal(exists, e)
	}
}
