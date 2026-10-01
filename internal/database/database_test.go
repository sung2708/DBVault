package database

import (
	"strings"
	"testing"
)

// Capability rejection is also exercised through every adapter's backup tests.
func TestCapabilitiesAreExplicit(t *testing.T) {
	c := Capabilities{FullBackup: true}
	if c.IncrementalBackup || c.DifferentialBackup || c.SelectiveRestore {
		t.Fatal("unsupported features enabled by default")
	}
}

type capabilityAdapter struct {
	Adapter
	cap Capabilities
}

func (a capabilityAdapter) Name() string               { return "postgres" }
func (a capabilityAdapter) Capabilities() Capabilities { return a.cap }
func TestLogicalStrategiesRejectAdvancedBackupRequests(t *testing.T) {
	a := capabilityAdapter{cap: Capabilities{FullBackup: true, IncrementalBackup: true, DifferentialBackup: true}}
	if e := RequireBackup(a, "full"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"incremental", "differential", "unknown"} {
		if e := RequireBackup(a, kind); e == nil || !strings.Contains(e.Error(), kind) {
			t.Fatal("strategy would produce a mislabeled backup", kind, e)
		}
	}
	a.cap.FullBackup = false
	if RequireBackup(a, "full") == nil {
		t.Fatal("adapter without full capability accepted")
	}
}
