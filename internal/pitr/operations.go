package pitr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sung2708/DBVault/internal/config"
)

// Storage publication is exclusive across processes, including restore readers.
// Never steal a lock left by a crashed owner; operators inspect it first.
func (s *Service) lock(ctx context.Context) (func(), error) {
	const key = "dbvault_pitr.lock"
	if err := s.Store.Put(ctx, key, bytes.NewReader([]byte("DBVault native chain operation in progress\n")), -1); err != nil {
		return nil, fmt.Errorf("native chain locked or storage unavailable; inspect dbvault_pitr.lock after a crashed operation: %w", err)
	}
	return func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = s.Store.Delete(c, key)
	}, nil
}

// Backup starts a baseline when none exists, otherwise captures from the newest
// chain belonging to the authenticated source. Errors never silently reset it.
func (s *Service) Backup(ctx context.Context, kind string, baseEvery time.Duration) (Record, error) {
	if s.Config.PITR == nil || (s.Config.Database.Type != "postgres" && s.Config.Database.Type != "mysql" && s.Config.Database.Type != "mongodb") {
		return Record{}, fmt.Errorf("native backup requires pitr configuration and PostgreSQL, MySQL or MongoDB")
	}
	if kind != "" && kind != "full" && kind != "incremental" {
		return Record{}, fmt.Errorf("native backup type must be full or incremental")
	}
	if baseEvery < 0 {
		return Record{}, fmt.Errorf("base interval must be nonnegative")
	}
	release, err := s.lock(ctx)
	if err != nil {
		return Record{}, err
	}
	defer release()
	if kind == "full" {
		return s.base(ctx)
	}
	var identity string
	if s.Config.Database.Type == "mongodb" {
		client, e := s.mongoClient()
		if e != nil {
			return Record{}, e
		}
		defer closeMongo(ctx, client)
		identity, _, err = mongoIdentity(ctx, client)
	} else {
		identity, _, err = s.sqlIdentity(ctx)
	}
	if err != nil {
		return Record{}, err
	}
	items, err := s.List(ctx)
	if err != nil {
		return Record{}, err
	}
	selected, selectedBase, err := selectParent(items, identity)
	if err != nil {
		return Record{}, err
	}
	if selected.Name == "" || (baseEvery > 0 && time.Since(selectedBase) >= baseEvery) {
		return s.base(ctx)
	}
	return s.capture(ctx, selected.Name)
}

// Validate the graph once and cache ancestry to avoid quadratic storage reads
// when selecting a parent from a long native log history.
func selectParent(items []Record, identity string) (Record, time.Time, error) {
	if _, err := SelectCleanup(items, config.Retention{}, time.Now()); err != nil {
		return Record{}, time.Time{}, err
	}
	byName := map[string]Record{}
	for _, m := range items {
		byName[m.Name] = m
	}
	roots := map[string]string{}
	depths := map[string]int{}
	var root func(string) string
	root = func(name string) string {
		if r, ok := roots[name]; ok {
			return r
		}
		m := byName[name]
		if m.Kind == "base" {
			roots[name] = name
		} else {
			roots[name] = root(m.Parent)
			depths[name] = depths[m.Parent] + 1
		}
		return roots[name]
	}
	var selected Record
	var baseTime time.Time
	for _, m := range items {
		if m.Identity != identity {
			continue
		}
		at := byName[root(m.Name)].Until
		if selected.Name == "" || at.After(baseTime) || (at.Equal(baseTime) && (m.Until.After(selected.Until) || (m.Until.Equal(selected.Until) && (depths[m.Name] > depths[selected.Name] || (depths[m.Name] == depths[selected.Name] && m.Name < selected.Name))))) {
			selected, baseTime = m, at
		}
	}
	return selected, baseTime, nil
}

// SelectCleanup retains whole baseline chains. Count is a count of independent
// recovery chains, not log objects; day retention uses each chain's newest end.
func SelectCleanup(items []Record, policy config.Retention, now time.Time) ([]Record, error) {
	if policy.KeepDays < 0 || policy.KeepCount < 0 {
		return nil, fmt.Errorf("retention values must be nonnegative")
	}
	byName := map[string]Record{}
	for _, m := range items {
		if err := m.validate(); err != nil {
			return nil, err
		}
		if _, exists := byName[m.Name]; exists {
			return nil, fmt.Errorf("duplicate native record")
		}
		byName[m.Name] = m
	}
	root := map[string]string{}
	depth := map[string]int{}
	state := map[string]int{}
	var visit func(string, int) error
	visit = func(name string, n int) error {
		if n >= 1024 || state[name] == 1 {
			return fmt.Errorf("cyclic or oversized native chain; cleanup aborted")
		}
		if state[name] == 2 {
			return nil
		}
		m, ok := byName[name]
		if !ok {
			return fmt.Errorf("missing native parent; cleanup aborted")
		}
		state[name] = 1
		if m.Kind == "base" {
			root[name] = name
		} else {
			if err := visit(m.Parent, n+1); err != nil {
				return err
			}
			p := byName[m.Parent]
			if p.Hash != m.ParentHash || p.Engine != m.Engine || p.Identity != m.Identity || p.ServerVersion != m.ServerVersion || p.GTIDMode != m.GTIDMode || p.End != m.Start || !p.Until.Equal(m.From) || p.SegmentSize != m.SegmentSize {
				return fmt.Errorf("native continuity mismatch; cleanup aborted")
			}
			root[name], depth[name] = root[m.Parent], depth[m.Parent]+1
			if depth[name] >= 1024 {
				return fmt.Errorf("oversized native chain; cleanup aborted")
			}
		}
		state[name] = 2
		return nil
	}
	tails := map[string]time.Time{}
	groups := map[string][]string{}
	for _, m := range items {
		if err := visit(m.Name, 0); err != nil {
			return nil, err
		}
		r := root[m.Name]
		if m.Until.After(tails[r]) {
			tails[r] = m.Until
		}
		if m.Kind == "base" {
			groups[m.Engine+"\x00"+m.Identity] = append(groups[m.Engine+"\x00"+m.Identity], m.Name)
		}
	}
	result := []Record{}
	if policy.KeepDays == 0 && policy.KeepCount == 0 {
		return result, nil
	}
	selected := map[string]bool{}
	cutoff := now.UTC().AddDate(0, 0, -policy.KeepDays)
	for _, chains := range groups {
		sort.Slice(chains, func(i, j int) bool {
			if byName[chains[i]].Until.Equal(byName[chains[j]].Until) {
				return chains[i] < chains[j]
			}
			return byName[chains[i]].Until.After(byName[chains[j]].Until)
		})
		for i, r := range chains {
			if i == 0 || byName[r].Until.Equal(byName[chains[0]].Until) || i < policy.KeepCount || (policy.KeepDays > 0 && !tails[r].Before(cutoff)) {
				continue
			}
			selected[r] = true
		}
	}
	for _, m := range items {
		if selected[root[m.Name]] {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if depth[result[i].Name] == depth[result[j].Name] {
			return result[i].Name < result[j].Name
		}
		return depth[result[i].Name] > depth[result[j].Name]
	})
	return result, nil
}

// Cleanup verifies every retained and expired archive before deleting any data.
// Dry runs validate the entire dependency graph and never delete archives.
func (s *Service) Cleanup(ctx context.Context, now time.Time, dry bool) ([]Record, error) {
	release, err := s.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	candidates, err := SelectCleanup(items, s.Config.Retention, now)
	if err != nil || dry || len(candidates) == 0 {
		return candidates, err
	}
	for _, m := range items {
		dir, e := os.MkdirTemp("", "dbvault-native-retention-*")
		if e != nil {
			return nil, e
		}
		e = s.unpack(ctx, m, dir)
		removeErr := os.RemoveAll(dir)
		if e != nil {
			return nil, e
		}
		if removeErr != nil {
			return nil, removeErr
		}
	}
	for _, m := range candidates {
		if err = s.Store.Delete(ctx, m.Name); err != nil {
			return nil, err
		}
		if err = s.Store.Delete(ctx, m.Name+".pitr.json"); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}
