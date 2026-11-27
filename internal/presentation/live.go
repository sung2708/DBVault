package presentation

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/security"
)

// LiveView redraws one block of output in place, for refreshing views such as
// status --watch. It moves the cursor back over the previous frame and clears
// to the end of the screen; it never enters the alternate screen, so the last
// frame stays in the terminal after exit.
type LiveView struct {
	out     io.Writer
	options Options
	caps    Capabilities
	lines   int
}

func NewLiveView(out io.Writer, o Options) *LiveView {
	caps := Detect(out)
	if o.OutCaps != nil {
		caps = *o.OutCaps
	}
	if o.Redactor == nil {
		o.Redactor = security.New()
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor || o.NoColor {
		caps.Color = false
	}
	return &LiveView{out: out, options: o, caps: caps}
}

// Supported reports whether in-place redraws are safe: a terminal on stdout,
// with human output (not JSON or quiet).
func (v *LiveView) Supported() bool {
	return v.caps.TTY && os.Getenv("TERM") != "dumb" && !v.options.JSON && !v.options.Quiet
}

// Render returns value as the normal human result, rendered for stdout's
// capabilities but captured so it can be drawn as one frame.
func (v *LiveView) Render(operation string, value any) (string, error) {
	var b bytes.Buffer
	o := v.options
	caps := v.caps
	o.OutCaps, o.ErrCaps = &caps, &caps
	o.DisableAnimation = true
	if err := New(&b, io.Discard, o).Result(operation, value); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Footer is a muted status line under the frame.
func (v *LiveView) Footer(text string) string {
	return newStyles(v.caps.Color).muted.Render(Sanitize(v.options.Redactor.Text(text))) + "\n"
}

// Draw replaces the previous frame with frame.
func (v *LiveView) Draw(frame string) {
	if !strings.HasSuffix(frame, "\n") {
		frame += "\n"
	}
	if v.lines > 0 {
		// Cursor to the start of the previous frame's first line, then clear.
		fmt.Fprintf(v.out, "\x1b[%dF\x1b[J", v.lines)
	}
	fmt.Fprint(v.out, frame)
	v.lines = visualLines(frame, v.caps.Width)
}

// visualLines counts terminal rows, including rows added when the terminal
// wraps a line wider than the screen.
func visualLines(frame string, width int) int {
	rows := 0
	for _, line := range strings.Split(strings.TrimSuffix(frame, "\n"), "\n") {
		w := ansi.StringWidth(line)
		if width <= 0 || w <= width {
			rows++
			continue
		}
		rows += (w + width - 1) / width
	}
	return rows
}
