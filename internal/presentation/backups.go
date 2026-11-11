package presentation

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/onboarding"
)

const (
	pickerGap       = "  "
	pickerMinName   = 24
	pickerMinSource = 10
	pickerMaxSource = 32
	// pickerIndent is the "> " selector drawn before every option.
	pickerIndent = 2
)

// BackupPicker is an aligned backup list. Source is set when every backup comes
// from the same engine/database, which then moves out of the rows.
type BackupPicker struct {
	Header, Source string
	Choices        []onboarding.Choice
}

// BackupChoices renders backups as aligned picker rows that fit width terminal
// columns: created (UTC), engine/database, type, encryption, stored size and
// name. When space runs short the source column shrinks, then the source,
// encryption, type and size columns are dropped in that order. Long names lose
// their middle, never their start or end, and the name is never dropped.
func BackupChoices(items []metadata.Manifest, width int, unicode bool) BackupPicker {
	ellipsis := "..."
	if unicode {
		ellipsis = "…"
	}
	type column struct {
		heading     string
		cells       []string
		width       int
		alignRight  bool
		dropped     bool
		truncatable bool
	}
	created := &column{heading: "CREATED (UTC)"}
	source := &column{heading: "SOURCE", truncatable: true}
	kind := &column{heading: "TYPE"}
	encrypted := &column{heading: "ENC"}
	size := &column{heading: "SIZE", alignRight: true}
	columns := []*column{created, source, kind, encrypted, size}
	names := make([]string, len(items))
	for i, m := range items {
		when := "unknown"
		if !m.CreatedAt.IsZero() {
			when = m.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		}
		backupType := Sanitize(m.BackupType)
		if backupType == "" {
			backupType = "unknown"
		}
		enc := "no"
		if m.Encryption != nil {
			enc = "yes"
		}
		created.cells = append(created.cells, when)
		source.cells = append(source.cells, Sanitize(m.Database.Engine+"/"+m.Database.Name))
		kind.cells = append(kind.cells, backupType)
		encrypted.cells = append(encrypted.cells, enc)
		size.cells = append(size.cells, FormatBytes(m.Pipeline.Stored))
		names[i] = Sanitize(m.Name)
	}
	for _, c := range columns {
		c.width = ansi.StringWidth(c.heading)
		for _, cell := range c.cells {
			c.width = max(c.width, ansi.StringWidth(cell))
		}
	}
	source.width = min(source.width, pickerMaxSource)
	picker := BackupPicker{}
	if len(items) > 0 && allEqual(source.cells) {
		picker.Source = source.cells[0]
		source.dropped = true
	}
	available := width - pickerIndent - 1
	nameWidth := func() int {
		used := 0
		for _, c := range columns {
			if !c.dropped {
				used += c.width + len(pickerGap)
			}
		}
		return available - used
	}
	if short := pickerMinName - nameWidth(); short > 0 {
		source.width = max(min(source.width, pickerMinSource), source.width-short)
	}
	for _, c := range []*column{source, encrypted, kind, size} {
		if nameWidth() >= pickerMinName {
			break
		}
		c.dropped = true
	}
	nw := max(pickerMinName, nameWidth())
	line := func(i int) string {
		var parts []string
		for _, c := range columns {
			if c.dropped {
				continue
			}
			cell := c.heading
			if i >= 0 {
				cell = c.cells[i]
			}
			if c.truncatable {
				cell = ansi.Truncate(cell, c.width, ellipsis)
			}
			if c.alignRight {
				parts = append(parts, padLeft(cell, c.width))
			} else {
				parts = append(parts, pad(cell, c.width))
			}
		}
		name := "NAME"
		if i >= 0 {
			name = TruncateMiddle(names[i], nw, ellipsis)
		}
		return strings.Join(append(parts, name), pickerGap)
	}
	picker.Header = line(-1)
	picker.Choices = make([]onboarding.Choice, len(items))
	for i := range items {
		picker.Choices[i] = onboarding.Choice{Label: line(i), Value: items[i].Name}
	}
	return picker
}

func allEqual(values []string) bool {
	for _, v := range values {
		if v != values[0] {
			return false
		}
	}
	return true
}

// TruncateMiddle keeps the start and end of s within width display columns.
func TruncateMiddle(s string, width int, ellipsis string) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	keep := width - ansi.StringWidth(ellipsis)
	if keep <= 0 {
		return ansi.Truncate(s, width, "")
	}
	runes := []rune(s)
	head, tail := (keep+1)/2, keep/2
	var left, right []rune
	for _, r := range runes {
		if ansi.StringWidth(string(left))+ansi.StringWidth(string(r)) > head {
			break
		}
		left = append(left, r)
	}
	for i := len(runes) - 1; i >= len(left); i-- {
		if ansi.StringWidth(string(right))+ansi.StringWidth(string(runes[i])) > tail {
			break
		}
		right = append([]rune{runes[i]}, right...)
	}
	return string(left) + ellipsis + string(right)
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func padLeft(s string, width int) string {
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(s))) + s
}
