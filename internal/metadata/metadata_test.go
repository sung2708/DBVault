package metadata

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func fixture() Manifest {
	now := time.Now().UTC()
	return Manifest{Version: "1.0", ID: "id", Name: "db.dump", BackupType: "full", CreatedAt: now, CompletedAt: now, Database: Database{Engine: "postgres", Format: "custom", Name: "db"}, Pipeline: Pipeline{Compression: "gzip", Level: 6, Raw: 100, Stored: 50}, Checksum: Checksum{Algorithm: "sha256", Hash: strings.Repeat("0", 64)}, Status: "completed"}
}
func TestManifest(t *testing.T) {
	m := fixture()
	b, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Decode(bytes.NewReader(b))
	if err != nil || n.Name != m.Name {
		t.Fatal(n, err)
	}
	m.Version = "2.0"
	if _, err = Encode(m); err == nil {
		t.Fatal("future version accepted")
	}
	if _, err = Decode(strings.NewReader(string(b) + "{}")); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
func TestIdentity(t *testing.T) {
	now := time.Date(2026, 10, 1, 2, 3, 4, 0, time.FixedZone("test", 7*3600))
	_, a, err := Identity("../db:with\\unsafe", ".dump.gz", now)
	if err != nil {
		t.Fatal(err)
	}
	_, b, _ := Identity("../db:with\\unsafe", ".dump.gz", now)
	if a == b || strings.ContainsAny(a, "/\\:") || !strings.Contains(a, "20260930_190304") {
		t.Fatal(a, b)
	}
}
