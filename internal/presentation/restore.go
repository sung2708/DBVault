package presentation

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
)

// RestorePlan describes a pending restore for human review. Destination.Database
// holds the destination database name (SQLite: file path).
type RestorePlan struct {
	Manifest                         metadata.Manifest
	Destination                      config.Database
	NewDatabase, BackupBefore, Clean bool
	Tables, Schemas, Collections     []string
}

// Fields lists labeled plan values; empty selectors and unknown values are omitted.
func (p RestorePlan) Fields() [][2]string {
	var fields [][2]string
	add := func(label, value string) {
		if value != "" {
			fields = append(fields, [2]string{label, value})
		}
	}
	m := p.Manifest
	add("Backup", m.Name)
	if !m.CreatedAt.IsZero() {
		add("Created", FormatTime(m.CreatedAt))
	}
	source := m.Database.Name
	if m.Database.Engine != "" {
		source = strings.TrimSuffix(Engine(m.Database.Engine)+" / "+m.Database.Name, " / ")
	}
	add("Source", source)
	if m.Pipeline.Stored > 0 {
		add("Stored size", FormatBytes(m.Pipeline.Stored))
	}
	add("Destination", p.Destination.Database)
	if p.Destination.Type != "sqlite" && p.Destination.Host != "" {
		add("Server", fmt.Sprintf("%s:%d", p.Destination.Host, p.Destination.Port))
	}
	if p.NewDatabase {
		add("Mode", "Create new database")
	} else {
		add("Mode", "Restore into existing database")
		if p.BackupBefore {
			add("Safety backup", "Full backup verified before restore")
		} else {
			add("Safety backup", "None")
		}
	}
	if p.Clean {
		add("Clean", "Drop restored objects before recreating them")
	}
	add("Tables", strings.Join(p.Tables, ", "))
	add("Schemas", strings.Join(p.Schemas, ", "))
	add("Collections", strings.Join(p.Collections, ", "))
	return fields
}

// Warnings lists risks the operator should notice before confirming.
func (p RestorePlan) Warnings() []string {
	if !p.Clean {
		return nil
	}
	return []string{"--clean drops existing objects that this backup restores before recreating them in " + p.Destination.Database + "."}
}

// FormatFields lays out labeled values like result records, wrapping long values
// under their column when width is positive.
func FormatFields(fields [][2]string, width int) string {
	var b strings.Builder
	for _, f := range fields {
		prefix := fmt.Sprintf("  %-13s ", f[0])
		value := Sanitize(f[1])
		if width > 0 {
			if w := width - ansi.StringWidth(prefix); w >= 12 {
				value = ansi.Hardwrap(value, w, true)
				value = strings.ReplaceAll(value, "\n", "\n"+strings.Repeat(" ", ansi.StringWidth(prefix)))
			}
		}
		b.WriteString(prefix + value + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// Preview uses stderr so a machine result remains exactly one JSON value.
func (r *Renderer) RestorePreview(p RestorePlan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.JSON || r.options.Quiet {
		return
	}
	r.clearLocked()
	style, caps := r.outStyle, r.outCaps
	r.outStyle, r.outCaps = r.errStyle, r.errCaps
	defer func() { r.outStyle, r.outCaps = style, caps }()
	var b strings.Builder
	r.title(&b, "header", "Restore plan")
	for _, f := range p.Fields() {
		r.field(&b, f[0], f[1])
	}
	for _, w := range p.Warnings() {
		fmt.Fprintln(&b, r.status("warning", w, true))
	}
	fmt.Fprint(r.err, b.String())
}
