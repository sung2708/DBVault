package pitr

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/storage/local"
)

func TestNativeEncryptedArchiveAndContinuity(t *testing.T) {
	t.Setenv("PITR_TEST_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	store, e := local.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	svc := &Service{Store: store, Config: config.Config{Encryption: &config.Encryption{KeyID: "v1", Keys: map[string]string{"v1": "PITR_TEST_KEY"}}}}
	source := t.TempDir()
	if e = os.Mkdir(filepath.Join(source, "empty"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "base.sql"), []byte("database payload"), 0600); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	base, e := svc.publish(context.Background(), Record{Engine: "mysql", Identity: "server-uuid", ServerVersion: "8.4.0", Kind: "base", Start: "binlog.000001:123", End: "binlog.000001:123", From: now, Until: now}, source)
	if e != nil {
		t.Fatal(e)
	}
	logs := t.TempDir()
	os.WriteFile(filepath.Join(logs, "binlog.000001"), []byte("log payload"), 0600)
	child, e := svc.publish(context.Background(), Record{Engine: "mysql", Identity: base.Identity, ServerVersion: base.ServerVersion, Kind: "logs", Parent: base.Name, ParentHash: base.Hash, Start: base.End, End: "binlog.000002:4", From: base.Until, Until: now.Add(time.Minute)}, logs)
	if e != nil {
		t.Fatal(e)
	}
	chain, e := svc.chain(context.Background(), child.Name)
	if e != nil || len(chain) != 2 || chain[0].Name != base.Name {
		t.Fatal(chain, e)
	}
	dest := t.TempDir()
	if e = svc.unpack(context.Background(), base, dest); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(dest, "base.sql"))
	if string(data) != "database payload" {
		t.Fatal("payload mismatch")
	}
	if info, err := os.Stat(filepath.Join(dest, "empty")); err != nil || !info.IsDir() {
		t.Fatal("empty native directory was lost", err)
	}
	base.Identity = "changed"
	if e = svc.unpack(context.Background(), base, t.TempDir()); e == nil {
		t.Fatal("archive binding mismatch accepted")
	}
	for _, name := range []string{"../file", "pitr_fake.tar", ""} {
		if _, e = svc.Read(context.Background(), name); e == nil {
			t.Fatal("unsafe name accepted")
		}
	}
	if _, e = svc.Restore(context.Background(), child.Name, now.Add(2*time.Hour), "", nil, true); e == nil {
		t.Fatal("uncaptured target time accepted")
	}
}
func TestNativeLogSequences(t *testing.T) {
	if !consecutiveBinlog("binlog.000009", "binlog.000010") || consecutiveBinlog("binlog.000009", "binlog.000011") {
		t.Fatal("binary log sequence")
	}
	if got := nextWAL("000000010000000A000000FF", 16<<20); got != "000000010000000B00000000" {
		t.Fatal(got)
	}
	for _, size := range []int64{0, 123, 1 << 32} {
		if nextWAL("000000010000000A000000FF", size) != "" {
			t.Fatal("invalid WAL size accepted")
		}
	}
}
