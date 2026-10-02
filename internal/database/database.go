package database

import (
	"context"
	"fmt"
	"io"
	"regexp"

	"github.com/sung2708/DBVault/internal/fault"
)

type Capabilities struct{ ConnectionTest, FullBackup, FullRestore, SelectiveBackup, SelectiveRestore, IncrementalBackup, DifferentialBackup, StreamingBackup, RecoveryDrill bool }

type RecoveryValidation struct {
	Method  string `json:"method"`
	Objects int64  `json:"objects"`
}
type RecoveryValidator interface {
	PreflightRecovery(context.Context) (Info, error)
	ValidateRecovery(context.Context) (RecoveryValidation, error)
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
