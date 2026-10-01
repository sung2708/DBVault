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
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
