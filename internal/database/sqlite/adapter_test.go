package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"path/filepath"
	"testing"
	"time"
)

func TestWALSnapshotAndOnlineRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unicode dữ liệu.sqlite")
	db, err := sql.Open("sqlite", uri(path, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE records(id INTEGER PRIMARY KEY, value TEXT)", "INSERT INTO records(value) VALUES ('Việt Nam'),('snapshot')"} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a := &Adapter{Config: config.Database{Database: path}}
	info, err := a.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Compatible(info, info.ServerVersion, info.ToolVersion); err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err = a.Dump(context.Background(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TABLE records; CREATE TABLE unwanted(x)"); err != nil {
		t.Fatal(err)
	}
	if err = a.Restore(context.Background(), bytes.NewReader(snapshot.Bytes()), database.RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err = db.QueryRow("SELECT group_concat(value, '|') FROM records").Scan(&got); err != nil || got != "Việt Nam|snapshot" {
		t.Fatalf("restored %q: %v", got, err)
	}
	if err = a.Restore(context.Background(), bytes.NewBufferString("not SQLite"), database.RestoreOptions{}); err == nil {
		t.Fatal("corrupt image accepted")
	}
	if err = db.QueryRow("SELECT group_concat(value, '|') FROM records").Scan(&got); err != nil || got != "Việt Nam|snapshot" {
		t.Fatal("invalid restore modified target")
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("INSERT INTO records(value) VALUES ('lock')"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err = a.Restore(ctx, bytes.NewReader(snapshot.Bytes()), database.RestoreOptions{})
	tx.Rollback()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("locked restore: %v", err)
	}
}
