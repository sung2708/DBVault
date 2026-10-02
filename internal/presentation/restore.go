package presentation

import (
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"strings"
)

// Preview uses stderr so a machine result remains exactly one JSON value.
func (r *Renderer) RestorePreview(m metadata.Manifest, d config.Database, newDB, safety, clean bool, tables, schemas, collections []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.JSON || r.options.Quiet {
		return
	}
	r.clearLocked()
	style := r.outStyle
	r.outStyle = r.errStyle
	defer func() { r.outStyle = style }()
	var b strings.Builder
	r.title(&b, "header", "Restore plan")
	r.field(&b, "Backup", m.Name)
	r.field(&b, "Created", FormatTime(m.CreatedAt))
	r.field(&b, "Source DB", m.Database.Name)
	r.field(&b, "Destination", d.Database)
	if d.Type != "sqlite" {
		r.field(&b, "Server", fmt.Sprintf("%s:%d", d.Host, d.Port))
	}
	r.field(&b, "Create new", fmt.Sprint(newDB))
	r.field(&b, "Backup first", fmt.Sprint(safety))
	r.field(&b, "Clean/drop", fmt.Sprint(clean))
	if len(tables) > 0 {
		r.field(&b, "Tables", strings.Join(tables, ", "))
	}
	if len(schemas) > 0 {
		r.field(&b, "Schemas", strings.Join(schemas, ", "))
	}
	if len(collections) > 0 {
		r.field(&b, "Collections", strings.Join(collections, ", "))
	}
	fmt.Fprint(r.err, b.String())
}
