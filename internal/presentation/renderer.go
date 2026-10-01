package presentation

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/security"
)

type Options struct {
	JSON, Quiet, NoColor, Verbose, DisableAnimation bool
	OutCaps, ErrCaps                                *Capabilities
	Redactor                                        *security.Redactor
	Now                                             func() time.Time
}
type Details struct {
	Operation, Target, ConfigPath string
	Config                        config.Config
	DryRun                        bool
	Started                       time.Time
}
type Renderer struct {
	out, err           io.Writer
	options            Options
	outCaps, errCaps   Capabilities
	outStyle, errStyle styles
	redactor           *security.Redactor
	mu                 sync.Mutex
	details            Details
	manifest           metadata.Manifest
	stage              string
	active, visible    bool
	bytes, total       int64
	stageStart         time.Time
	frame              int
}

func New(out, err io.Writer, o Options) *Renderer {
	outCaps, errCaps := Detect(out), Detect(err)
	if o.OutCaps != nil {
		outCaps = *o.OutCaps
	}
	if o.ErrCaps != nil {
		errCaps = *o.ErrCaps
	}
	_, noColorEnv := os.LookupEnv("NO_COLOR")
	if o.NoColor || noColorEnv || o.Quiet || o.JSON || !outCaps.TTY || !errCaps.TTY {
		outCaps.Color = false
		errCaps.Color = false
	}
	outCaps.Color = outCaps.Color && outCaps.TTY
	errCaps.Color = errCaps.Color && errCaps.TTY
	if outCaps.Width <= 0 {
		outCaps.Width = 80
	}
	if errCaps.Width <= 0 {
		errCaps.Width = 80
	}
	if o.Redactor == nil {
		o.Redactor = security.New()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Renderer{out: out, err: err, options: o, outCaps: outCaps, errCaps: errCaps, outStyle: newStyles(outCaps.Color), errStyle: newStyles(errCaps.Color), redactor: o.Redactor}
}
func (r *Renderer) Configure(d Details, redactor *security.Redactor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.details = d
	if redactor != nil {
		r.redactor = redactor
	}
}
func (r *Renderer) safe(s string) string { return Sanitize(r.redactor.Text(s)) }
func (r *Renderer) status(kind, s string, stderr bool) string {
	caps, st := r.outCaps, r.outStyle
	if stderr {
		caps, st = r.errCaps, r.errStyle
	}
	text := r.safe(s)
	if !r.options.Quiet {
		text = strings.TrimSpace(symbol(kind, caps.Unicode) + " " + text)
	}
	switch kind {
	case "success":
		return st.success.Render(text)
	case "error":
		return st.failure.Render(text)
	case "warning":
		return st.warning.Render(text)
	case "unknown":
		return st.muted.Render(text)
	default:
		return st.accent.Render(text)
	}
}
func (r *Renderer) Status(kind, s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.JSON || r.options.Quiet {
		return
	}
	r.clearLocked()
	fmt.Fprintln(r.err, r.status(kind, s, true))
}
func (r *Renderer) Warning(s string) { r.Status("warning", s) }
func (r *Renderer) Help(text string, root bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	if root && !r.options.Quiet && !r.options.JSON {
		fmt.Fprintln(&b, r.status("header", "DBVault", false))
		fmt.Fprintln(&b)
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if len(line) > 0 && line[0] != ' ' && strings.HasSuffix(line, ":") {
			line = r.outStyle.accent.Render(line)
		}
		fmt.Fprintln(&b, line)
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}
func (r *Renderer) field(b *strings.Builder, label, value string) {
	value = r.safe(value)
	if value == "" {
		value = "unknown"
	}
	prefix := fmt.Sprintf("  %-13s ", label)
	if r.outCaps.TTY {
		width := r.outCaps.Width - ansi.StringWidth(prefix)
		if width < 12 {
			fmt.Fprintln(b, r.outStyle.muted.Render("  "+label))
			prefix = "  "
			width = max(1, r.outCaps.Width-2)
		}
		value = ansi.Hardwrap(value, width, true)
		value = strings.ReplaceAll(value, "\n", "\n"+strings.Repeat(" ", ansi.StringWidth(prefix)))
	}
	fmt.Fprintln(b, r.outStyle.muted.Render(prefix)+value)
}
func (r *Renderer) title(b *strings.Builder, kind, title string) {
	fmt.Fprintln(b, r.status(kind, title, false))
	if !r.options.Quiet {
		fmt.Fprintln(b)
	}
}
func (r *Renderer) hint(b *strings.Builder, text string) {
	if !r.options.Quiet {
		fmt.Fprintln(b)
		fmt.Fprintln(b, r.outStyle.muted.Render(text))
	}
}

// Start owns only one transient stderr line, never an alternate screen or cursor
// visibility. Both streams must be terminals; no-color/JSON/quiet are stable.
func (r *Renderer) Start(ctx context.Context) func() {
	if !r.animation() {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		defer func() { r.mu.Lock(); defer r.mu.Unlock(); r.clearLocked() }()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				r.tick(now)
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }); <-done }
}
