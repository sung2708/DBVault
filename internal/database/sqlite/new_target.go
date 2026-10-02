package sqlite

import (
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/database"
	"os"
)

func (a *Adapter) ForFullBackup() database.Adapter { b := *a; return &b }

func (a *Adapter) PreflightNew(ctx context.Context) (database.Info, error) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_, err := os.Lstat(a.Config.Database + suffix)
		if err == nil {
			return database.Info{}, fmt.Errorf("destination file or SQLite sidecar already exists")
		}
		if !os.IsNotExist(err) {
			return database.Info{}, err
		}
	}
	return a.PreflightRecovery(ctx)
}
func (a *Adapter) CreateNew(ctx context.Context) error {
	if _, err := a.PreflightNew(ctx); err != nil {
		return err
	}
	f, err := os.OpenFile(a.Config.Database, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	db, err := open(a.Config.Database, "rw")
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "PRAGMA user_version=0")
	return err
}
