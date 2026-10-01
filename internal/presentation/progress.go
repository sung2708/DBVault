package presentation

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/app"
)

var stages = map[string][2]string{
	"configuration":   {"Loading configuration", "Configuration loaded"},
	"preflight":       {"Checking database connectivity and tools", "Database connection and tools verified"},
	"backup.stream":   {"Creating, compressing and storing backup", "Backup stream stored"},
	"backup.register": {"Registering completed backup", "Backup metadata registered"},
	"manifest":        {"Reading backup metadata", "Backup metadata valid"},
	"verify":          {"Verifying stored size and SHA-256", "Stored size and SHA-256 verified"},
	"restore.verify":  {"Reading and verifying a private restore snapshot", "Restore snapshot verified"},
	"restore.write":   {"Decompressing and restoring database", "Database restore completed"},
	"delete":          {"Deleting archive and metadata", "Archive and metadata deleted"},
	"delete.preview":  {"Checking deletion selection", "Deletion preview ready"},
	"list":            {"Reading registered backups", "Backup listing loaded"},
	"cleanup.select":  {"Selecting retention candidates", "Retention selection ready"},
	"cleanup.verify":  {"Verifying registered backups before deletion", "Registered backups verified"},
	"cleanup.delete":  {"Deleting retention candidates", "Retention cleanup completed"},
}

func stageLabel(stage string, complete bool) string {
	if pair, ok := stages[stage]; ok {
		if complete {
			return pair[1]
		}
		return pair[0]
	}
	return stage
}
func (r *Renderer) animation() bool {
	return !r.options.DisableAnimation && !r.options.Quiet && !r.options.JSON && r.outCaps.TTY && r.errCaps.TTY && r.outCaps.Color && r.errCaps.Color
}
func (r *Renderer) clearLocked() {
	if r.visible {
		fmt.Fprint(r.err, "\r\x1b[2K")
		r.visible = false
	}
}
func (r *Renderer) Observe(e app.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Manifest.Name != "" {
		r.manifest = e.Manifest
	}
	if r.options.JSON || r.options.Quiet {
		return
	}
	switch e.State {
	case "start":
		r.clearLocked()
		r.stage = e.Stage
		r.active = true
		r.bytes = 0
		r.total = 0
		r.stageStart = r.options.Now()
		if !r.animation() {
			fmt.Fprintln(r.err, r.status("active", stageLabel(e.Stage, false)+"...", true))
		}
	case "progress":
		if r.stage == e.Stage {
			r.bytes = e.Bytes
			r.total = e.Total
		}
	case "complete":
		r.clearLocked()
		r.active = false
		fmt.Fprintln(r.err, r.status("success", stageLabel(e.Stage, true), true))
	}
}
func (r *Renderer) Step(stage string) { r.Observe(app.Event{Stage: stage, State: "start"}) }
func (r *Renderer) Done(stage string) { r.Observe(app.Event{Stage: stage, State: "complete"}) }
func (r *Renderer) progressLine() string {
	frames := []string{"|", "/", "-", "\\"}
	if r.errCaps.Unicode {
		frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	label := r.safe(stageLabel(r.stage, false))
	detail := ""
	if r.bytes > 0 {
		detail = "  " + FormatBytes(r.bytes)
		if r.total > 0 {
			detail += " / " + FormatBytes(r.total)
			if r.bytes <= r.total {
				fraction := float64(r.bytes) / float64(r.total)
				detail += fmt.Sprintf(" %d%%", int(fraction*100))
				if r.errCaps.Width >= 72 {
					filled := int(fraction * 10)
					detail += " [" + strings.Repeat("#", filled) + strings.Repeat("-", 10-filled) + "]"
				}
			}
		}
	}
	width := max(1, r.errCaps.Width-3-ansi.StringWidth(detail))
	label = ansi.Truncate(label, width, "...")
	return frames[r.frame%len(frames)] + " " + label + detail
}
func (r *Renderer) tick(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.animation() || !r.active || now.Sub(r.stageStart) < 200*time.Millisecond {
		return
	}
	r.clearLocked()
	fmt.Fprint(r.err, "\r"+r.errStyle.accent.Render(r.progressLine()))
	r.visible = true
	r.frame++
}
