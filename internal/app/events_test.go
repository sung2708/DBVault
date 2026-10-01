package app

import (
	"context"
	"github.com/sung2708/DBVault/internal/database"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestProgressReflectsConsumedBytesAndVerifiedStages(t *testing.T) {
	s, _, dir := setup(t, "gzip")
	var mu sync.Mutex
	var events []Event
	s.Observe = func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() }
	m, e := s.Backup(context.Background(), "full", false)
	if e != nil {
		t.Fatal(e)
	}
	check := func(stage string, bytes, total int64) {
		t.Helper()
		var last Event
		complete := false
		mu.Lock()
		defer mu.Unlock()
		for _, e := range events {
			if e.Stage == stage {
				if e.State == "progress" {
					last = e
				}
				if e.State == "complete" {
					complete = true
				}
			}
		}
		if last.Bytes != bytes || last.Total != total || !complete {
			t.Fatal(stage, last, complete)
		}
	}
	check("backup.stream", m.Pipeline.Stored, 0)
	if _, e = s.Verify(context.Background(), m.Name); e != nil {
		t.Fatal(e)
	}
	check("verify", m.Pipeline.Stored, m.Pipeline.Stored)
	if e = s.Restore(context.Background(), m.Name, true, false, database.RestoreOptions{}); e != nil {
		t.Fatal(e)
	}
	check("restore.verify", m.Pipeline.Stored, m.Pipeline.Stored)
	check("restore.write", m.Pipeline.Raw, m.Pipeline.Raw)
	mu.Lock()
	events = nil
	mu.Unlock()
	f, e := os.OpenFile(filepath.Join(dir, m.Name), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteAt([]byte("corrupt"), 0)
	f.Close()
	if _, e = s.Verify(context.Background(), m.Name); e == nil {
		t.Fatal("corruption accepted")
	}
	for _, e := range events {
		if e.Stage == "verify" && e.State == "complete" {
			t.Fatal("failed verification reported success")
		}
	}
}
