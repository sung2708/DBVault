//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/mongodb"
	"github.com/sung2708/DBVault/internal/database/mysql"
	"github.com/sung2708/DBVault/internal/database/postgres"
	"github.com/sung2708/DBVault/internal/database/sqlite"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/metadata"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func assertCloudRestoreOperations(t *testing.T, ctx context.Context, source *app.Service, m metadata.Manifest) {
	t.Helper()
	work := *source
	work.Config.Database.Database = filepath.Join(t.TempDir(), "cloud-new.sqlite")
	work.DB = &sqlite.Adapter{Config: work.Config.Database}
	result, e := work.RestoreWithResult(ctx, m.Name, true, false, app.RestoreRequest{NewDatabase: true})
	if e != nil || result.Status != "completed" {
		t.Fatal(result, e)
	}
	result, e = work.RestoreWithResult(ctx, m.Name, true, false, app.RestoreRequest{BackupBefore: true})
	if e != nil || result.SafetyBackup == "" {
		t.Fatal(result, e)
	}
	history, e := work.RestoreHistory(ctx)
	if e != nil || len(history) != 2 {
		t.Fatal(history, e)
	}
	exported, e := work.Export(ctx, result.SafetyBackup, filepath.Join(t.TempDir(), "cloud-export.sqlite"), true)
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", exported.File)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var got string
	if e = db.QueryRowContext(ctx, "SELECT value FROM records WHERE id=1").Scan(&got); e != nil || got != "cloud Việt Nam" {
		t.Fatal(got, e)
	}
}

func restoreAdapterFor(t *testing.T, source database.Adapter, cfg config.Database) database.Adapter {
	t.Helper()
	switch a := source.(type) {
	case *postgres.Adapter:
		b := *a
		b.Config = cfg
		return &b
	case *mysql.Adapter:
		b := *a
		b.Config = cfg
		return &b
	case *mongodb.Adapter:
		b := *a
		b.Config = cfg
		return &b
	}
	t.Fatal("unsupported integration adapter")
	return nil
}
func assertNewRestoreWorkflow(t *testing.T, ctx context.Context, source *app.Service, m metadata.Manifest) {
	t.Helper()
	work := *source
	name, e := app.NewDestination(source.Config.Database.Type, source.Config.Database.Database, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	work.Config.Database.Database = name
	work.DB = restoreAdapterFor(t, source.DB, work.Config.Database)
	preview, e := work.RestoreWithResult(ctx, m.Name, false, true, app.RestoreRequest{NewDatabase: true})
	if e != nil || preview.Status != "preview_passed" {
		t.Fatal(preview, e)
	}
	result, e := work.RestoreWithResult(ctx, m.Name, true, false, app.RestoreRequest{NewDatabase: true})
	if e != nil || result.Status != "completed" {
		t.Fatal(result, e)
	}
	assertRestoredRows(t, ctx, work.DB)
	if _, e = work.RestoreWithResult(ctx, m.Name, true, false, app.RestoreRequest{NewDatabase: true}); e == nil {
		t.Fatal("existing database accepted as new")
	}
	markerRows(t, ctx, work.DB, true)
	// Deliberately configure a filtered backup. A safety copy must still include
	// the destination-only marker, rather than inheriting the normal backup filter.
	if work.Config.Database.Type == "mongodb" {
		work.Config.Database.Options.IncludeCollections = []string{"records"}
	} else {
		work.Config.Database.Options.IncludeTables = []string{"items"}
	}
	work.DB = restoreAdapterFor(t, work.DB, work.Config.Database)
	clean := work.Config.Database.Type != "mysql"
	result, e = work.RestoreWithResult(ctx, m.Name, true, false, app.RestoreRequest{BackupBefore: true, Options: database.RestoreOptions{Clean: clean}})
	if e != nil || result.SafetyBackup == "" {
		t.Fatal(result, e)
	}
	assertRestoredRows(t, ctx, work.DB)
	if _, e = work.Verify(ctx, result.SafetyBackup); e != nil {
		t.Fatal(e)
	}
	rollback := work
	rollback.Config.Database.Database, e = app.NewDestination(work.Config.Database.Type, "rollback", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	rollback.DB = restoreAdapterFor(t, work.DB, rollback.Config.Database)
	if _, e = rollback.RestoreWithResult(ctx, result.SafetyBackup, true, false, app.RestoreRequest{NewDatabase: true}); e != nil {
		t.Fatal(e)
	}
	if got := markerRows(t, ctx, rollback.DB, false); got != 1 {
		t.Fatal("full safety backup lost destination-only table/collection", got)
	}
	history, e := work.RestoreHistory(ctx)
	if e != nil || len(history) != 4 {
		t.Fatal(history, e)
	}
}

func markerRows(t *testing.T, ctx context.Context, target database.Adapter, create bool) int64 {
	t.Helper()
	query := "SELECT count(*) FROM safety_marker"
	if create {
		query = "CREATE TABLE safety_marker(id integer PRIMARY KEY); INSERT INTO safety_marker VALUES(42)"
	}
	var b bytes.Buffer
	var e error
	switch a := target.(type) {
	case *postgres.Adapter:
		e = a.Runner.Run(ctx, runner.Spec{Executable: "psql", Args: []string{"--no-psqlrc", "--no-password", "--quiet", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=" + query}, Env: map[string]string{"PGDATABASE": a.Config.Database, "PGUSER": a.Config.User, "PGPASSWORD": a.Password}, UnsetEnv: []string{"PGHOSTADDR"}, Stdout: &b})
	case *mysql.Adapter:
		e = a.Runner.Run(ctx, runner.Spec{Executable: "mysql", Args: []string{"--no-defaults", "--no-login-paths", "--host=127.0.0.1", "--user=root", "--ssl-mode=REQUIRED", "--batch", "--skip-column-names", "--database=" + a.Config.Database, "--execute=" + query}, Env: map[string]string{"MYSQL_PWD": a.Password}, Stdout: &b})
	case *mongodb.Adapter:
		client, err := mongo.Connect(options.Client().SetHosts([]string{net.JoinHostPort(a.Config.Host, strconv.Itoa(a.Config.Port))}).SetAuth(options.Credential{Username: a.Config.User, Password: a.Password, AuthSource: a.Config.AuthDatabase}))
		if err != nil {
			t.Fatal(err)
		}
		defer client.Disconnect(ctx)
		collection := client.Database(a.Config.Database).Collection("safety_marker")
		if create {
			_, e = collection.InsertOne(ctx, bson.D{{Key: "_id", Value: 42}})
		} else {
			n, err := collection.CountDocuments(ctx, bson.D{})
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	if e != nil {
		t.Fatal(e)
	}
	if create {
		return 1
	}
	n, e := strconv.ParseInt(strings.TrimSpace(b.String()), 10, 64)
	if e != nil {
		t.Fatal(b.String(), e)
	}
	return n
}
func assertRestoredRows(t *testing.T, ctx context.Context, target database.Adapter) {
	t.Helper()
	var out bytes.Buffer
	switch a := target.(type) {
	case *postgres.Adapter:
		e := a.Runner.Run(ctx, runner.Spec{Executable: "psql", Args: []string{"--no-psqlrc", "--no-password", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=SELECT count(*) FROM items"}, Env: map[string]string{"PGDATABASE": a.Config.Database, "PGUSER": a.Config.User, "PGPASSWORD": a.Password}, UnsetEnv: []string{"PGHOSTADDR"}, Stdout: &out})
		if e != nil || strings.TrimSpace(out.String()) != "1000" {
			t.Fatal(out.String(), e)
		}
	case *mysql.Adapter:
		e := a.Runner.Run(ctx, runner.Spec{Executable: "mysql", Args: []string{"--no-defaults", "--no-login-paths", "--host=127.0.0.1", "--user=root", "--ssl-mode=REQUIRED", "--batch", "--skip-column-names", "--database=" + a.Config.Database, "--execute=SELECT count(*) FROM items"}, Env: map[string]string{"MYSQL_PWD": a.Password}, Stdout: &out})
		if e != nil || strings.TrimSpace(out.String()) != "3" {
			t.Fatal(out.String(), e)
		}
	case *mongodb.Adapter:
		client, e := mongo.Connect(options.Client().SetHosts([]string{net.JoinHostPort(a.Config.Host, strconv.Itoa(a.Config.Port))}).SetAuth(options.Credential{Username: a.Config.User, Password: a.Password, AuthSource: a.Config.AuthDatabase}))
		if e != nil {
			t.Fatal(e)
		}
		defer client.Disconnect(ctx)
		count, e := client.Database(a.Config.Database).Collection("records").CountDocuments(ctx, bson.D{})
		if e != nil || count != 1000 {
			t.Fatal(fmt.Sprint(count), e)
		}
	}
}
