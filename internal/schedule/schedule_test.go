package schedule

import (
	"context"
	"errors"
	"github.com/robfig/cron/v3"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStateLifecycleAndValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	j := Job{ID: "daily", Cron: "CRON_TZ=Asia/Bangkok 0 2 * * *", Config: "config.yaml", Enabled: true}
	if e := Validate(j); e != nil {
		t.Fatal(e)
	}
	if e := Update(p, func(s *State) error { s.Jobs = append(s.Jobs, j); return nil }); e != nil {
		t.Fatal(e)
	}
	if e := Update(p, func(s *State) error { s.Jobs[0].Enabled = false; return nil }); e != nil {
		t.Fatal(e)
	}
	s, e := Load(p)
	if e != nil || len(s.Jobs) != 1 || s.Jobs[0].Enabled {
		t.Fatal(s, e)
	}
	j.Cron = "* * * * * *"
	if Validate(j) == nil {
		t.Fatal("seconds accepted")
	}
	j.Cron = "0 2 * * *"
	j.ID = "../escape"
	if Validate(j) == nil {
		t.Fatal("invalid id accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = Run(ctx, s.Jobs, func(context.Context, Job) error { t.Fatal("unexpected run"); return nil }, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestDaemonSkipsOverlapAndCancelsActiveBackup(t *testing.T) {
	// Use second-granularity cron only in this test so the real scheduling loop
	// can be exercised promptly. Production parsing remains five fields.
	original := parser
	parser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	defer func() { parser = original }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := make(chan struct{}, 2)
	reports := make(chan Result, 10)
	done := make(chan error, 1)
	jobs := []Job{{ID: "one", Cron: "* * * * * *", Config: "config.yaml", Enabled: true}, {ID: "two", Cron: "* * * * * *", Config: "config.yaml", Enabled: true}}
	go func() {
		done <- Run(ctx, jobs, func(ctx context.Context, _ Job) error {
			calls.Add(1)
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}, func(r Result) { reports <- r })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("scheduled backup never started")
	}
	select {
	case r := <-reports:
		if !r.Skipped {
			cancel()
			<-done
			t.Fatal("overlap was executed", r)
		}
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("overlap was not reported")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon failed to stop active backup")
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping executions", calls.Load())
	}
}
