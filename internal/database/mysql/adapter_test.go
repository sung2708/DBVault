package mysql

import (
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"io"
	"strings"
	"testing"
)

type fake struct {
	specs            []runner.Spec
	nonTransactional bool
}

func (f *fake) LookPath(s string) (string, error) { return s, nil }
func (f *fake) Run(_ context.Context, s runner.Spec) error {
	f.specs = append(f.specs, s)
	joined := strings.Join(s.Args, " ")
	if strings.Contains(joined, "--version") {
		fmt.Fprint(s.Stdout, "mysql Ver 8.4.8")
	} else if strings.Contains(joined, "SELECT VERSION()") {
		fmt.Fprint(s.Stdout, "8.4.8")
	} else if strings.Contains(joined, "COUNT(*)") {
		if f.nonTransactional {
			fmt.Fprint(s.Stdout, "1")
		} else {
			fmt.Fprint(s.Stdout, "0")
		}
	}
	if s.Stdin != nil {
		_, err := io.Copy(io.Discard, s.Stdin)
		return err
	}
	return nil
}
func TestCommands(t *testing.T) {
	f := &fake{}
	a := &Adapter{Config: config.Database{Host: "localhost", Port: 3306, User: "operator", Database: "db; inert", SSLMode: "require", Options: config.Options{IncludeTables: []string{"$(inert)"}}}, Password: "SUPER_SECRET_DB_PASSWORD_12345", Runner: f}
	info, err := a.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Compatible(info, "8.4.8", "mysqldump Ver 8.4.8"); err != nil {
		t.Fatal(err)
	}
	if err = a.Dump(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err = a.Restore(context.Background(), strings.NewReader("SQL"), database.RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, s := range f.specs {
		if s.Executable == "mysqldump" && strings.Contains(strings.Join(s.Args, " "), "--connect-timeout") {
			t.Fatal("mysqldump does not support mysql's connect-timeout flag")
		}
		if strings.Contains(strings.Join(s.Args, " "), a.Password) || s.Env["MYSQL_PWD"] != a.Password {
			t.Fatal("unsafe credentials")
		}
	}
	f.nonTransactional = true
	if _, err = a.Preflight(context.Background()); err == nil {
		t.Fatal("unsafe table engine accepted")
	}
	if err = a.Restore(context.Background(), strings.NewReader("SQL"), database.RestoreOptions{Clean: true}); err == nil {
		t.Fatal("fake clean restore accepted")
	}
	if err = database.RequireBackup(a, "incremental"); err == nil {
		t.Fatal("fake incremental accepted")
	}
}
func TestSeries(t *testing.T) {
	for _, v := range []string{"MariaDB 10.11.1", "5.7.1", "unrecognized"} {
		if _, err := Series(v); err == nil {
			t.Fatal("unsupported version accepted", v)
		}
	}
}
