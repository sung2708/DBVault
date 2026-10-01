package postgres

import (
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"io"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	specs  []runner.Spec
	server string
}

func (f *fakeRunner) LookPath(s string) (string, error) { return s, nil }
func (f *fakeRunner) Run(_ context.Context, s runner.Spec) error {
	f.specs = append(f.specs, s)
	if len(s.Args) == 1 && s.Args[0] == "--version" {
		_, err := fmt.Fprint(s.Stdout, s.Executable+" (PostgreSQL) 16.4")
		return err
	}
	if s.Executable == "psql" {
		v := f.server
		if v == "" {
			v = "160004"
		}
		_, err := fmt.Fprint(s.Stdout, v)
		return err
	}
	if s.Stdin != nil {
		_, err := io.Copy(io.Discard, s.Stdin)
		return err
	}
	return nil
}
func TestSafeCommands(t *testing.T) {
	for _, payload := range []string{"db; rm -rf /", "$(evil)", "`evil`", `"; evil`} {
		f := &fakeRunner{}
		a := &Adapter{Config: config.Database{Database: payload, Host: "localhost", User: "operator", Port: 5432, SSLMode: "require", Options: config.Options{IncludeTables: []string{payload}}}, Password: "SUPER_SECRET_DB_PASSWORD_12345", Runner: f}
		if _, err := a.Preflight(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := a.Dump(context.Background(), io.Discard); err != nil {
			t.Fatal(err)
		}
		if err := a.Restore(context.Background(), strings.NewReader("data"), database.RestoreOptions{Clean: true, Tables: []string{payload}}); err != nil {
			t.Fatal(err)
		}
		for _, s := range f.specs {
			if _, ok := s.Env["PGSERVICE"]; ok {
				t.Fatal("PGSERVICE must be removed, not set to an empty service")
			}
			if len(s.UnsetEnv) == 0 {
				t.Fatal("inherited services not cleared")
			}
			if !slices.Contains(s.UnsetEnv, "PGHOSTADDR") {
				t.Fatal("inherited host address can override configured host")
			}
			if strings.Contains(strings.Join(s.Args, " "), a.Password) || s.Executable == "sh" || s.Executable == "cmd" {
				t.Fatal(s)
			}
			if s.Env["PGPASSWORD"] != a.Password {
				t.Fatal("missing isolated credential")
			}
		}
		if err := database.RequireBackup(a, "incremental"); err == nil {
			t.Fatal("fake incremental accepted")
		}
	}
}
func TestVersions(t *testing.T) {
	a := &Adapter{Runner: &fakeRunner{server: "170001"}}
	if _, err := a.Preflight(context.Background()); err == nil {
		t.Fatal("client mismatch accepted")
	}
	if err := a.Compatible(database.Info{ServerVersion: "150001", RestoreToolVersion: "pg_restore (PostgreSQL) 16.4"}, "160004", "pg_dump (PostgreSQL) 16.4"); err == nil {
		t.Fatal("downgrade accepted")
	}
}
