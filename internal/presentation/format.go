package presentation

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func FormatBytes(n int64) string {
	if n < 0 {
		return "unknown"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	unit := 0
	value /= 1024
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
func FormatChecksum(s string, full bool) string {
	if full || len(s) <= 20 {
		return "sha256:" + s
	}
	return "sha256:" + s[:12] + "..." + s[len(s)-8:]
}
func Engine(s string) string {
	switch s {
	case "postgres":
		return "PostgreSQL"
	case "mysql":
		return "MySQL"
	case "mongodb":
		return "MongoDB"
	case "sqlite":
		return "SQLite"
	}
	return s
}
func ServerVersion(engine, s string) string {
	if engine == "postgres" {
		if n, e := strconv.Atoi(s); e == nil && n >= 100000 {
			return fmt.Sprintf("%d.%d", n/10000, n%10000)
		}
	}
	return s
}

// Sanitize keeps untrusted metadata and native diagnostics from emitting terminal
// control sequences or injecting extra status lines. JSON handles escapes itself.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, s)
}
