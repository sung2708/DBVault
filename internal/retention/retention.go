// Package retention selects candidates without performing I/O or deletion.
package retention

import (
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/metadata"
	"sort"
	"time"
)

// Select applies both keep policies as protections (union). The newest valid
// backup of each engine/database is always protected. Disabled policies add no
// protection; with both disabled, no deletion occurs.
func Select(backups []metadata.Manifest, p config.Retention, now time.Time) ([]metadata.Manifest, error) {
	if p.KeepDays < 0 || p.KeepCount < 0 {
		return nil, fmt.Errorf("retention values must be nonnegative")
	}
	result := []metadata.Manifest{}
	if p.KeepDays == 0 && p.KeepCount == 0 {
		return result, nil
	}
	groups := map[string][]metadata.Manifest{}
	for _, m := range backups {
		if err := m.Validate(); err != nil {
			return nil, fmt.Errorf("invalid backup manifest; cleanup aborted")
		}
		k := m.Database.Engine + "\x00" + m.Database.Name
		groups[k] = append(groups[k], m)
	}
	for _, items := range groups {
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].Name < items[j].Name
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
		cutoff := now.UTC().AddDate(0, 0, -p.KeepDays)
		for i, m := range items {
			if i == 0 || i < p.KeepCount || (p.KeepDays > 0 && !m.CreatedAt.Before(cutoff)) {
				continue
			}
			result = append(result, m)
		}
	}
	// Protect the complete ancestry of every retained backup. Obsolete chains
	// can be removed in one pass, provided children precede their bases.
	candidates := map[string]bool{}
	byName := map[string]metadata.Manifest{}
	for _, m := range result {
		candidates[m.Name] = true
	}
	for _, m := range backups {
		byName[m.Name] = m
	}
	referenced := map[string]bool{}
	var protect func(string)
	protect = func(name string) {
		if referenced[name] {
			return
		}
		referenced[name] = true
		if m, ok := byName[name]; ok && m.Delta != nil {
			protect(m.Delta.BaseName)
		}
	}
	for _, m := range backups {
		if !candidates[m.Name] {
			protect(m.Name)
		}
	}
	filtered := result[:0]
	for _, m := range result {
		if !referenced[m.Name] {
			filtered = append(filtered, m)
		}
	}
	result = filtered
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	// Topological ordering also detects corrupt cyclic dependencies before
	// cleanup is allowed to delete any object.
	selected := map[string]bool{}
	children := map[string][]string{}
	for _, m := range result {
		selected[m.Name] = true
		if m.Delta != nil {
			children[m.Delta.BaseName] = append(children[m.Delta.BaseName], m.Name)
		}
	}
	state := map[string]int{}
	ordered := make([]metadata.Manifest, 0, len(result))
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("cyclic backup dependency; cleanup aborted")
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, child := range children[name] {
			if selected[child] {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		ordered = append(ordered, byName[name])
		return nil
	}
	for _, m := range result {
		if err := visit(m.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
