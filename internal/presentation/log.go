package presentation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// HumanHandler funnels diagnostics through the renderer's transient-line lock.
// Default operation feedback uses actual events; raw informational logs are only
// shown in verbose mode, avoiding duplicate progress and key=value noise.
type HumanHandler struct {
	Renderer *Renderer
	Attrs    []slog.Attr
	Group    string
}

func (h *HumanHandler) Enabled(_ context.Context, l slog.Level) bool {
	if h.Renderer.options.Quiet {
		return l >= slog.LevelError
	}
	return h.Renderer.options.Verbose || l >= slog.LevelWarn
}
func (h *HumanHandler) Handle(_ context.Context, record slog.Record) error {
	r := h.Renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLocked()
	kind := "active"
	if record.Level >= slog.LevelError {
		kind = "error"
	} else if record.Level >= slog.LevelWarn {
		kind = "warning"
	}
	fmt.Fprintln(r.err, r.status(kind, record.Message, true))
	if r.options.Verbose {
		attrs := append([]slog.Attr(nil), h.Attrs...)
		record.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
		for _, a := range attrs {
			fmt.Fprintf(r.err, "  %s%s: %s\n", h.Group, r.safe(a.Key), r.safe(a.Value.Resolve().String()))
		}
	}
	return nil
}
func (h *HumanHandler) WithAttrs(a []slog.Attr) slog.Handler {
	clone := *h
	clone.Attrs = append(append([]slog.Attr(nil), h.Attrs...), a...)
	return &clone
}
func (h *HumanHandler) WithGroup(name string) slog.Handler {
	clone := *h
	if strings.TrimSpace(name) != "" {
		clone.Group += name + "."
	}
	return &clone
}
