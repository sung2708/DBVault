//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/pitr"
	"github.com/sung2708/DBVault/internal/security"
	"gopkg.in/yaml.v3"
)

// Build a Linux binary before running this Docker-only fixture and point
// DBVAULT_NATIVE_BINARY at it. This exercises the public CLI and vendor tools.
func TestNativePITRDocker(t *testing.T) {
	binary := os.Getenv("DBVAULT_NATIVE_BINARY")
	if binary == "" {
		t.Skip("DBVAULT_NATIVE_BINARY is required for native Linux CLI fixture")
	}
	binary, e := filepath.Abs(binary)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	secret := "native-integration-test-password"
	native := runner.Native{Redactor: security.New(secret)}
	t.Setenv("NATIVE_PASS", secret)
	t.Setenv("NATIVE_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	network := fmt.Sprintf("dbvault-native-%d", time.Now().UnixNano())
	run := func(args []string) (string, error) {
		var out bytes.Buffer
		e := native.Run(ctx, runner.Spec{Executable: "docker", Args: args, Env: map[string]string{"NATIVE_PASS": secret, "NATIVE_KEY": os.Getenv("NATIVE_KEY"), "POSTGRES_PASSWORD": secret, "MYSQL_ROOT_PASSWORD": secret, "MYSQL_PWD": secret}, Stdout: &out})
		return strings.TrimSpace(out.String()), e
	}
	must := func(args ...string) string {
		t.Helper()
		v, e := run(args)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	must("network", "create", network)
	t.Cleanup(func() {
		cleanup, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"network", "rm", network}, Stdout: io.Discard})
	})
	for _, variant := range []string{"mysql", "mysql-gtid", "mongodb", "postgres"} {
		t.Run(variant, func(t *testing.T) {
			engine := variant
			gtid := variant == "mysql-gtid"
			if gtid {
				engine = "mysql"
			}
			must := func(args ...string) string {
				t.Helper()
				value, err := run(args)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			waitNative := func(fn func() error) {
				t.Helper()
				// Windows Docker bind mounts can take several minutes to fsync a
				// physical PostgreSQL cluster before accepting recovery queries.
				deadline := time.Now().Add(5 * time.Minute)
				var last error
				for time.Now().Before(deadline) {
					last = fn()
					if last == nil {
						return
					}
					if strings.Contains(last.Error(), "is not running") {
						t.Fatal(last)
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Second):
					}
				}
				t.Fatal("native engine readiness", last)
			}
			work, err := os.MkdirTemp(filepath.Dir(binary), "native-fixture-"+engine+"-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				rel, err := filepath.Rel(filepath.Dir(binary), work)
				if err != nil || !filepath.IsLocal(rel) {
					t.Error("unsafe fixture cleanup path")
					return
				}
				if err = os.RemoveAll(work); err != nil {
					t.Error(err)
				}
			})
			source := network + "-" + engine
			target := source + "-target"
			image := map[string]string{"mysql": "mysql:8.4", "mongodb": "mongo:8.0", "postgres": "postgres:16-bookworm"}[engine]
			for _, name := range []string{source, target} {
				t.Cleanup(func() {
					cleanup, c := context.WithTimeout(context.Background(), 30*time.Second)
					defer c()
					if t.Failed() {
						var logs bytes.Buffer
						native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"logs", "--tail", "25", name}, Stdout: &logs})
						t.Log(native.Redactor.Text(logs.String()))
					}
					native.Run(cleanup, runner.Spec{Executable: "docker", Args: []string{"rm", "--force", "--volumes", name}, Stdout: io.Discard})
				})
			}
			mount := work + ":/work"
			baseArgs := []string{"run", "--detach", "--tty", "--name", source, "--network", network, "--volume", mount}
			switch engine {
			case "mysql":
				baseArgs = append(baseArgs, "--env", "MYSQL_ROOT_PASSWORD", image)
				if gtid {
					baseArgs = append(baseArgs, "--gtid-mode=ON", "--enforce-gtid-consistency=ON")
				}
			case "postgres":
				os.Mkdir(filepath.Join(work, "wal"), 0700)
				baseArgs = append(baseArgs, "--env", "POSTGRES_PASSWORD", image, "-c", "archive_mode=on", "-c", "archive_command=cp %p /work/wal/%f")
			case "mongodb":
				baseArgs = append(baseArgs, image, "--replSet", "source", "--bind_ip_all")
			}
			must(baseArgs...)
			query := func(name, sql string) (string, error) {
				args := []string{"exec", "--env", "NATIVE_PASS", "--env", "MYSQL_PWD", name}
				switch engine {
				case "mysql":
					args = append(args, "mysql", "--no-defaults", "--no-login-paths", "--host=127.0.0.1", "--user=root", "--ssl-mode=REQUIRED", "--batch", "--skip-column-names", "--execute="+sql)
				case "postgres":
					args = append(args, "psql", "--username=postgres", "--dbname=postgres", "-XAt", "-v", "ON_ERROR_STOP=1", "-c", sql)
				case "mongodb":
					args = append(args, "mongosh", "--quiet", "--norc", "--eval", sql)
				}
				return run(args)
			}
			waitNative(func() error {
				_, e := query(source, map[string]string{"mysql": "SELECT 1", "postgres": "SELECT 1", "mongodb": "db.adminCommand({ping:1})"}[engine])
				return e
			})
			if engine == "postgres" {
				must("exec", source, "chown", "999:999", "/work", "/work/wal")
				must("exec", source, "sh", "-c", `printf '\nhost replication postgres 0.0.0.0/0 scram-sha-256\n' >> "$PGDATA/pg_hba.conf"`)
				if _, err := query(source, "SELECT pg_reload_conf()"); err != nil {
					t.Fatal(err)
				}
			}
			if engine == "mongodb" {
				if _, e = query(source, `rs.initiate({_id:"source",members:[{_id:0,host:"`+source+`:27017"}]})`); e != nil {
					t.Fatal(e)
				}
				waitNative(func() error {
					v, e := query(source, `db.hello().isWritablePrimary`)
					if e != nil {
						return e
					}
					if v != "true" {
						return fmt.Errorf("not primary")
					}
					return nil
				})
				if _, e = query(source, `db.getSiblingDB("admin").createUser({user:"root",pwd:process.env.NATIVE_PASS,roles:["root"]})`); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = query(source, map[string]string{"mysql": "CREATE DATABASE app; CREATE TABLE app.items(id INT PRIMARY KEY); INSERT INTO app.items VALUES(1)", "postgres": "CREATE TABLE items(id INT PRIMARY KEY); INSERT INTO items VALUES(1)", "mongodb": `db.getSiblingDB("app").items.insertOne({_id:1})`}[engine]); e != nil {
				t.Fatal(e)
			}
			cfg := config.Defaults()
			cfg.Version = "1"
			cfg.Database = config.Database{Type: engine, Host: source, Port: map[string]int{"mysql": 3306, "mongodb": 27017, "postgres": 5432}[engine], User: map[string]string{"mysql": "root", "mongodb": "root", "postgres": "postgres"}[engine], PasswordEnv: "NATIVE_PASS", Database: "app", SSLMode: map[string]string{"mysql": "require", "mongodb": "disable", "postgres": "disable"}[engine]}
			cfg.Storage.Local.Path = "/work/storage"
			cfg.PITR = &config.PITR{ArchiveDirectory: "/work/wal", Quiesced: true}
			cfg.Encryption = &config.Encryption{KeyID: "v1", Keys: map[string]string{"v1": "NATIVE_KEY"}}
			save := func(name string, cfg config.Config) {
				data, e := yaml.Marshal(cfg)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(work, name), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			save("source.yaml", cfg)
			cli := func(args ...string) (string, error) {
				toolImage := image
				if engine == "mysql" {
					toolImage = "dbvault:native-mysql-tools"
				}
				command := []string{"run", "--rm", "--network", network, "--volume", mount, "--volume", binary + ":/usr/local/bin/dbvault:ro", "--env", "NATIVE_PASS", "--env", "NATIVE_KEY", "--entrypoint", "/usr/local/bin/dbvault", toolImage, "--config", "/work/source.yaml", "--json", "pitr"}
				return run(append(command, args...))
			}
			data, e := cli("backup", "--type", "incremental")
			if e != nil {
				t.Fatal(e)
			}
			var base pitr.Record
			if json.Unmarshal([]byte(data), &base) != nil {
				t.Fatal(data)
			}
			time.Sleep(2 * time.Second)
			insert := func(id int) {
				t.Helper()
				sql := fmt.Sprintf("INSERT INTO items VALUES(%d)", id)
				if engine == "mysql" {
					sql = fmt.Sprintf("INSERT INTO app.items VALUES(%d)", id)
				}
				if engine == "mongodb" {
					sql = fmt.Sprintf(`db.getSiblingDB("app").items.insertOne({_id:%d})`, id)
				}
				if _, e = query(source, sql); e != nil {
					t.Fatal(e)
				}
			}
			insert(2)
			at := time.Now().UTC().Truncate(time.Second).Add(time.Second)
			time.Sleep(time.Until(at) + 1100*time.Millisecond)
			insert(3)
			var capture pitr.Record
			waitNative(func() error {
				data, e := cli("backup", "--type", "incremental", "--cleanup")
				if e != nil {
					return e
				}
				return json.Unmarshal([]byte(data), &capture)
			})
			if capture.Kind != "logs" || capture.Parent != base.Name {
				t.Fatal("native capture mismatch")
			}
			// Refuse out-of-range time before creating or mutating a target.
			if _, e = cli("restore", "--target", capture.Name, "--time", at.Add(24*time.Hour).Format(time.RFC3339), "--directory", "/work/refused", "--confirm"); e == nil {
				t.Fatal("uncaptured time accepted")
			}
			if engine == "postgres" {
				data, e = cli("restore", "--target", capture.Name, "--time", at.Format(time.RFC3339), "--directory", "/work/recovered", "--confirm")
				if e != nil {
					t.Fatal(e)
				}
				must("run", "--detach", "--tty", "--name", target, "--network", network, "--volume", mount, "--env", "PGDATA=/work/recovered", image)
				waitNative(func() error {
					v, e := query(target, "SELECT pg_is_wal_replay_paused()")
					if e != nil {
						return e
					}
					if v != "t" {
						return fmt.Errorf("PITR target not reached")
					}
					return nil
				})
			} else {
				args := []string{"run", "--detach", "--tty", "--name", target, "--network", network}
				if engine == "mysql" {
					args = append(args, "--env", "MYSQL_ROOT_PASSWORD", image)
					if gtid {
						args = append(args, "--gtid-mode=ON", "--enforce-gtid-consistency=ON")
					}
				} else {
					args = append(args, image, "--replSet", "target", "--bind_ip_all")
				}
				must(args...)
				waitNative(func() error {
					_, e := query(target, map[string]string{"mysql": "SELECT 1", "mongodb": "db.adminCommand({ping:1})"}[engine])
					return e
				})
				if engine == "mongodb" {
					if _, e = query(target, `rs.initiate({_id:"target",members:[{_id:0,host:"`+target+`:27017"}]})`); e != nil {
						t.Fatal(e)
					}
					waitNative(func() error {
						v, e := query(target, `db.hello().isWritablePrimary`)
						if e != nil {
							return e
						}
						if v != "true" {
							return fmt.Errorf("not primary")
						}
						return nil
					})
					if _, e = query(target, `db.getSiblingDB("admin").createUser({user:"root",pwd:process.env.NATIVE_PASS,roles:["root"]})`); e != nil {
						t.Fatal(e)
					}
				}
				destination := cfg
				destination.Database.Host = target
				save("target.yaml", destination)
				data, e = cli("restore", "--target", capture.Name, "--time", at.Format(time.RFC3339), "--target-config", "/work/target.yaml", "--confirm")
				if e != nil {
					t.Fatal(e)
				}
			}
			var result pitr.RestoreResult
			if json.Unmarshal([]byte(data), &result) != nil {
				t.Fatal(data)
			}
			countSQL := map[string]string{"mysql": "SELECT COUNT(*) FROM app.items", "postgres": "SELECT COUNT(*) FROM items", "mongodb": `db.getSiblingDB("app").items.countDocuments({})`}[engine]
			count, e := query(target, countSQL)
			if e != nil || count != "2" {
				t.Fatalf("PITR data: got %q, want 2: %v", count, e)
			}
			idsSQL := map[string]string{"mysql": "SELECT GROUP_CONCAT(id ORDER BY id) FROM app.items", "postgres": "SELECT string_agg(id::text,',' ORDER BY id) FROM items", "mongodb": `db.getSiblingDB("app").items.find().sort({_id:1}).toArray().map(x=>x._id).join(",")`}[engine]
			ids, err := query(target, idsSQL)
			if err != nil || ids != "1,2" {
				t.Fatalf("wrong PITR rows: %q %v", ids, err)
			}
			sourceCount, e := query(source, countSQL)
			if e != nil || sourceCount != "3" {
				t.Fatalf("source mutated: %q %v", sourceCount, e)
			}
			t.Logf("%s restored before exclusive target %s; base=%d bytes, logs=%d bytes", engine, at.Format(time.RFC3339), base.Size, capture.Size)
		})
	}
}
