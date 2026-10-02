package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/storage/local"
)

type drillDocker struct {
	failure, label string
	commands       []string
}

func (*drillDocker) LookPath(string) (string, error) { return "docker", nil }
func (d *drillDocker) Run(ctx context.Context, s runner.Spec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.commands = append(d.commands, s.Args[0])
	out := ""
	switch s.Args[0] {
	case "image":
		out = "sha256:" + strings.Repeat("a", 64)
	case "create":
		for i, arg := range s.Args {
			if arg == "--label" {
				d.label = strings.TrimPrefix(s.Args[i+1], "io.dbvault.recovery=")
			}
		}
		out = strings.Repeat("b", 64)
	case "inspect":
		out = fmt.Sprintf(`[{"Id":%q,"Image":%q,"Config":{"Labels":{"io.dbvault.recovery":%q}},"HostConfig":{"NetworkMode":"none"}}]`, strings.Repeat("b", 64), "sha256:"+strings.Repeat("a", 64), d.label)
	case "rm":
		if d.failure == "cleanup" {
			return errors.New("cleanup failed")
		}
	case "exec":
		joined := strings.Join(s.Args, " ")
		switch {
		case strings.Contains(joined, "--version"):
			out = "PostgreSQL 16.4"
		case strings.Contains(joined, "SHOW server_version_num"):
			out = "160004"
		case strings.Contains(joined, "pg_restore"):
			if _, err := io.Copy(io.Discard, s.Stdin); err != nil {
				return err
			}
			if d.failure == "restore" {
				return errors.New("restore failed")
			}
			if d.failure == "cancel" {
				return context.Canceled
			}
		case strings.Contains(joined, "BEGIN READ ONLY"):
			if d.failure == "validation" {
				return errors.New("validation failed")
			}
			out = "1"
		}
	}
	if s.Stdout != nil {
		_, err := io.WriteString(s.Stdout, out)
		return err
	}
	return nil
}

func postgresDrillFixture(t *testing.T, corrupt bool) (*Service, metadata.Manifest) {
	t.Helper()
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	data := []byte("fixture archive consumed by scripted restore")
	hash := sha256.Sum256(data)
	now := time.Now().UTC()
	id, name, err := metadata.Identity("production", ".dump", now)
	if err != nil {
		t.Fatal(err)
	}
	m := metadata.Manifest{Version: "1.0", ID: id, Name: name, BackupType: "full", CreatedAt: now, CompletedAt: now, Database: metadata.Database{Engine: "postgres", Version: "160004", Name: "production", Format: "custom"}, Pipeline: metadata.Pipeline{Compression: "none", Level: 6, Raw: int64(len(data)), Stored: int64(len(data))}, Checksum: metadata.Checksum{Algorithm: "sha256", Hash: hex.EncodeToString(hash[:])}, Status: "completed", ToolVersion: "pg_dump (PostgreSQL) 16.4", Storage: "local"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if corrupt {
		data[0] ^= 1
	}
	for key, value := range map[string][]byte{name: data, name + ".meta.json": manifest} {
		if err := store.Put(context.Background(), key, bytes.NewReader(value), int64(len(value))); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	cfg.Database = config.Database{Type: "postgres", Database: "production", Host: "unreachable.invalid", User: "production_user"}
	return &Service{Config: cfg, Store: store}, m
}

func TestPostgresDrillFailurePreservationAndEvidence(t *testing.T) {
	for _, failure := range []string{"", "restore", "validation", "cleanup", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			s, m := postgresDrillFixture(t, false)
			d := &drillDocker{failure: failure}
			r, err := s.postgresRecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: "recovered", Confirm: true, Cleanup: true}, d)
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
						t.Fatal("lost cancellation cause", err)
					}
				}
				if err == nil || r.TargetState != "preserved" || !strings.Contains(strings.Join(d.commands, ","), "stop") {
					t.Fatal("failure target not preserved/stopped", r, err, d.commands)
				}
				if failure != "cleanup" && strings.Contains(strings.Join(d.commands, ","), "rm") {
					t.Fatal("failed drill removed target")
				}
			}
			if r.Status != want || r.BackupID != m.ID || r.RecordKey == "" {
				t.Fatal(r, err)
			}
			evidence, err := s.Store.Get(context.Background(), r.RecordKey)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeDrill(evidence)
			evidence.Close()
			if err != nil || decoded.Status != want {
				t.Fatal(decoded, err)
			}
		})
	}
}

func TestPostgresDrillCorruptionAndDryRunNeverCreateContainer(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		s, m := postgresDrillFixture(t, corrupt)
		d := &drillDocker{}
		r, err := s.postgresRecoveryDrill(context.Background(), DrillOptions{Target: m.Name, RecoveryDatabase: "recovered", DryRun: true}, d)
		if corrupt && err == nil {
			t.Fatal("corrupt backup accepted")
		}
		if !corrupt && (err != nil || r.Status != "preflight_passed") {
			t.Fatal(r, err)
		}
		if r.RecordKey != "" || r.RecoveryTarget != "" {
			t.Fatal("dry run created evidence/target", r)
		}
		for _, command := range d.commands {
			if command != "image" {
				t.Fatal("dry run contacted server", d.commands)
			}
		}
		if corrupt && len(d.commands) != 0 {
			t.Fatal("corruption reached Docker", d.commands)
		}
	}
}
