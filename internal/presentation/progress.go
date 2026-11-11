package presentation

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/app"
)

var stages = map[string][2]string{
	"restore.create":        {"Checking and preparing new destination", "New destination checked/prepared"},
	"restore.safety":        {"Backing up and verifying existing destination", "Destination safety backup verified"},
	"restore.validation":    {"Checking restored destination", "Post-restore checks passed"},
	"export.write":          {"Writing verified export", "Verified export written"},
	"configuration":         {"Loading configuration", "Configuration loaded"},
	"preflight":             {"Checking database connectivity and tools", "Database connection and tools verified"},
	"backup.stream":         {"Creating, compressing and storing backup", "Backup stream stored"},
	"backup.base":           {"Verifying and reconstructing the base backup", "Base backup is ready"},
	"backup.dump":           {"Creating the current database snapshot", "Database snapshot is ready"},
	"backup.register":       {"Registering completed backup", "Backup metadata registered"},
	"manifest":              {"Reading backup metadata", "Backup metadata valid"},
	"verify":                {"Verifying stored size and SHA-256", "Stored size and SHA-256 verified"},
	"restore.verify":        {"Reading and verifying a private restore snapshot", "Restore snapshot verified"},
	"restore.write":         {"Decompressing and restoring database", "Database restore completed"},
	"restore.compatibility": {"Checking restore compatibility", "Restore compatibility verified"},
	"recovery.validation":   {"Validating restored SQLite structure", "Post-restore validation passed"},
	"delete":                {"Deleting archive and metadata", "Archive and metadata deleted"},
	"delete.preview":        {"Checking deletion selection", "Deletion preview ready"},
	"list":                  {"Reading registered backups", "Backup listing loaded"},
	"cleanup.select":        {"Selecting retention candidates", "Retention selection ready"},
	"cleanup.verify":        {"Verifying registered backups before deletion", "Registered backups verified"},
	"cleanup.delete":        {"Deleting retention candidates", "Retention cleanup completed"},
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
		r.rate = 0
		r.sampleAt = r.stageStart
		r.sampleBytes = 0
		if !r.animation() {
			fmt.Fprintln(r.err, r.status("active", stageLabel(e.Stage, false)+"...", true))
		}
	case "progress":
		if r.stage == e.Stage {
			r.bytes = e.Bytes
			r.total = e.Total
			r.sample(r.options.Now())
		}
	case "complete":
		r.clearLocked()
		r.active = false
		fmt.Fprintln(r.err, r.status("success", stageLabel(e.Stage, true), true))
	}
}
func (r *Renderer) Step(stage string) { r.Observe(app.Event{Stage: stage, State: "start"}) }
func (r *Renderer) Done(stage string) { r.Observe(app.Event{Stage: stage, State: "complete"}) }

const (
	// rateWindow is the minimum spacing between throughput samples; rateWeight
	// is how much each new sample moves the smoothed rate.
	rateWindow = 250 * time.Millisecond
	rateWeight = 0.3
	// Throughput and ETA stay hidden until a stage has run this long.
	rateWarmup = 2 * time.Second
)

// sample folds consumed bytes into an exponentially smoothed rate.
func (r *Renderer) sample(now time.Time) {
	elapsed := now.Sub(r.sampleAt)
	if elapsed < rateWindow || r.bytes < r.sampleBytes {
		return
	}
	current := float64(r.bytes-r.sampleBytes) / elapsed.Seconds()
	if r.rate == 0 {
		r.rate = current
	} else {
		r.rate = rateWeight*current + (1-rateWeight)*r.rate
	}
	r.sampleAt, r.sampleBytes = now, r.bytes
}

func (r *Renderer) progressLine() string {
	frames := []string{"|", "/", "-", "\\"}
	if r.errCaps.Unicode {
		frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	label := r.safe(stageLabel(r.stage, false))
	var counted, percent, bar, rate, eta string
	if r.bytes > 0 {
		counted = FormatBytes(r.bytes)
		known := r.total > 0 && r.bytes <= r.total
		if r.total > 0 {
			counted += " / " + FormatBytes(r.total)
		}
		if known {
			fraction := float64(r.bytes) / float64(r.total)
			percent = fmt.Sprintf("%d%%", int(fraction*100))
			if r.errCaps.Width >= 72 {
				filled := int(fraction * 10)
				if r.errCaps.Unicode {
					bar = strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
				} else {
					bar = "[" + strings.Repeat("#", filled) + strings.Repeat("-", 10-filled) + "]"
				}
			}
		}
		if r.rate > 0 && r.options.Now().Sub(r.stageStart) >= rateWarmup {
			rate = FormatBytes(int64(r.rate)) + "/s"
			if known && r.bytes < r.total {
				if left := time.Duration(float64(r.total-r.bytes) / r.rate * float64(time.Second)); left < 100*time.Hour {
					eta = "ETA " + formatETA(left)
				}
			}
		}
	}
	// Keep a readable label; drop the least important details first.
	room := r.errCaps.Width - 3 - min(16, ansi.StringWidth(label))
	detail := ""
	for _, parts := range [][]string{{counted, percent, bar, rate, eta}, {counted, percent, rate, eta}, {counted, percent, rate}, {counted, percent}, {counted}} {
		detail = joinDetail(parts)
		if ansi.StringWidth(detail) <= room {
			break
		}
	}
	width := max(1, r.errCaps.Width-3-ansi.StringWidth(detail))
	label = ansi.Truncate(label, width, "...")
	return frames[r.frame%len(frames)] + " " + label + detail
}

func joinDetail(parts []string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "  " + strings.Join(kept, " ")
}

func formatETA(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
