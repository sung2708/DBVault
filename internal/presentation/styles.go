package presentation

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"io"
)

type styles struct{ accent, success, warning, failure, muted lipgloss.Style }

func newStyles(color bool) styles {
	renderer := lipgloss.NewRenderer(io.Discard)
	profile := termenv.Ascii
	if color {
		profile = termenv.ANSI
	}
	renderer.SetColorProfile(profile)
	return styles{
		accent:  renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		success: renderer.NewStyle().Foreground(lipgloss.Color("2")),
		warning: renderer.NewStyle().Foreground(lipgloss.Color("3")),
		failure: renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
		muted:   renderer.NewStyle().Foreground(lipgloss.Color("8")),
	}
}
func symbol(kind string, unicode bool) string {
	if unicode {
		switch kind {
		case "header":
			return "◆"
		case "success":
			return "✓"
		case "error":
			return "✗"
		case "warning":
			return "!"
		case "unknown":
			return "?"
		default:
			return "→"
		}
	}
	switch kind {
	case "header":
		return ""
	case "success":
		return "[OK]"
	case "error":
		return "[ERROR]"
	case "warning":
		return "[WARN]"
	case "unknown":
		return "?"
	default:
		return ">"
	}
}
