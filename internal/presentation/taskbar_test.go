package presentation

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
)

func TestTerminalProgressAllowlist(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{}, false},
		{map[string]string{"WT_SESSION": "x"}, true},
		{map[string]string{"ConEmuANSI": "ON"}, true},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.6"}, true},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.10.0beta1"}, true},
		// Older iTerm2 shows OSC 9 as a desktop notification.
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.5"}, false},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, false},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{map[string]string{"WT_SESSION": "x", "TMUX": "/tmp/tmux"}, false},
		{map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "screen-256color"}, false},
		{map[string]string{"WT_SESSION": "x", progressEnv: "0"}, false},
		{map[string]string{progressEnv: "1", "TMUX": "/tmp/tmux"}, true},
	} {
		got := terminalProgressSupported(func(k string) string { return tc.env[k] })
		if got != tc.want {
			t.Errorf("%v: got %t, want %t", tc.env, got, tc.want)
		}
	}
}

func TestTerminalProgressFollowsStageAndClears(t *testing.T) {
	enableColorEnv(t)
	now := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	var out, errOut bytes.Buffer
	caps := &Capabilities{TTY: true, Color: true, Unicode: true, Progress: true, Width: 120}
	r := New(&out, &errOut, Options{OutCaps: caps, ErrCaps: caps, Now: func() time.Time { return now }})
	r.Step("restore.write")
	now = now.Add(time.Second)
	r.Observe(app.Event{Stage: "restore.write", State: "progress", Bytes: 25, Total: 100})
	r.tick(now)
	r.tick(now.Add(100 * time.Millisecond)) // unchanged state within keep-alive: not resent
	if got := strings.Count(errOut.String(), "\x1b]9;4;1;25\x1b\\"); got != 1 {
		t.Fatalf("want one 25%% report, got %d in %q", got, errOut.String())
	}
	r.tick(now.Add(2 * time.Second)) // keep-alive resend
	if got := strings.Count(errOut.String(), "\x1b]9;4;1;25\x1b\\"); got != 2 {
		t.Fatalf("keep-alive not resent: %q", errOut.String())
	}
	r.Done("restore.write")
	if !strings.HasSuffix(strings.SplitAfter(errOut.String(), "\x1b]9;4;0;0\x1b\\")[0], "\x1b]9;4;0;0\x1b\\") {
		t.Fatalf("progress not cleared on completion: %q", errOut.String())
	}

	// Unknown totals are indeterminate.
	errOut.Reset()
	r.Step("backup.stream")
	now = now.Add(5 * time.Second)
	r.Observe(app.Event{Stage: "backup.stream", State: "progress", Bytes: 1 << 20})
	r.tick(now)
	if !strings.Contains(errOut.String(), "\x1b]9;4;3;0\x1b\\") {
		t.Fatalf("missing indeterminate state: %q", errOut.String())
	}
	r.Error("backup", "dbvault backup", "dbvault backup --help", errTest)
	if !strings.Contains(errOut.String(), "\x1b]9;4;0;0\x1b\\") {
		t.Fatal("progress not cleared on error")
	}
}

func TestTerminalProgressNeverWithoutCapabilityOrColor(t *testing.T) {
	enableColorEnv(t)
	now := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	for _, o := range []Options{
		{OutCaps: &Capabilities{TTY: true, Color: true, Width: 120}, ErrCaps: &Capabilities{TTY: true, Color: true, Width: 120}},
		{NoColor: true, OutCaps: &Capabilities{TTY: true, Color: true, Progress: true, Width: 120}, ErrCaps: &Capabilities{TTY: true, Color: true, Progress: true, Width: 120}},
	} {
		var out, errOut bytes.Buffer
		o.Now = func() time.Time { return now }
		r := New(&out, &errOut, o)
		r.Step("verify")
		now = now.Add(time.Second)
		r.Observe(app.Event{Stage: "verify", State: "progress", Bytes: 5, Total: 10})
		r.tick(now)
		r.Done("verify")
		if strings.Contains(errOut.String(), "\x1b]9;4") {
			t.Fatalf("unexpected OSC 9;4 for %+v: %q", o, errOut.String())
		}
	}
}

var errTest = errors.New("test failure")
