package presentation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/app"
)

func TestHealthTerminalPolicies(t *testing.T) {
	enableColorEnv(t)
	for _, mode := range []string{"tty", "non-tty", "no-color", "NO_COLOR", "quiet"} {
		t.Run(mode, func(t *testing.T) {
			o := Options{OutCaps: tty(100), ErrCaps: tty(100)}
			switch mode {
			case "non-tty":
				o.OutCaps = &Capabilities{Width: 80}
			case "no-color":
				o.NoColor = true
			case "NO_COLOR":
				t.Setenv("NO_COLOR", "1")
			case "quiet":
				o.Quiet = true
			}
			var out, stderr bytes.Buffer
			r := New(&out, &stderr, o)
			if err := r.Result("health", app.HealthReport{Status: app.Unknown, Databases: []app.DatabaseHealth{{Name: "production", Status: app.Unknown, Integrity: "unknown", RestoreTest: "unknown", Reason: "No backup freshness policy configured"}}}); err != nil {
				t.Fatal(err)
			}
			color := mode == "tty"
			if strings.Contains(out.String(), "\x1b[") != color || stderr.Len() != 0 {
				t.Fatalf("%q %q", out.String(), stderr.String())
			}
			text := ansi.Strip(out.String())
			if strings.Contains(text, "[36m") || !strings.Contains(text, "Last backup   Unknown") {
				t.Fatal(text)
			}
			if mode == "quiet" && (strings.Contains(text, "◆") || strings.Contains(text, "? unknown") || strings.Contains(text, "recovery drill")) {
				t.Fatal(text)
			}
		})
	}
}
