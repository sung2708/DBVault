package presentation

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Terminal tab/taskbar progress uses the ConEmu OSC 9;4 sequence. iTerm2
// before 3.6.6 and other terminals read OSC 9 as a desktop notification, so it
// is sent only to terminals known to render progress, never through tmux or
// screen. DBVAULT_TERMINAL_PROGRESS=0 disables it and =1 forces it.
const progressEnv = "DBVAULT_TERMINAL_PROGRESS"

// progressKeepAlive refreshes the state: some terminals (Ghostty) reset a
// progress bar that is not updated for about 15 seconds.
const progressKeepAlive = time.Second

func terminalProgressSupported(getenv func(string) string) bool {
	switch getenv(progressEnv) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	}
	if getenv("TMUX") != "" || getenv("STY") != "" || strings.HasPrefix(getenv("TERM"), "screen") {
		return false
	}
	if getenv("WT_SESSION") != "" || getenv("ConEmuANSI") == "ON" {
		return true
	}
	switch getenv("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	case "iTerm.app":
		return versionAtLeast(getenv("TERM_PROGRAM_VERSION"), 3, 6, 6)
	}
	return false
}

func versionAtLeast(v string, want ...int) bool {
	parts := strings.Split(v, ".")
	for i, w := range want {
		if i >= len(parts) {
			return false
		}
		digits := parts[i]
		if end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			digits = digits[:end]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return false
		}
		if n != w {
			return n > w
		}
	}
	return true
}

func detectTerminalProgress() bool { return terminalProgressSupported(os.Getenv) }

// progressSequence returns the OSC 9;4 state for the active stage: a percentage
// when the total is known, otherwise indeterminate.
func (r *Renderer) progressSequence() string {
	if r.total > 0 && r.bytes <= r.total {
		return fmt.Sprintf("\x1b]9;4;1;%d\x1b\\", int(float64(r.bytes)/float64(r.total)*100))
	}
	return "\x1b]9;4;3;0\x1b\\"
}

func (r *Renderer) updateTerminalProgressLocked(now time.Time) {
	if !r.errCaps.Progress {
		return
	}
	seq := r.progressSequence()
	if seq == r.taskbarState && now.Sub(r.taskbarSent) < progressKeepAlive {
		return
	}
	fmt.Fprint(r.err, seq)
	r.taskbarState, r.taskbarSent = seq, now
}

// clearTerminalProgressLocked removes the indicator so no stale bar outlives
// the stage, an error, cancellation or the command itself.
func (r *Renderer) clearTerminalProgressLocked() {
	if r.taskbarState == "" {
		return
	}
	fmt.Fprint(r.err, "\x1b]9;4;0;0\x1b\\")
	r.taskbarState = ""
}
