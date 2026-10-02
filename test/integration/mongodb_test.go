//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/mongodb"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/storage/local"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type mongoContainerRunner struct{ containerRunner }

func (r mongoContainerRunner) Run(ctx context.Context, s runner.Spec) error {
	args := append([]string(nil), s.Args...)
	for i, arg := range args {
		if strings.HasPrefix(arg, "--port=") {
			args[i] = "--port=27017"
		}
		if strings.HasPrefix(arg, "--config=") {
			source := strings.TrimPrefix(arg, "--config=")
			target := fmt.Sprintf("/tmp/dbvault-credentials-%d.yaml", time.Now().UnixNano())
			if e := r.native.Run(ctx, runner.Spec{Executable: "docker", Args: []string{"cp", source, r.container + ":" + target}, Stdout: io.Discard}); e != nil {
				return e
			}
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				r.native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"exec", r.container, "rm", "-f", target}, Stdout: io.Discard})
			}()
			args[i] = "--config=" + target
		}
	}
	s.Args = args
	return r.containerRunner.Run(ctx, s)
}
func TestMongoDBBackupDestroyRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	secret := "development-only-mongo-password"
	endpoint, r := containerEndpoint(t, ctx, "mongo:8.0", 27017, map[string]string{"MONGO_INITDB_ROOT_USERNAME": "dbvault", "MONGO_INITDB_ROOT_PASSWORD": secret})
	u, _ := url.Parse(endpoint)
	host, portText, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portText)
	cfg := config.Defaults()
	cfg.Database = config.Database{Type: "mongodb", Host: host, Port: port, User: "dbvault", Database: "source", AuthDatabase: "admin", SSLMode: "disable", Options: config.Options{Quiesced: true}}
	adapter := &mongodb.Adapter{Config: cfg.Database, Password: secret, Runner: mongoContainerRunner{r}}
	waitReady(t, ctx, func() error { _, e := adapter.Preflight(ctx); return e })
	client, e := mongo.Connect(options.Client().SetHosts([]string{u.Host}).SetAuth(options.Credential{Username: "dbvault", Password: secret, AuthSource: "admin"}))
	if e != nil {
		t.Fatal(e)
	}
	defer client.Disconnect(context.Background())
	docs := []any{}
	for i := 0; i < 1000; i++ {
		docs = append(docs, bson.D{{Key: "_id", Value: i}, {Key: "value", Value: fmt.Sprintf("Việt Nam_%d", i)}})
	}
	if _, e = client.Database("source").Collection("records").InsertMany(ctx, docs); e != nil {
		t.Fatal(e)
	}
	compare := func(db string) {
		t.Helper()
		cursor, e := client.Database(db).Collection("records").Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
		if e != nil {
			t.Fatal(e)
		}
		var got []struct {
			ID    int    `bson:"_id"`
			Value string `bson:"value"`
		}
		if e = cursor.All(ctx, &got); e != nil {
			t.Fatal(e)
		}
		if len(got) != 1000 {
			t.Fatal("wrong count", len(got))
		}
		for i, v := range got {
			if v.ID != i || v.Value != fmt.Sprintf("Việt Nam_%d", i) {
				t.Fatal("dataset mismatch", i)
			}
		}
	}
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			p, e := local.New(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			cfg.Compression.Type = codec
			svc := &app.Service{Config: cfg, DB: adapter, Store: p, Version: "integration"}
			m, e := svc.Backup(ctx, "full", false)
			if e != nil {
				t.Fatal(e)
			}
			assertBackupHealth(t, ctx, svc, m)
			if codec == "none" {
				assertNewRestoreWorkflow(t, ctx, svc, m)
			}
			if e = client.Database("source").Drop(ctx); e != nil {
				t.Fatal(e)
			}
			if e = svc.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); e != nil {
				t.Fatal(e)
			}
			compare("source")
			adapter.Config.Database = "target"
			svc.Config.Database.Database = "target"
			if e = svc.Restore(ctx, m.Name, true, false, database.RestoreOptions{Collections: []string{"records"}, Clean: true}); e != nil {
				t.Fatal(e)
			}
			compare("target")
			adapter.Config.Database = "source"
		})
	}
	t.Run("selected-backup", func(t *testing.T) {
		if _, e := client.Database("source").Collection("excluded").InsertOne(ctx, bson.D{{Key: "_id", Value: 42}}); e != nil {
			t.Fatal(e)
		}
		cfg.Database.Options.IncludeCollections = []string{"records"}
		adapter.Config = cfg.Database
		cfg.Compression.Type = "gzip"
		p, e := local.New(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		svc := &app.Service{Config: cfg, DB: adapter, Store: p, Version: "integration"}
		m, e := svc.Backup(ctx, "full", false)
		if e != nil {
			t.Fatal(e)
		}
		if e = client.Database("source").Drop(ctx); e != nil {
			t.Fatal(e)
		}
		if e = svc.Restore(ctx, m.Name, true, false, database.RestoreOptions{}); e != nil {
			t.Fatal(e)
		}
		compare("source")
		if n, e := client.Database("source").Collection("excluded").CountDocuments(ctx, bson.D{}); e != nil || n != 0 {
			t.Fatal("excluded collection restored", n, e)
		}
	})
	var b bytes.Buffer
	if e = r.Run(ctx, runner.Spec{Executable: "find", Args: []string{"/tmp", "-name", "dbvault-credentials-*"}, Stdout: &b}); e != nil || strings.TrimSpace(b.String()) != "" {
		t.Fatal("credential file leak", b.String(), e)
	}
}
