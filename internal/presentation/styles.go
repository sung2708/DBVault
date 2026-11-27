package presentation

import "charm.land/lipgloss/v2"

type styles struct{ accent, success, warning, failure, muted lipgloss.Style }

// newStyles returns basic ANSI styles, or unstyled ones that render plain text.
// Lip Gloss v2 styles always emit their SGR sequences, so the color decision
// made from the destination stream's capabilities is applied here.
func newStyles(color bool) styles {
	if !color {
		plain := lipgloss.NewStyle()
		return styles{accent: plain, success: plain, warning: plain, failure: plain, muted: plain}
	}
	return styles{
		accent:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		success: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		warning: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		failure: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
		muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
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
