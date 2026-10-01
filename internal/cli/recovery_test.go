package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/fault"
)

func TestRecoveryCLIRealSQLite(t *testing.T) {
	for _, mode := range []string{"dry-run", "json", "quiet", "no-color", "same-target", "cleanup", "lost-source"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.sqlite")
			target := filepath.Join(dir, "recovery.sqlite")
			cfgPath := filepath.Join(dir, "dbvault.yaml")
			db, err := sql.Open("sqlite", source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("CREATE TABLE fixture(value TEXT); INSERT INTO fixture VALUES('data')"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			config := "version: '1'\ndatabase:\n  type: sqlite\n  database: " + filepath.ToSlash(source) + "\nstorage:\n  local:\n    path: " + filepath.ToSlash(filepath.Join(dir, "backups")) + "\nhealth:\n  max_backup_age: 12h\n"
			if err := os.WriteFile(cfgPath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			root := New(Build{}, &out, &stderr)
			root.SetArgs([]string{"backup", "--config", cfgPath, "--quiet"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			name := strings.TrimSpace(out.String())
			if mode == "lost-source" {
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			}
			out.Reset()
			stderr.Reset()
			root = New(Build{}, &out, &stderr)
			flags := []string{"recovery", "drill", "--config", cfgPath, "--target", name, "--recovery-database", target}
			switch mode {
			case "dry-run":
				flags = append(flags, "--dry-run", "--json")
			case "same-target":
				flags[len(flags)-1] = source
				flags = append(flags, "--confirm", "--json")
			case "quiet":
				flags = append(flags, "--confirm", "--quiet")
			case "no-color":
				flags = append(flags, "--confirm", "--no-color")
			case "cleanup":
				flags = append(flags, "--confirm", "--cleanup", "--json")
			default:
				flags = append(flags, "--confirm", "--output", "json")
			}
			root.SetArgs(flags)
			c, err := root.ExecuteC()
			wantCode := 0
			if mode == "same-target" {
				wantCode = 1
			}
			if fault.ExitCode(err) != wantCode {
				t.Fatal(err, out.String(), stderr.String())
			}
			if err != nil {
				WriteError(&stderr, c, err)
			}
			if strings.Contains(out.String(), "\x1b[") || strings.Contains(stderr.String(), "\x1b[") {
				t.Fatal("ANSI in redirected results")
			}
			if jsonMode(c) {
				var result app.DrillResult
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err, out.String())
				}
				status := "passed"
				if mode == "dry-run" {
					status = "preflight_passed"
				} else if mode == "same-target" {
					status = "failed"
				}
				if result.Status != status {
					t.Fatal(result)
				}
				if mode != "same-target" && stderr.Len() != 0 {
					t.Fatal(stderr.String())
				}
			} else if !strings.Contains(out.String(), "passed") {
				t.Fatal(out.String())
			}
			if mode == "quiet" && (stderr.Len() != 0 || strings.Contains(out.String(), "[OK]") || strings.Contains(out.String(), "preserved for inspection")) {
				t.Fatal(out.String(), stderr.String())
			}
			if mode == "dry-run" || mode == "cleanup" || mode == "same-target" {
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatal("unexpected target file", err)
				}
			}
		})
	}
}

func TestRecoveryRequiredFlagsAndUnsupportedEngine(t *testing.T) {
	for _, args := range [][]string{{"recovery", "--help"}, {"recovery", "drill", "--help"}, {"recovery", "drill"}, {"recovery", "drill", "--target", "backup"}, {"recovery", "drill", "--target", "backup", "--recovery-database", "recovery.sqlite"}, {"recovery", "drill", "--target", "backup", "--recovery-database", "recovery.sqlite", "--dry-run", "--cleanup"}} {
		var out bytes.Buffer
		root := New(Build{}, &out, &out)
		root.SetArgs(args)
		err := root.Execute()
		if args[len(args)-1] == "--help" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("required guards bypassed", args)
		}
	}
}
