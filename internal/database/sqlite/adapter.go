package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/pipeline"
	"io"
	driver "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Adapter struct{ Config config.Database }

func (*Adapter) Name() string      { return "sqlite" }
func (*Adapter) Format() string    { return "sqlite" }
func (*Adapter) Extension() string { return ".sqlite" }
func (*Adapter) Capabilities() database.Capabilities {
	return database.Capabilities{ConnectionTest: true, FullBackup: true, FullRestore: true, StreamingBackup: false, RecoveryDrill: true}
}
func uri(path, mode string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	q := url.Values{"mode": []string{mode}, "_pragma": []string{"busy_timeout(100)"}}
	u.RawQuery = q.Encode()
	return u.String()
}
func open(path, mode string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", uri(abs, mode))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
func (a *Adapter) Preflight(ctx context.Context) (database.Info, error) {
	info := database.Info{}
	db, err := open(a.Config.Database, "ro")
	if err != nil {
		return info, err
	}
	defer db.Close()
	err = db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&info.ServerVersion)
	info.ToolVersion = "modernc.org/sqlite"
	info.RestoreToolVersion = info.ToolVersion
	return info, fault.Wrap(fault.Connection, "open SQLite database", err)
}
func (a *Adapter) Dump(ctx context.Context, w io.Writer) error {
	db, err := open(a.Config.Database, "ro")
	if err != nil {
		return err
	}
	defer db.Close()
	dir, err := os.MkdirTemp("", "dbvault-sqlite-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "snapshot.sqlite")
	// Parameter binding keeps arbitrary paths inert SQL values. VACUUM INTO is a
	// transactionally consistent SQLite snapshot, including committed WAL pages.
	if _, err = db.ExecContext(ctx, "VACUUM main INTO ?", path); err != nil {
		return err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, pipeline.Reader{Context: ctx, Source: f})
	return err
}
func (a *Adapter) Restore(ctx context.Context, r io.Reader, o database.RestoreOptions) error {
	if o.Clean || len(o.Tables) > 0 || len(o.Schemas) > 0 || len(o.Collections) > 0 {
		return fault.Wrap(fault.Unsupported, "SQLite restore", fmt.Errorf("selective restore and --clean are unsupported; full restore replaces all contents"))
	}
	f, err := os.CreateTemp("", "dbvault-sqlite-restore-*.sqlite")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = io.Copy(f, pipeline.Reader{Context: ctx, Source: r}); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	src, err := open(f.Name(), "ro")
	if err != nil {
		return err
	}
	var check string
	err = src.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check)
	src.Close()
	if err != nil || check != "ok" {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fault.Wrap(fault.Integrity, "validate SQLite image", fmt.Errorf("invalid SQLite snapshot"))
	}
	// Refuse automatic creation: restore targets must be deliberately initialized
	// by the operator. The online backup API preserves SQLite's locking/WAL rules.
	target, err := open(a.Config.Database, "rw")
	if err != nil {
		return err
	}
	defer target.Close()
	conn, err := target.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(raw any) (resultErr error) {
		c, ok := raw.(interface {
			NewRestore(string) (*driver.Backup, error)
		})
		if !ok {
			return fmt.Errorf("SQLite driver lacks online restore API")
		}
		backup, err := c.NewRestore(uri(f.Name(), "ro"))
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, backup.Finish()) }()
		for {
			if err = ctx.Err(); err != nil {
				return err
			}
			more, e := backup.Step(64)
			if e != nil {
				var se *driver.Error
				if !errors.As(e, &se) || (se.Code()&255 != 5 && se.Code()&255 != 6) {
					return e
				}
				timer := time.NewTimer(10 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			if !more {
				return nil
			}
		}
	})
}
func (a *Adapter) Compatible(target database.Info, sourceServer, sourceTool string) error {
	if !strings.HasPrefix(target.ServerVersion, "3.") || !strings.HasPrefix(sourceServer, "3.") {
		return fmt.Errorf("only SQLite 3 snapshots are supported")
	}
	return nil
}

// ValidateRecovery checks the restored image read-only. It does not establish
// application-specific business invariants or that every intended row was dumped.
func (a *Adapter) ValidateRecovery(ctx context.Context) (database.RecoveryValidation, error) {
	result := database.RecoveryValidation{Method: "SQLite PRAGMA integrity_check and sqlite_schema query"}
	db, err := open(a.Config.Database, "ro")
	if err != nil {
		return result, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return result, err
	}
	count := 0
	for rows.Next() {
		var check string
		if err = rows.Scan(&check); err != nil {
			rows.Close()
			return result, err
		}
		if check != "ok" {
			rows.Close()
			return result, fault.Wrap(fault.Integrity, "validate restored SQLite database", fmt.Errorf("integrity_check did not pass"))
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if count != 1 {
		return result, fmt.Errorf("integrity_check returned no reliable result")
	}
	err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&result.Objects)
	return result, err
}

// PreflightRecovery inspects the embedded engine without touching production or
// the not-yet-created target. Actual restore still preflights the created file.
func (a *Adapter) PreflightRecovery(ctx context.Context) (database.Info, error) {
	info := database.Info{ToolVersion: "modernc.org/sqlite", RestoreToolVersion: "modernc.org/sqlite"}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return info, err
	}
	defer db.Close()
	err = db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&info.ServerVersion)
	return info, err
}
