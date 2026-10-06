package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/metadata"
)

type serverDocker struct {
	drillDocker
	engine string
}

func (d *serverDocker) Run(ctx context.Context, s runner.Spec) error {
	if s.Args[0] != "exec" {
		return d.drillDocker.Run(ctx, s)
	}
	d.commands = append(d.commands, "exec")
	joined := strings.Join(s.Args, " ")
	out := ""
	switch {
	case strings.Contains(joined, "/proc/1/comm"):
		if d.engine == "mysql" {
			out = "mysqld"
		} else {
			out = "mongod"
		}
	case strings.Contains(joined, "--version"):
		if d.engine == "mysql" {
			out = "mysql Ver 8.4.0 for Linux"
		} else {
			out = "100.13.0"
		}
	case strings.Contains(joined, "SELECT VERSION()"):
		out = "8.4.0"
	case strings.Contains(joined, "engine <> 'InnoDB'"):
		out = "0"
	case strings.Contains(joined, "buildInfo"):
		out = "8.0.0"
	case strings.Contains(joined, "DBVAULT_COUNT=") || strings.Contains(joined, "CHECK TABLE"):
		if d.failure == "validation" {
			return errors.New("validation failed")
		}
		if d.engine == "mysql" {
			out = "recovered.fixture\tcheck\tstatus\tOK\n1"
		} else {
			out = "DBVAULT_COUNT=1"
		}
	case strings.Contains(joined, "SELECT table_name"):
		out = "fixture"
	case s.Stdin != nil:
		if _, err := io.Copy(io.Discard, s.Stdin); err != nil {
			return err
		}
		if d.failure == "restore" {
			return errors.New("restore failed")
		}
		if d.failure == "cancel" {
			return context.Canceled
		}
	}
	if s.Stdout != nil {
		_, err := io.WriteString(s.Stdout, out)
		return err
	}
	return nil
}
func serverDrillFixture(t *testing.T, engine string, corrupt bool) (*Service, metadata.Manifest) {
	t.Helper()
	s, m := postgresDrillFixture(t, corrupt)
	s.Config.Database.Type = engine
	m.Database.Engine = engine
	if engine == "mysql" {
		m.Database.Format = "sql"
		m.Database.Version = "8.4.0"
		m.ToolVersion = "mysqldump Ver 8.4.0"
	} else {
		m.Database.Format = "archive"
		m.Database.Version = "8.0.0"
		m.ToolVersion = "100.13.0"
	}
	encoded, err := metadata.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.Store.Delete(ctx, m.Name+".meta.json"); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Put(ctx, m.Name+".meta.json", bytes.NewReader(encoded), int64(len(encoded))); err != nil {
		t.Fatal(err)
	}
	return s, m
}
func TestServerDrillFailureEvidenceAndPreservation(t *testing.T) {
	for _, engine := range []string{"mysql", "mongodb"} {
		for _, failure := range []string{"", "restore", "validation", "cleanup", "cancel"} {
			t.Run(engine+"/"+failure, func(t *testing.T) {
				s, m := serverDrillFixture(t, engine, false)
				d := &serverDocker{drillDocker: drillDocker{failure: failure}, engine: engine}
				r, err := s.serverRecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: "recovered", Confirm: true, Cleanup: true}, d)
				want := "failed"
				if failure == "" {
					want = "passed"
					if err != nil || r.TargetState != "removed" {
						t.Fatal(r, err)
					}
				} else {
					if failure == "cancel" {
						want = "cancelled"
						if !errors.Is(err, context.Canceled) {
							t.Fatal(err)
						}
					}
					if err == nil || r.TargetState != "preserved" || !strings.Contains(strings.Join(d.commands, ","), "stop") {
						t.Fatal("failure was not preserved/stopped", r, err)
					}
					if failure != "cleanup" && strings.Contains(strings.Join(d.commands, ","), "rm") {
						t.Fatal("removed failed target")
					}
				}
				if r.Status != want || r.BackupID != m.ID || r.RecordKey == "" {
					t.Fatal(r, err)
				}
				reader, err := s.Store.Get(context.Background(), r.RecordKey)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				if decoded, err := DecodeDrill(reader); err != nil || decoded.Status != want {
					t.Fatal(decoded, err)
				}
			})
		}
	}
}
func TestServerDrillDryRunAndCorruption(t *testing.T) {
	for _, engine := range []string{"mysql", "mongodb"} {
		for _, corrupt := range []bool{false, true} {
			s, m := serverDrillFixture(t, engine, corrupt)
			d := &serverDocker{engine: engine}
			r, err := s.serverRecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: "recovered", DryRun: true}, d)
			if corrupt && err == nil {
				t.Fatal("corrupt accepted")
			}
			if !corrupt && (err != nil || r.Status != "preflight_passed") {
				t.Fatal(r, err)
			}
			if r.RecordKey != "" || r.RecoveryTarget != "" {
				t.Fatal("dry run wrote")
			}
			for _, command := range d.commands {
				if command != "image" {
					t.Fatal("dry run created server", d.commands)
				}
			}
		}
	}
}
