package retention

import (
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"strings"
	"testing"
	"time"
)

func backup(name, db string, when time.Time) metadata.Manifest {
	return metadata.Manifest{Version: "1.0", ID: name, Name: name, BackupType: "full", CreatedAt: when, CompletedAt: when, Database: metadata.Database{Engine: "postgres", Name: db, Format: "custom"}, Pipeline: metadata.Pipeline{Compression: "none", Level: 6, Raw: 1, Stored: 1}, Checksum: metadata.Checksum{Algorithm: "sha256", Hash: strings.Repeat("0", 64)}, Status: "completed"}
}
func TestSelection(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	items := []metadata.Manifest{backup("new", "a", now), backup("boundary", "a", now.AddDate(0, 0, -7)), backup("old", "a", now.AddDate(0, 0, -8)), backup("other", "b", now.AddDate(0, 0, -30))}
	for _, tt := range []struct {
		policy config.Retention
		want   int
	}{{config.Retention{}, 0}, {config.Retention{KeepDays: 7}, 1}, {config.Retention{KeepCount: 1}, 2}, {config.Retention{KeepCount: 2, KeepDays: 7}, 1}, {config.Retention{KeepCount: 10}, 0}} {
		result, err := Select(items, tt.policy, now)
		if err != nil || len(result) != tt.want {
			t.Fatal(tt, result, err)
		}
		for _, m := range result {
			if m.Name == "new" || m.Name == "other" {
				t.Fatal("newest per database selected")
			}
		}
	}
	if _, err := Select(items, config.Retention{KeepDays: -1}, now); err == nil {
		t.Fatal("negative policy accepted")
	}
	items[0].Status = "partial"
	if _, err := Select(items, config.Retention{KeepCount: 1}, now); err == nil {
		t.Fatal("invalid manifest accepted")
	}
}

func TestIncrementalCleanupOrderAndProtection(t *testing.T) {
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	base := backup("a-base", "a", now.Add(-72*time.Hour))
	child := backup("b-child", "a", now.Add(-48*time.Hour))
	child.BackupType = "incremental"
	child.Delta = &metadata.Delta{BaseName: base.Name, BaseID: base.ID, BaseHash: base.Checksum.Hash, RawHash: base.Checksum.Hash, Depth: 1}
	leaf := backup("c-leaf", "a", now.Add(-24*time.Hour))
	leaf.BackupType = "incremental"
	leaf.Delta = &metadata.Delta{BaseName: child.Name, BaseID: child.ID, BaseHash: child.Checksum.Hash, RawHash: child.Checksum.Hash, Depth: 2}
	items := []metadata.Manifest{base, leaf, child}
	selected, err := Select(items, config.Retention{KeepCount: 1}, now)
	if err != nil || len(selected) != 0 {
		t.Fatalf("retained leaf lost ancestors: %v, %v", selected, err)
	}
	items = append(items, backup("new-full", "a", now))
	selected, err = Select(items, config.Retention{KeepCount: 1}, now)
	if err != nil || len(selected) != 3 {
		t.Fatalf("obsolete chain not selected: %v, %v", selected, err)
	}
	for i, want := range []string{leaf.Name, child.Name, base.Name} {
		if selected[i].Name != want {
			t.Fatalf("position %d: got %s, want %s", i, selected[i].Name, want)
		}
	}
	// Corrupt dependency cycles must abort selection before any deletion.
	base.BackupType = "incremental"
	base.Delta = &metadata.Delta{BaseName: leaf.Name, BaseID: leaf.ID, BaseHash: leaf.Checksum.Hash, RawHash: leaf.Checksum.Hash, Depth: 3}
	items[0] = base
	if _, err := Select(items, config.Retention{KeepCount: 1}, now); err == nil {
		t.Fatal("cyclic obsolete chain accepted")
	}
}
