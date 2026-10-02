package postgres

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

func (a *Adapter) maintenance() *Adapter { b := *a; b.Config.Database = "postgres"; return &b }
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
	name := strings.ReplaceAll(a.Config.Database, "'", "''")
	count, err := a.maintenance().capture(ctx, "psql", "--no-psqlrc", "--no-password", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=SELECT count(*) FROM pg_database WHERE datname='"+name+"'")
	if err == nil && count != "0" {
		err = fmt.Errorf("destination database already exists; choose a different name")
	}
	return err
}
func (a *Adapter) CreateNew(ctx context.Context) error {
	if err := a.checkAbsent(ctx); err != nil {
		return err
	}
	name := strings.ReplaceAll(a.Config.Database, `"`, `""`)
	_, err := a.maintenance().capture(ctx, "psql", "--no-psqlrc", "--no-password", "--set=ON_ERROR_STOP=1", `--command=CREATE DATABASE "`+name+`" TEMPLATE template0`)
	return err
}
