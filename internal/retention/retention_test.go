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

func TestCalendarRetention(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	var items []metadata.Manifest
	// Two backups a day for 120 days, newest first after sorting.
	for d := 0; d < 120; d++ {
		for _, h := range []int{2, 14} {
			at := time.Date(2026, 11, 27, h, 0, 0, 0, time.UTC).AddDate(0, 0, -d)
			if at.After(now) {
				continue
			}
			items = append(items, backup(at.Format("db_20060102_15"), "a", at))
		}
	}
	policy := config.Retention{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3}
	selected, err := Select(items, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	deleted := map[string]bool{}
	for _, m := range selected {
		deleted[m.Name] = true
	}
	kept := []string{}
	for _, m := range items {
		if !deleted[m.Name] {
			kept = append(kept, m.Name)
		}
	}
	want := map[string]bool{
		// Newest of each of the last 7 days (today only has the 02:00 backup).
		"db_20261127_02": true, "db_20261126_14": true, "db_20261125_14": true, "db_20261124_14": true,
		"db_20261123_14": true, "db_20261122_14": true, "db_20261121_14": true,
		// Newest of ISO weeks 48 (today), 47, 46 and 45: Sundays.
		"db_20261115_14": true, "db_20261108_14": true,
		// Newest of November (today), October and September.
		"db_20261031_14": true, "db_20260930_14": true,
	}
	if len(kept) != len(want) {
		t.Fatalf("kept %d backups %v, want %d", len(kept), kept, len(want))
	}
	for _, name := range kept {
		if !want[name] {
			t.Fatalf("unexpected kept backup %s in %v", name, kept)
		}
	}

	// Calendar rules combine with count and age as a union.
	selected, err = Select(items, config.Retention{KeepYearly: 1, KeepCount: 3}, now)
	if err != nil || len(selected) != len(items)-3 {
		t.Fatalf("yearly+count: %d selected, %v", len(selected), err)
	}
	if _, err := Select(items, config.Retention{KeepWeekly: -1}, now); err == nil {
		t.Fatal("negative calendar rule accepted")
	}
}

func TestCalendarSkipsEmptyPeriods(t *testing.T) {
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	items := []Dated{{"nov", jan.AddDate(0, 10, 0)}, {"mar", jan.AddDate(0, 2, 0)}, {"jan", jan}}
	got := Calendar(items, config.Retention{KeepMonthly: 2})
	if !got["nov"] || !got["mar"] || got["jan"] {
		t.Fatalf("months without backups must not use the count: %v", got)
	}
}

func TestSelectiveBackupCannotDisplaceWholeDatabaseBackup(t *testing.T) {
	now := time.Now()
	partial := backup("partial", "orders", now)
	partial.Database.IncludeTables = []string{"public.audit_log"}
	full := backup("full", "orders", now.Add(-time.Hour))
	old := backup("old", "orders", now.Add(-2*time.Hour))
	got, err := Select([]metadata.Manifest{partial, old, full}, config.Retention{KeepCount: 1}, now)
	if err != nil || len(got) != 1 || got[0].Name != old.Name {
		t.Fatalf("selected %v: %v", got, err)
	}
}
