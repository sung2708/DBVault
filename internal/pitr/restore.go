package pitr

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/toolresolve"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type RestoreResult struct {
	Engine     string    `json:"engine"`
	TargetTime time.Time `json:"target_time"`
	Directory  string    `json:"directory,omitempty"`
	State      string    `json:"state"`
}

// Restore uses an exclusive fresh directory for PostgreSQL, and an independently
// authenticated empty instance for logical MySQL/MongoDB replay. All archives are
// authenticated before the destination receives any database writes.
func (s *Service) Restore(ctx context.Context, name string, at time.Time, directory string, target *config.Config, confirm bool) (RestoreResult, error) {
	result := RestoreResult{TargetTime: at, State: "not_started"}
	if !confirm {
		return result, fmt.Errorf("native PITR restore requires --confirm")
	}
	if at.IsZero() || at.Nanosecond() != 0 {
		return result, fmt.Errorf("PITR target must be a whole-second RFC3339 timestamp")
	}
	release, err := s.lock(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	at = at.UTC()
	result.TargetTime = at
	chain, e := s.chain(ctx, name)
	if e != nil {
		return result, e
	}
	base := chain[0]
	last := chain[len(chain)-1]
	result.Engine = base.Engine
	if base.Engine != s.Config.Database.Type || at.Before(base.From) || at.After(last.Until) || len(chain) < 2 {
		return result, fmt.Errorf("target time is outside the verified captured log range")
	}
	stage, e := os.MkdirTemp("", "dbvault-native-restore-*")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(stage)
	for i, m := range chain {
		dir := filepath.Join(stage, strconv.Itoa(i))
		if e = os.Mkdir(dir, 0700); e != nil {
			return result, e
		}
		if e = s.unpack(ctx, m, dir); e != nil {
			return result, e
		}
	}
	if base.Engine == "postgres" {
		if target != nil || directory == "" {
			return result, fmt.Errorf("PostgreSQL PITR requires a fresh --directory")
		}
		return s.preparePostgres(ctx, result, chain, stage, directory)
	}
	if target == nil || directory != "" || target.Database.Type != base.Engine {
		return result, fmt.Errorf("MySQL/MongoDB PITR requires --target-config for a fresh independent instance")
	}
	password, e := target.Password()
	if e != nil {
		return result, e
	}
	dest := &Service{Config: *target, Password: password, Runner: s.Runner}
	if base.Engine == "mysql" {
		id, version, e := dest.sqlIdentity(ctx)
		if e != nil {
			return result, e
		}
		if id == base.Identity {
			return result, fmt.Errorf("cannot replay PITR into the source instance")
		}
		if releaseSeries(version) != releaseSeries(base.ServerVersion) {
			return result, fmt.Errorf("MySQL PITR requires the same server release series")
		}
		mode, err := dest.query(ctx, "SELECT @@GTID_MODE")
		if err != nil {
			return result, err
		}
		if base.GTIDMode != "" && mode != base.GTIDMode {
			return result, fmt.Errorf("MySQL recovery target must match the baseline GTID mode")
		}
		visibility, err := dest.query(ctx, `SELECT COUNT(*) FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE(SUBSTRING_INDEX(CURRENT_USER(),'@',1)),'@',QUOTE(SUBSTRING_INDEX(CURRENT_USER(),'@',-1))) AND PRIVILEGE_TYPE='SHOW DATABASES'`)
		if err != nil {
			return result, err
		}
		if visibility != "1" {
			return result, fmt.Errorf("MySQL recovery account needs a direct global SHOW DATABASES privilege to prove the target is empty")
		}
		count, e := dest.query(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name NOT IN ('mysql','sys','performance_schema','information_schema')")
		if e != nil {
			return result, e
		}
		if count != "0" {
			return result, fmt.Errorf("PITR target must have no user databases")
		}
		sql, e := os.CreateTemp("", "dbvault-pitr-replay-*.sql")
		if e != nil {
			return result, e
		}
		defer func() { sql.Close(); os.Remove(sql.Name()) }()
		if _, e = io.WriteString(sql, "SET SESSION sql_log_bin=0;\n"); e != nil {
			return result, e
		}
		f, e := os.Open(filepath.Join(stage, "0", "base.sql"))
		if e != nil {
			return result, e
		}
		_, e = io.Copy(sql, f)
		f.Close()
		if e != nil {
			return result, e
		}
		file, pos, ok := strings.Cut(base.End, ":")
		position, e := strconv.ParseUint(pos, 10, 64)
		if !ok || e != nil || position < 4 || !binlogName.MatchString(file) {
			return result, fmt.Errorf("invalid base binary log coordinate")
		}
		args := []string{"--no-defaults", "--no-login-paths", "--verify-binlog-checksum", "--disable-log-bin", "--start-position=" + pos, "--stop-datetime=" + at.Format("2006-01-02 15:04:05")}
		for i := 1; i < len(chain); i++ {
			names, e := os.ReadDir(filepath.Join(stage, strconv.Itoa(i)))
			if e != nil {
				return result, e
			}
			for _, entry := range names {
				if !binlogName.MatchString(entry.Name()) || entry.IsDir() {
					return result, fmt.Errorf("invalid archived binary log")
				}
				args = append(args, filepath.Join(stage, strconv.Itoa(i), entry.Name()))
			}
		}
		if len(args) == 6 {
			return result, fmt.Errorf("PITR chain contains no binary logs")
		}
		tool, e := toolresolve.Resolve("mysqlbinlog", s.Config.Database.Tools["mysqlbinlog"], s.Runner)
		if e != nil {
			return result, e
		}
		if e = s.Runner.Run(ctx, runner.Spec{Executable: tool, Args: args, Env: map[string]string{"TZ": "UTC"}, Stdout: sql}); e != nil {
			return result, e
		}
		if _, e = sql.Seek(0, 0); e != nil {
			return result, e
		}
		result.State = "replaying"
		// The full baseline and all log ranges share one connection, so restoring
		// source account tables cannot interrupt an authenticated replay session.
		if e = dest.run(ctx, "mysql", []string{"--binary-mode"}, sql, io.Discard); e != nil {
			return result, e
		}
	} else {
		c, e := dest.mongoClient()
		if e != nil {
			return result, e
		}
		defer closeMongo(ctx, c)
		id, version, e := mongoIdentity(ctx, c)
		if e != nil {
			return result, e
		}
		sourceParts := strings.Split(base.Identity, "/")
		targetParts := strings.Split(id, "/")
		if len(sourceParts) != 3 || len(targetParts) != 3 {
			return result, fmt.Errorf("invalid replica set identity")
		}
		if targetParts[1] == sourceParts[1] {
			return result, fmt.Errorf("cannot replay PITR into the source replica set")
		}
		if releaseSeries(version) != releaseSeries(base.ServerVersion) {
			return result, fmt.Errorf("MongoDB PITR requires the same server release series")
		}
		names, e := c.ListDatabaseNames(ctx, bson.D{}, options.ListDatabases().SetAuthorizedDatabases(false))
		if e != nil {
			return result, e
		}
		for _, name := range names {
			if name != "admin" && name != "config" && name != "local" {
				return result, fmt.Errorf("PITR target must have no user databases")
			}
		}
		// Pre-assemble one oplog range. mongorestore owns transaction and command
		// replay; the application never translates BSON into approximate writes.
		file, e := os.CreateTemp("", "dbvault-pitr-oplog-*.bson")
		if e != nil {
			return result, e
		}
		defer func() { file.Close(); os.Remove(file.Name()) }()
		for i := 1; i < len(chain); i++ {
			src, e := os.Open(filepath.Join(stage, strconv.Itoa(i), "oplog.bson"))
			if e != nil {
				return result, e
			}
			_, e = io.Copy(file, src)
			src.Close()
			if e != nil {
				return result, e
			}
		}
		if e = file.Close(); e != nil {
			return result, e
		}
		result.State = "replaying"
		if e = dest.run(ctx, "mongorestore", []string{"--archive=" + filepath.Join(stage, "0", "base.archive"), "--oplogReplay"}, nil, io.Discard); e != nil {
			return result, e
		}
		empty := filepath.Join(stage, "empty")
		if e = os.Mkdir(empty, 0700); e != nil {
			return result, e
		}
		if e = dest.run(ctx, "mongorestore", []string{"--dir=" + empty, "--oplogReplay", "--oplogFile=" + file.Name(), "--oplogLimit=" + strconv.FormatInt(at.Unix(), 10) + ":0"}, nil, io.Discard); e != nil {
			return result, e
		}
	}
	result.State = "restored"
	return result, nil
}
func releaseSeries(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return v
	}
	return strings.Join(parts[:2], ".")
}

func (s *Service) preparePostgres(ctx context.Context, result RestoreResult, chain []Record, stage, directory string) (RestoreResult, error) {
	absolute, e := filepath.Abs(directory)
	if e != nil {
		return result, e
	}
	absolute = filepath.Clean(absolute)
	// Both SQL and the server's restore_command parse this generated path.
	if strings.ContainsAny(absolute, "'\"$`\r\n%!&|<>^") {
		return result, fmt.Errorf("unsafe PostgreSQL recovery directory path")
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(absolute))
	if e != nil {
		return result, e
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	if strings.ContainsAny(absolute, "'\"$`\r\n%!&|<>^") {
		return result, fmt.Errorf("unsafe resolved PostgreSQL recovery directory path")
	}
	if e = os.Mkdir(absolute, 0700); e != nil {
		return result, fmt.Errorf("recovery directory must not exist: %w", e)
	}
	result.Directory = absolute
	result.State = "preparing"
	// No recursive cleanup on failure: preserve the private partial target.
	baseDir := filepath.Join(stage, "0", "data")
	if e = copyTree(ctx, baseDir, absolute); e != nil {
		return result, e
	}
	archive := filepath.Join(absolute, "dbvault_wal_archive")
	if e = os.Mkdir(archive, 0700); e != nil {
		return result, e
	}
	for i := 1; i < len(chain); i++ {
		dir := filepath.Join(stage, strconv.Itoa(i))
		entries, e := os.ReadDir(dir)
		if e != nil {
			return result, e
		}
		for _, entry := range entries {
			if !walName.MatchString(entry.Name()) {
				return result, fmt.Errorf("invalid archived WAL member")
			}
			if e = copyRegular(filepath.Join(dir, entry.Name()), filepath.Join(archive, entry.Name()), chain[0].SegmentSize); e != nil {
				return result, e
			}
		}
	}
	command := `cp -- "` + filepath.ToSlash(archive) + `/%f" "%p"`
	if runtime.GOOS == "windows" {
		command = `copy /Y "` + filepath.ToSlash(archive) + `/%f" "%p"`
	}
	settings := "\n# DBVault verified PITR, target is exclusive\nrestore_command = '" + command + "'\nrecovery_target_time = '" + result.TargetTime.UTC().Format("2006-01-02 15:04:05+00") + "'\nrecovery_target_inclusive = false\nrecovery_target_action = 'pause'\nrecovery_target_timeline = 'current'\n"
	f, e := os.OpenFile(filepath.Join(absolute, "postgresql.auto.conf"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if e != nil {
		return result, e
	}
	_, e = io.WriteString(f, settings)
	ce := f.Close()
	if e != nil {
		return result, e
	}
	if ce != nil {
		return result, ce
	}
	// A base backup must not silently remain a streaming standby.
	if e = os.Remove(filepath.Join(absolute, "standby.signal")); e != nil && !os.IsNotExist(e) {
		return result, e
	}
	f, e = os.OpenFile(filepath.Join(absolute, "recovery.signal"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return result, e
	}
	if e = f.Close(); e != nil {
		return result, e
	}
	result.State = "prepared_requires_server_start"
	return result, nil
}
func copyTree(ctx context.Context, source, dest string) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == source {
			return nil
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.Mkdir(target, 0700)
		}
		return copyRegular(path, target, 0)
	})
}
