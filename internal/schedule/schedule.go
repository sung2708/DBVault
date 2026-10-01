// Package schedule supplies durable cron definitions and a foreground daemon.
package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/robfig/cron/v3"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
	_ "time/tzdata"
)

type Job struct {
	ID      string `json:"id"`
	Cron    string `json:"cron"`
	Config  string `json:"config"`
	Enabled bool   `json:"enabled"`
}
type State struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func Validate(j Job) error {
	if !identifier.MatchString(j.ID) {
		return fmt.Errorf("schedule ID must contain 1-64 letters, digits, underscores or hyphens")
	}
	if _, err := parser.Parse(j.Cron); err != nil {
		return fmt.Errorf("invalid five-field cron expression: %w", err)
	}
	if j.Config == "" {
		return fmt.Errorf("schedule requires a config file")
	}
	return nil
}
func Load(path string) (State, error) {
	s := State{Version: 1, Jobs: []Job{}}
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return s, e
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return s, fmt.Errorf("schedule state must be a regular file under 1 MiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(&s); e != nil {
		return s, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return s, fmt.Errorf("schedule state has trailing data")
	}
	if s.Version != 1 {
		return s, fmt.Errorf("unsupported schedule state version")
	}
	seen := map[string]bool{}
	for _, j := range s.Jobs {
		if e = Validate(j); e != nil {
			return s, e
		}
		if seen[j.ID] {
			return s, fmt.Errorf("duplicate schedule ID")
		}
		seen[j.ID] = true
	}
	return s, nil
}

// Update serializes writers using an exclusive, operator-owned lock file. A
// crashed writer may leave this lock; it must be removed after checking no
// writer is running. Definitions never store resolved credentials.
func Update(path string, change func(*State) error) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	lock, e := os.OpenFile(abs+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("schedule state is locked or unavailable: %w", e)
	}
	lock.Close()
	defer os.Remove(abs + ".lock")
	s, e := Load(abs)
	if e != nil {
		return e
	}
	if e = change(&s); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, j := range s.Jobs {
		if e = Validate(j); e != nil {
			return e
		}
		if seen[j.ID] {
			return fmt.Errorf("duplicate schedule ID")
		}
		seen[j.ID] = true
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("schedule state exceeds 1 MiB")
	}
	f, e := os.CreateTemp(filepath.Dir(abs), ".dbvault-schedule-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), abs)
}

type Result struct {
	ID      string
	Err     error
	Skipped bool
}

// Run uses UTC unless a job explicitly supplies CRON_TZ. It never catches up
// missed executions. All jobs share a nonblocking gate to bound resource use.
func Run(ctx context.Context, jobs []Job, run func(context.Context, Job) error, report func(Result)) error {
	engine := cron.New(cron.WithParser(parser), cron.WithLocation(time.UTC))
	var gate sync.Mutex
	for _, job := range jobs {
		j := job
		if e := Validate(j); e != nil {
			return e
		}
		if !j.Enabled {
			continue
		}
		if _, e := engine.AddFunc(j.Cron, func() {
			if ctx.Err() != nil {
				return
			}
			if !gate.TryLock() {
				if report != nil {
					report(Result{ID: j.ID, Skipped: true})
				}
				return
			}
			defer gate.Unlock()
			e := run(ctx, j)
			if report != nil {
				report(Result{ID: j.ID, Err: e})
			}
		}); e != nil {
			return e
		}
	}
	engine.Start()
	<-ctx.Done()
	<-engine.Stop().Done()
	return ctx.Err()
}
