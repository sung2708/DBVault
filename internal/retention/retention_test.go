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
