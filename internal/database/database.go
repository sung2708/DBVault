package database

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/metadata"
)

type Capabilities struct{ ConnectionTest, FullBackup, FullRestore, SelectiveBackup, SelectiveRestore, IncrementalBackup, DifferentialBackup, StreamingBackup, RecoveryDrill bool }

type RecoveryValidation struct {
	Method  string `json:"method"`
	Objects int64  `json:"objects"`
	// Rows holds per-table counts of the restored copy. Drill records keep it
	// only when the backup recorded source counts to compare against.
	Rows map[string]int64 `json:"rows,omitempty"`
	// RowCheck compares Rows with the backup's source counts.
	RowCheck *RowCheck `json:"row_check,omitempty"`
}

// RowCheck is the outcome of comparing restored and source row counts.
type RowCheck struct {
	Consistency    string          `json:"consistency"`
	Tables         int             `json:"tables"`
	Matched        int             `json:"matched"`
	Differences    []RowDifference `json:"differences,omitempty"`
	Missing        []string        `json:"missing,omitempty"`
	OmittedDetails int             `json:"omitted_details,omitempty"`
}
type RowDifference struct {
	Table    string `json:"table"`
	Source   int64  `json:"source"`
	Restored int64  `json:"restored"`
}

// RowCounter reports per-table row counts of the configured source.
type RowCounter interface {
	CountRows(context.Context) (map[string]int64, error)
}

// CompareRows checks every source table exists in the restored copy with the
// same count. Only snapshot counts can establish an integrity failure. Changes
// made between the dump and separate counts can explain any mismatch.
func CompareRows(source map[string]int64, consistency string, restored map[string]int64) (RowCheck, error) {
	check := RowCheck{Consistency: consistency, Tables: len(source)}
	names := make([]string, 0, len(source))
	for name := range source {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		got, ok := restored[name]
		switch {
		case !ok:
			check.Missing = append(check.Missing, name)
		case got != source[name]:
			check.Differences = append(check.Differences, RowDifference{Table: name, Source: source[name], Restored: got})
		default:
			check.Matched++
		}
	}
	if consistency != "snapshot" {
		return check, nil
	}
	if len(check.Missing) > 0 {
		return check, fault.Wrap(fault.Integrity, "compare restored row counts", fmt.Errorf("%d source tables are missing after restore", len(check.Missing)))
	}
	if len(check.Differences) > 0 {
		return check, fault.Wrap(fault.Integrity, "compare restored row counts", fmt.Errorf("%d tables differ from snapshot row counts", len(check.Differences)))
	}
	return check, nil
}

// ParsePairs reads alternating name/count fields separated by NUL bytes, as
// printed by psql --field-separator-zero --record-separator-zero.
func ParsePairs(out string) (map[string]int64, error) {
	fields := strings.Split(strings.TrimRight(out, "\x00\n"), "\x00")
	counts := map[string]int64{}
	if len(fields) == 1 && fields[0] == "" {
		return counts, nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("invalid row count output")
	}
	for i := 0; i < len(fields); i += 2 {
		name := strings.TrimLeft(fields[i], "\n")
		n, err := strconv.ParseInt(strings.TrimSpace(fields[i+1]), 10, 64)
		if name == "" || err != nil || n < 0 {
			return nil, fmt.Errorf("invalid row count output")
		}
		counts[name] = n
	}
	if len(counts) > metadata.MaxRowCountTables {
		return nil, fmt.Errorf("too many tables to record row counts")
	}
	return counts, nil
}

type RecoveryValidator interface {
	PreflightRecovery(context.Context) (Info, error)
	ValidateRecovery(context.Context, bool) (RecoveryValidation, error)
}
type Info struct{ ServerVersion, ToolVersion, RestoreToolVersion string }
type RestoreOptions struct {
	Clean           bool
	Tables, Schemas []string
	Collections     []string
	SourceDatabase  string
}
type Adapter interface {
	Name() string
	Format() string
	Extension() string
	Capabilities() Capabilities
	Preflight(context.Context) (Info, error)
	Dump(context.Context, io.Writer) error
	Restore(context.Context, io.Reader, RestoreOptions) error
	Compatible(Info, string, string) error
}

// NewTarget checks a not-yet-existing destination without creating it. Creation
// must refuse existing targets, including a destination created after preview.
type NewTarget interface {
	PreflightNew(context.Context) (Info, error)
	CreateNew(context.Context) error
}

// FullBackupAdapter returns a copy with include/exclude filters removed.
type FullBackupAdapter interface{ ForFullBackup() Adapter }

var portableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func ValidateNewName(name string) error {
	if !portableName.MatchString(name) {
		return fmt.Errorf("new database name must be a lowercase ASCII identifier, at most 63 characters")
	}
	switch name {
	case "postgres", "template0", "template1", "mysql", "information_schema", "performance_schema", "sys", "admin", "local", "config":
		return fmt.Errorf("system database names cannot be used as new destinations")
	}
	return nil
}

func RequireBackup(a Adapter, kind string) error {
	if kind != "full" || !a.Capabilities().FullBackup {
		return fault.Wrap(fault.Unsupported, "backup", fmt.Errorf("%s backup is not supported by the configured %s strategy", kind, a.Name()))
	}
	return nil
}
