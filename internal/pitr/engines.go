package pitr

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/toolresolve"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"gopkg.in/yaml.v3"
)

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("native diagnostic output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func (s *Service) run(ctx context.Context, tool string, args []string, in io.Reader, out io.Writer) error {
	d := s.Config.Database
	env := map[string]string{"TZ": "UTC"}
	var prefix []string
	unset := []string{"PGSERVICE", "PGSERVICEFILE", "PGOPTIONS", "MYSQL_HOME", "MYSQL_TEST_LOGIN_FILE"}
	switch d.Type {
	case "postgres":
		env["PGPASSWORD"] = s.Password
		env["PGSSLMODE"] = d.SSLMode
		prefix = []string{"--host=" + d.Host, "--port=" + strconv.Itoa(d.Port), "--username=" + d.User, "--no-password"}
	case "mysql":
		env["MYSQL_PWD"] = s.Password
		mode := map[string]string{"disable": "DISABLED", "prefer": "PREFERRED", "require": "REQUIRED", "verify-ca": "VERIFY_CA", "verify-full": "VERIFY_IDENTITY"}[d.SSLMode]
		prefix = []string{"--no-defaults", "--no-login-paths", "--host=" + d.Host, "--port=" + strconv.Itoa(d.Port), "--user=" + d.User, "--ssl-mode=" + mode}
	case "mongodb":
		file, e := os.CreateTemp("", "dbvault-pitr-credentials-*.yaml")
		if e != nil {
			return e
		}
		defer os.Remove(file.Name())
		data, _ := yaml.Marshal(map[string]string{"password": s.Password})
		_, e = file.Write(data)
		ce := file.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		auth := d.AuthDatabase
		if auth == "" {
			auth = "admin"
		}
		prefix = []string{"--config=" + file.Name(), "--host=" + d.Host, "--port=" + strconv.Itoa(d.Port), "--username=" + d.User, "--authenticationDatabase=" + auth}
		if d.SSLMode == "require" || d.SSLMode == "verify-full" {
			prefix = append(prefix, "--ssl")
		}
	}
	path, e := toolresolve.Resolve(tool, d.Tools[tool], s.Runner)
	if e != nil {
		return e
	}
	return s.Runner.Run(ctx, runner.Spec{Executable: path, Args: append(prefix, args...), Env: env, UnsetEnv: unset, Stdin: in, Stdout: out})
}
func (s *Service) query(ctx context.Context, sql string) (string, error) {
	b := &boundedOutput{}
	tool := "mysql"
	args := []string{"--batch", "--skip-column-names", "--execute=" + sql}
	if s.Config.Database.Type == "postgres" {
		tool = "psql"
		args = []string{"--dbname=postgres", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", sql}
	}
	e := s.run(ctx, tool, args, nil, b)
	return strings.TrimSpace(b.String()), e
}

func (s *Service) sqlIdentity(ctx context.Context) (string, string, error) {
	sql := "SELECT @@server_uuid, VERSION()"
	if s.Config.Database.Type == "postgres" {
		sql = "SELECT system_identifier, current_setting('server_version_num') FROM pg_control_system()"
	}
	data, e := s.query(ctx, sql)
	if e != nil {
		return "", "", e
	}
	fields := strings.FieldsFunc(data, func(r rune) bool { return r == '|' || r == '\t' })
	if len(fields) != 2 {
		return "", "", fmt.Errorf("cannot identify native server")
	}
	if s.Config.Database.Type == "mysql" && (!strings.HasPrefix(fields[1], "8.") || strings.Contains(strings.ToLower(fields[1]), "maria")) {
		return "", "", fmt.Errorf("native binlog backup requires Oracle MySQL 8.x")
	}
	return fields[0], fields[1], nil
}
func (s *Service) clock(ctx context.Context) (time.Time, error) {
	sql := "SELECT DATE_FORMAT(UTC_TIMESTAMP(6),'%Y-%m-%dT%H:%i:%s.%fZ')"
	if s.Config.Database.Type == "postgres" {
		sql = "SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"')"
	}
	v, e := s.query(ctx, sql)
	if e != nil {
		return time.Time{}, e
	}
	return time.Parse(time.RFC3339Nano, v)
}

var mysqlCoordinate = regexp.MustCompile(`(?:SOURCE|MASTER)_LOG_FILE='([^']+)', (?:SOURCE|MASTER)_LOG_POS=([0-9]+)`)
var binlogName = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[0-9]{6,}$`)
var walName = regexp.MustCompile(`^[0-9A-F]{24}$`)
var pgLabel = regexp.MustCompile(`START WAL LOCATION: [^\n]+\(file ([0-9A-F]{24})\)`)

func (s *Service) requireTools(ctx context.Context, base bool) error {
	tools := []string{}
	switch s.Config.Database.Type {
	case "postgres":
		tools = []string{"psql"}
		if base {
			tools = append(tools, "pg_basebackup")
		}
	case "mysql":
		tools = []string{"mysql", "mysqlbinlog"}
		if base {
			tools = append(tools, "mysqldump")
		}
	case "mongodb":
		if base {
			tools = []string{"mongodump", "mongorestore"}
		}
	default:
		return fmt.Errorf("native PITR supports PostgreSQL, MySQL and MongoDB")
	}
	versions := map[string]string{}
	for _, name := range tools {
		path, err := toolresolve.Resolve(name, s.Config.Database.Tools[name], s.Runner)
		if err != nil {
			return err
		}
		out := &boundedOutput{}
		if err = s.Runner.Run(ctx, runner.Spec{Executable: path, Args: []string{"--version"}, Stdout: out}); err != nil {
			return err
		}
		versions[name] = out.String()
	}
	if base && s.Config.Database.Type == "mongodb" {
		pattern := regexp.MustCompile(`\b100\.\d+\.\d+`)
		dump, restore := pattern.FindString(versions["mongodump"]), pattern.FindString(versions["mongorestore"])
		if dump == "" || dump != restore {
			return fmt.Errorf("native MongoDB PITR requires matching Database Tools 100.x")
		}
	}
	return nil
}

func (s *Service) Base(ctx context.Context) (Record, error) {
	release, err := s.lock(ctx)
	if err != nil {
		return Record{}, err
	}
	defer release()
	return s.base(ctx)
}

func (s *Service) base(ctx context.Context) (Record, error) {
	m := Record{Engine: s.Config.Database.Type, Kind: "base"}
	if s.Config.PITR == nil {
		return m, fmt.Errorf("configure pitr before native backup")
	}
	options := s.Config.Database.Options
	if len(options.IncludeTables)+len(options.ExcludeTables)+len(options.IncludeCollections)+len(options.ExcludeCollections)+len(options.ExtraFlags) > 0 {
		return m, fmt.Errorf("native PITR baselines require full instance scope without selectors or extra flags")
	}
	if err := s.requireTools(ctx, true); err != nil {
		return m, err
	}
	dir, e := os.MkdirTemp("", "dbvault-native-base-*")
	if e != nil {
		return m, e
	}
	defer os.RemoveAll(dir)
	if m.Engine == "mongodb" {
		return s.mongoBase(ctx, m, dir)
	}
	if m.Engine != "postgres" && m.Engine != "mysql" {
		return m, fmt.Errorf("PITR supports PostgreSQL, MySQL and MongoDB")
	}
	m.Identity, m.ServerVersion, e = s.sqlIdentity(ctx)
	if e != nil {
		return m, e
	}
	switch m.Engine {
	case "postgres":
		v, e := s.query(ctx, "SELECT pg_size_bytes(current_setting('wal_segment_size'))")
		if e != nil {
			return m, e
		}
		m.SegmentSize, e = strconv.ParseInt(v, 10, 64)
		if e != nil {
			return m, e
		}
		dest := filepath.Join(dir, "data")
		if e = s.run(ctx, "pg_basebackup", []string{"--pgdata=" + dest, "--format=plain", "--wal-method=stream", "--checkpoint=fast", "--no-sync"}, nil, io.Discard); e != nil {
			return m, e
		}
		label, e := os.ReadFile(filepath.Join(dest, "backup_label"))
		if e != nil {
			return m, e
		}
		match := pgLabel.FindSubmatch(label)
		if len(match) != 2 {
			return m, fmt.Errorf("cannot identify base WAL start")
		}
		m.Start = string(match[1])
		m.End = m.Start
	case "mysql":
		f, e := os.OpenFile(filepath.Join(dir, "base.sql"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return m, e
		}
		defer f.Close()
		m.GTIDMode, e = s.query(ctx, "SELECT @@GTID_MODE")
		if e != nil {
			return m, e
		}
		if m.GTIDMode != "ON" && m.GTIDMode != "OFF" {
			return m, fmt.Errorf("native PITR requires stable MySQL GTID mode ON or OFF")
		}
		// A global read lock provides an instance-wide baseline, including system
		// tables. Incremental captures only read closed binary logs thereafter.
		e = s.run(ctx, "mysqldump", []string{"--all-databases", "--source-data=2", "--lock-all-tables", "--routines", "--events", "--triggers", "--hex-blob", "--no-tablespaces", "--set-gtid-purged=OFF"}, nil, f)
		if e != nil {
			return m, e
		}
		if _, e = f.Seek(0, 0); e != nil {
			return m, e
		}
		head, e := io.ReadAll(io.LimitReader(f, 128<<10))
		if e != nil {
			return m, e
		}
		// Windows directory entries can report a stale file size until its writer
		// closes. Finalize the dump before publish reads TAR header sizes.
		if e = f.Close(); e != nil {
			return m, e
		}
		match := mysqlCoordinate.FindSubmatch(head)
		if len(match) != 3 || !binlogName.Match(match[1]) {
			return m, fmt.Errorf("binary logging must be enabled; dump lacks source coordinates")
		}
		m.Start = string(match[1]) + ":" + string(match[2])
		m.End = m.Start
	}
	m.From, e = s.clock(ctx)
	if e != nil {
		return m, e
	}
	m.Until = m.From
	// Fail if a server was replaced at the source endpoint during the backup.
	id, version, e := s.sqlIdentity(ctx)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("source changed during base backup")
	}
	if m.Engine == "mysql" {
		mode, err := s.query(ctx, "SELECT @@GTID_MODE")
		if err != nil {
			return m, err
		}
		if mode != m.GTIDMode {
			return m, fmt.Errorf("GTID mode changed during baseline")
		}
	}
	return s.publish(ctx, m, dir)
}

func (s *Service) Capture(ctx context.Context, parent string) (Record, error) {
	release, err := s.lock(ctx)
	if err != nil {
		return Record{}, err
	}
	defer release()
	return s.capture(ctx, parent)
}

func (s *Service) capture(ctx context.Context, parent string) (Record, error) {
	chain, e := s.chain(ctx, parent)
	if e != nil {
		return Record{}, e
	}
	last := chain[len(chain)-1]
	if last.Engine != s.Config.Database.Type {
		return Record{}, fmt.Errorf("PITR engine mismatch")
	}
	if err := s.requireTools(ctx, false); err != nil {
		return Record{}, err
	}
	m := Record{Engine: last.Engine, Identity: last.Identity, ServerVersion: last.ServerVersion, GTIDMode: last.GTIDMode, Kind: "logs", Parent: last.Name, ParentHash: last.Hash, Start: last.End, From: last.Until, SegmentSize: last.SegmentSize}
	dir, e := os.MkdirTemp("", "dbvault-native-logs-*")
	if e != nil {
		return m, e
	}
	defer os.RemoveAll(dir)
	if m.Engine == "mongodb" {
		return s.mongoCapture(ctx, m, dir)
	}
	id, version, e := s.sqlIdentity(ctx)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("native source identity/version changed; create a new base")
	}
	m.Until, e = s.clock(ctx)
	if e != nil {
		return m, e
	}
	switch m.Engine {
	case "mysql":
		mode, err := s.query(ctx, "SELECT @@GTID_MODE")
		if err != nil {
			return m, err
		}
		if m.GTIDMode != "" && mode != m.GTIDMode {
			return m, fmt.Errorf("GTID mode changed; create a new baseline")
		}
		if _, e = s.query(ctx, "FLUSH BINARY LOGS"); e != nil {
			return m, e
		}
		logs, e := s.query(ctx, "SHOW BINARY LOGS")
		if e != nil {
			return m, e
		}
		rows := strings.Split(logs, "\n")
		var names []string
		for _, row := range rows {
			fields := strings.Fields(row)
			if len(fields) < 2 || !binlogName.MatchString(fields[0]) {
				return m, fmt.Errorf("invalid binary log inventory")
			}
			names = append(names, fields[0])
		}
		start, _, ok := strings.Cut(m.Start, ":")
		if !ok {
			return m, fmt.Errorf("invalid binary log cursor")
		}
		index := -1
		for i, name := range names {
			if name == start {
				index = i
				break
			}
		}
		if index < 0 {
			return m, fmt.Errorf("binary log gap: previous log was purged")
		}
		if index >= len(names)-1 {
			return m, fmt.Errorf("no closed binary log range")
		}
		for i := index + 1; i < len(names); i++ {
			if !consecutiveBinlog(names[i-1], names[i]) {
				return m, fmt.Errorf("binary log sequence gap")
			}
		}
		for _, name := range names[index : len(names)-1] {
			if e = s.run(ctx, "mysqlbinlog", []string{"--read-from-remote-server", "--raw", "--result-file=" + dir + string(os.PathSeparator), name}, nil, io.Discard); e != nil {
				return m, e
			}
		}
		m.End = names[len(names)-1] + ":4"
	case "postgres":
		if s.Config.PITR == nil || s.Config.PITR.ArchiveDirectory == "" {
			return m, fmt.Errorf("PostgreSQL capture requires a local archive_directory fed by archive_command")
		}
		boundary, e := s.query(ctx, "SELECT pg_walfile_name(pg_switch_wal())")
		if e != nil {
			return m, e
		}
		if !walName.MatchString(boundary) || !walName.MatchString(m.Start) || boundary[:8] != m.Start[:8] {
			return m, fmt.Errorf("WAL timeline changed; create a new physical base")
		}
		entries, e := os.ReadDir(s.Config.PITR.ArchiveDirectory)
		if e != nil {
			return m, e
		}
		var names []string
		for _, entry := range entries {
			if walName.MatchString(entry.Name()) && entry.Name() >= m.Start && entry.Name() <= boundary {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		if len(names) == 0 || names[0] != m.Start || names[len(names)-1] != boundary {
			return m, fmt.Errorf("WAL archive is incomplete; wait for archive_command and retry")
		}
		for i, name := range names {
			if i > 0 && nextWAL(names[i-1], m.SegmentSize) != name {
				return m, fmt.Errorf("WAL archive gap")
			}
			if e = copyRegular(filepath.Join(s.Config.PITR.ArchiveDirectory, name), filepath.Join(dir, name), m.SegmentSize); e != nil {
				return m, e
			}
		}
		m.End = nextWAL(boundary, m.SegmentSize)
		if m.End == "" {
			return m, fmt.Errorf("invalid WAL segment size")
		}
	}
	id, version, e = s.sqlIdentity(ctx)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("source changed during log capture")
	}
	if m.Engine == "mysql" && m.GTIDMode != "" {
		mode, err := s.query(ctx, "SELECT @@GTID_MODE")
		if err != nil {
			return m, err
		}
		if mode != m.GTIDMode {
			return m, fmt.Errorf("GTID mode changed during log capture")
		}
	}
	return s.publish(ctx, m, dir)
}

func consecutiveBinlog(a, b string) bool {
	ap, an, ok := strings.Cut(a, ".")
	bp, bn, ok2 := strings.Cut(b, ".")
	x, e := strconv.ParseUint(an, 10, 64)
	y, e2 := strconv.ParseUint(bn, 10, 64)
	return ok && ok2 && e == nil && e2 == nil && ap == bp && y == x+1
}
func nextWAL(name string, size int64) string {
	if !walName.MatchString(name) || size < 1<<20 || size > 1<<30 || size&(size-1) != 0 {
		return ""
	}
	log, e := strconv.ParseUint(name[8:16], 16, 32)
	segment, e2 := strconv.ParseUint(name[16:], 16, 32)
	if e != nil || e2 != nil || segment >= uint64((1<<32)/size) {
		return ""
	}
	segment++
	if segment == uint64((1<<32)/size) {
		segment = 0
		log++
	}
	if log > 0xffffffff {
		return ""
	}
	return fmt.Sprintf("%s%08X%08X", name[:8], log, segment)
}
func copyRegular(source, dest string, expected int64) error {
	info, e := os.Lstat(source)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || (expected > 0 && info.Size() != expected) {
		return fmt.Errorf("invalid native log file")
	}
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}

func (s *Service) mongoClient() (*mongo.Client, error) {
	d := s.Config.Database
	auth := d.AuthDatabase
	if auth == "" {
		auth = "admin"
	}
	o := options.Client().SetHosts([]string{net.JoinHostPort(d.Host, strconv.Itoa(d.Port))}).SetAuth(options.Credential{Username: d.User, Password: s.Password, AuthSource: auth}).SetServerSelectionTimeout(10 * time.Second)
	if d.SSLMode == "require" || d.SSLMode == "verify-full" {
		o.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return mongo.Connect(o)
}
func closeMongo(ctx context.Context, c *mongo.Client) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.Disconnect(cleanup)
}
func mongoIdentity(ctx context.Context, c *mongo.Client) (string, string, error) {
	var conf struct {
		ID       string `bson:"_id"`
		Settings struct {
			ReplicaSetID bson.ObjectID `bson:"replicaSetId"`
		} `bson:"settings"`
	}
	var build struct {
		Version string `bson:"version"`
	}
	var rbid struct {
		RBID int64 `bson:"rbid"`
	}
	if e := c.Database("local").Collection("system.replset").FindOne(ctx, bson.D{}).Decode(&conf); e != nil {
		return "", "", fmt.Errorf("PITR requires a replica set and access to local.system.replset")
	}
	if e := c.Database("admin").RunCommand(ctx, bson.D{{Key: "replSetGetRBID", Value: 1}}).Decode(&rbid); e != nil {
		return "", "", e
	}
	if e := c.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&build); e != nil {
		return "", "", e
	}
	if conf.ID == "" || conf.Settings.ReplicaSetID == (bson.ObjectID{}) {
		return "", "", fmt.Errorf("replica set identity missing")
	}
	return fmt.Sprintf("%s/%s/%d", conf.ID, conf.Settings.ReplicaSetID.Hex(), rbid.RBID), build.Version, nil
}
func stamp(t bson.Timestamp) string { return fmt.Sprintf("%d:%d", t.T, t.I) }
func parseStamp(v string) (bson.Timestamp, error) {
	a, b, ok := strings.Cut(v, ":")
	t, e := strconv.ParseUint(a, 10, 32)
	i, e2 := strconv.ParseUint(b, 10, 32)
	if !ok || e != nil || e2 != nil {
		return bson.Timestamp{}, fmt.Errorf("invalid oplog cursor")
	}
	return bson.Timestamp{T: uint32(t), I: uint32(i)}, nil
}
func latestStamp(ctx context.Context, c *mongo.Client) (bson.Timestamp, error) {
	var row struct {
		TS bson.Timestamp `bson:"ts"`
	}
	e := c.Database("local").Collection("oplog.rs").FindOne(ctx, bson.D{}, options.FindOne().SetSort(bson.D{{Key: "$natural", Value: -1}})).Decode(&row)
	return row.TS, e
}

func committedStamp(ctx context.Context, c *mongo.Client) (bson.Timestamp, error) {
	var status struct {
		Optimes struct {
			LastCommitted struct {
				TS bson.Timestamp `bson:"ts"`
			} `bson:"lastCommittedOpTime"`
		} `bson:"optimes"`
	}
	if err := c.Database("admin").RunCommand(ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&status); err != nil {
		return bson.Timestamp{}, err
	}
	if status.Optimes.LastCommitted.TS.T == 0 {
		return bson.Timestamp{}, fmt.Errorf("no majority-committed oplog boundary")
	}
	return status.Optimes.LastCommitted.TS, nil
}
func timestampBefore(a, b bson.Timestamp) bool { return a.T < b.T || (a.T == b.T && a.I < b.I) }
func (s *Service) mongoBase(ctx context.Context, m Record, dir string) (Record, error) {
	if !s.Config.PITR.Quiesced {
		return m, fmt.Errorf("MongoDB native baseline requires pitr.quiesced=true and paused application writes")
	}
	c, e := s.mongoClient()
	if e != nil {
		return m, e
	}
	defer closeMongo(ctx, c)
	m.Identity, m.ServerVersion, e = mongoIdentity(ctx, c)
	if e != nil {
		return m, e
	}
	start, e := latestStamp(ctx, c)
	if e != nil {
		return m, e
	}
	committed, err := committedStamp(ctx, c)
	if err != nil {
		return m, err
	}
	if timestampBefore(committed, start) {
		return m, fmt.Errorf("wait for majority commit before creating a quiesced baseline")
	}
	var status struct {
		Transactions struct {
			Open int64 `bson:"currentOpen"`
		} `bson:"transactions"`
	}
	if err = c.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&status); err != nil {
		return m, err
	}
	if status.Transactions.Open != 0 {
		return m, fmt.Errorf("drain open/prepared transactions before creating a baseline")
	}
	f, e := os.OpenFile(filepath.Join(dir, "base.archive"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return m, e
	}
	e = s.run(ctx, "mongodump", []string{"--archive", "--oplog"}, nil, f)
	ce := f.Close()
	if e != nil {
		return m, e
	}
	if ce != nil {
		return m, ce
	}
	end, e := latestStamp(ctx, c)
	if e != nil {
		return m, e
	}
	if err := c.Database("local").Collection("oplog.rs").FindOne(ctx, bson.D{{Key: "ts", Value: start}}).Err(); err != nil {
		return m, fmt.Errorf("oplog start was purged during baseline")
	}
	committed, err = committedStamp(ctx, c)
	if err != nil {
		return m, err
	}
	if timestampBefore(committed, end) {
		return m, fmt.Errorf("baseline oplog boundary is not majority committed")
	}
	if end != start {
		changed, err := c.Database("local").Collection("oplog.rs").CountDocuments(ctx, bson.D{{Key: "ts", Value: bson.D{{Key: "$gt", Value: start}, {Key: "$lte", Value: end}}}, {Key: "op", Value: bson.D{{Key: "$ne", Value: "n"}}}})
		if err != nil || changed != 0 {
			return m, fmt.Errorf("oplog changed during quiesced baseline; pause writes and retry")
		}
	}
	id, version, e := mongoIdentity(ctx, c)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("replica set rollback/version changed during baseline")
	}
	m.Start = stamp(end)
	m.End = m.Start
	m.From = time.Unix(int64(end.T), 0).UTC().Add(time.Second)
	m.Until = m.From
	return s.publish(ctx, m, dir)
}
func (s *Service) mongoCapture(ctx context.Context, m Record, dir string) (Record, error) {
	c, e := s.mongoClient()
	if e != nil {
		return m, e
	}
	defer closeMongo(ctx, c)
	id, version, e := mongoIdentity(ctx, c)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("replica set rollback/identity/version changed; create a new base")
	}
	start, e := parseStamp(m.Start)
	if e != nil {
		return m, e
	}
	oplog := c.Database("local").Collection("oplog.rs")
	retained := func() error {
		if e := oplog.FindOne(ctx, bson.D{{Key: "ts", Value: start}}).Err(); e != nil {
			return fmt.Errorf("oplog gap: previous cursor is no longer retained")
		}
		return nil
	}
	if e = retained(); e != nil {
		return m, e
	}
	end, e := committedStamp(ctx, c)
	if e != nil {
		return m, e
	}
	if end == start {
		return m, fmt.Errorf("no new oplog entries")
	}
	rows, e := oplog.Find(ctx, bson.D{{Key: "ts", Value: bson.D{{Key: "$gt", Value: start}, {Key: "$lte", Value: end}}}}, options.Find().SetSort(bson.D{{Key: "$natural", Value: 1}}))
	if e != nil {
		return m, e
	}
	defer rows.Close(ctx)
	f, e := os.OpenFile(filepath.Join(dir, "oplog.bson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return m, e
	}
	defer f.Close()
	var final bson.Timestamp
	for rows.Next(ctx) {
		raw := rows.Current
		if len(raw) < 5 || int(binary.LittleEndian.Uint32(raw[:4])) != len(raw) {
			return m, fmt.Errorf("invalid oplog BSON")
		}
		var row struct {
			TS bson.Timestamp `bson:"ts"`
		}
		if e = bson.Unmarshal(raw, &row); e != nil {
			return m, e
		}
		final = row.TS
		if _, e = f.Write(raw); e != nil {
			return m, e
		}
	}
	if e = rows.Err(); e != nil {
		return m, e
	}
	if e = f.Close(); e != nil {
		return m, e
	}
	if final != end {
		return m, fmt.Errorf("oplog range changed during capture")
	}
	if e = retained(); e != nil {
		return m, e
	}
	id, version, e = mongoIdentity(ctx, c)
	if e != nil {
		return m, e
	}
	if id != m.Identity || version != m.ServerVersion {
		return m, fmt.Errorf("rollback during oplog capture")
	}
	m.End = stamp(end)
	m.Until = time.Unix(int64(end.T), 0).UTC()
	if m.Until.Before(m.From) {
		return m, fmt.Errorf("oplog range does not cover a new wall-clock second; retry later")
	}
	return s.publish(ctx, m, dir)
}
