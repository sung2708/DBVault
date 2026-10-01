package database

import (
	"context"
	"fmt"
	"io"

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

func RequireBackup(a Adapter, kind string) error {
	if kind != "full" || !a.Capabilities().FullBackup {
		return fault.Wrap(fault.Unsupported, "backup", fmt.Errorf("%s backup is not supported by the configured %s strategy", kind, a.Name()))
	}
	return nil
}
