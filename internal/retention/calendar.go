package retention

import (
	"fmt"
	"time"

	"github.com/sung2708/DBVault/internal/config"
)

// Dated is one retention unit: a backup, or a native recovery chain.
type Dated struct {
	Name string
	At   time.Time
}

// Calendar returns the names protected by grandfather-father-son rules. Items
// must be ordered newest first. For each enabled rule it keeps the newest item
// in each of the most recent N UTC periods (day, ISO week, month, year) that
// contain an item; periods without backups do not use up the count.
func Calendar(items []Dated, p config.Retention) map[string]bool {
	kept := map[string]bool{}
	rules := []struct {
		count int
		key   func(time.Time) string
	}{
		{p.KeepDaily, func(t time.Time) string { return t.Format("2006-01-02") }},
		{p.KeepWeekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%04d-W%02d", y, w) }},
		{p.KeepMonthly, func(t time.Time) string { return t.Format("2006-01") }},
		{p.KeepYearly, func(t time.Time) string { return t.Format("2006") }},
	}
	for _, rule := range rules {
		if rule.count <= 0 {
			continue
		}
		seen := map[string]bool{}
		for _, item := range items {
			key := rule.key(item.At.UTC())
			if seen[key] {
				continue
			}
			if len(seen) == rule.count {
				break
			}
			seen[key] = true
			kept[item.Name] = true
		}
	}
	return kept
}
