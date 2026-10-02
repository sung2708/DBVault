package mysql

import (
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"strings"
)

func (a *Adapter) ForFullBackup() database.Adapter {
	b := *a
	b.Config.Options = config.Options{Quiesced: a.Config.Options.Quiesced}
	return &b
}

func (a *Adapter) maintenance() *Adapter {
	b := *a
	b.Config.Database = "information_schema"
	return &b
}
func (a *Adapter) PreflightNew(ctx context.Context) (database.Info, error) {
	if err := database.ValidateNewName(a.Config.Database); err != nil {
		return database.Info{}, err
	}
	b := a.maintenance()
	info, err := b.Preflight(ctx)
	if err != nil {
		return info, err
	}
	return info, a.checkAbsent(ctx)
}
func (a *Adapter) checkAbsent(ctx context.Context) error {
	if err := database.ValidateNewName(a.Config.Database); err != nil {
		return err
	}
	b := a.maintenance()
	// Generated and explicit new names are restricted to portable ASCII by CLI.
	name := strings.ReplaceAll(a.Config.Database, "'", "''")
	count, err := b.capture(ctx, "mysql", append(b.args(), "--connect-timeout=10", "--batch", "--skip-column-names", "--execute=SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='"+name+"'"))
	if err == nil && count != "0" {
		err = fmt.Errorf("destination database already exists; choose a different name")
	}
	return err
}
func (a *Adapter) CreateNew(ctx context.Context) error {
	if err := a.checkAbsent(ctx); err != nil {
		return err
	}
	b := a.maintenance()
	name := strings.ReplaceAll(a.Config.Database, "`", "``")
	_, err := b.capture(ctx, "mysql", append(b.args(), "--connect-timeout=10", "--execute=CREATE DATABASE `"+name+"`"))
	return err
}
