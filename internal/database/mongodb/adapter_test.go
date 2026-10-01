package mongodb

import (
	"context"
	"fmt"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"strings"
	"testing"
)

type fake struct {
	paths    []string
	specs    []runner.Spec
	password string
}

func (f *fake) LookPath(s string) (string, error) { return s, nil }
func (f *fake) Run(_ context.Context, s runner.Spec) error {
	f.specs = append(f.specs, s)
	if len(s.Args) == 1 && s.Args[0] == "--version" {
		fmt.Fprint(s.Stdout, s.Executable+" version: 100.19.0")
		return nil
	}
	for _, arg := range s.Args {
		if strings.HasPrefix(arg, "--config=") {
			path := strings.TrimPrefix(arg, "--config=")
			f.paths = append(f.paths, path)
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var cfg map[string]string
			if err = yaml.Unmarshal(b, &cfg); err != nil {
				return err
			}
			if cfg["password"] != f.password {
				return fmt.Errorf("credential file incorrect")
			}
		}
	}
	if s.Stdin != nil {
		_, err := io.Copy(io.Discard, s.Stdin)
		return err
	}
	return nil
}
func TestSafeArchiveCommands(t *testing.T) {
	secret := "SUPER_SECRET_DB_PASSWORD_12345"
	f := &fake{password: secret}
	a := &Adapter{Config: config.Database{Host: "localhost", Port: 27017, User: "operator", Database: "source", SSLMode: "disable", Options: config.Options{Quiesced: true, IncludeCollections: []string{"$(inert)"}}}, Password: secret, Runner: f, Probe: func(context.Context) (string, error) { return "8.0.1", nil }}
	info, err := a.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Dump(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	a.Config.Database = "target"
	if err = a.Restore(context.Background(), strings.NewReader("archive"), database.RestoreOptions{SourceDatabase: "source", Collections: []string{"$(inert)"}, Clean: true}); err != nil {
		t.Fatal(err)
	}
	for _, s := range f.specs {
		if strings.Contains(strings.Join(s.Args, " "), secret) {
			t.Fatal("password in argv")
		}
	}
	for _, p := range f.paths {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("credential file survived", p)
		}
	}
	if err = a.Compatible(info, "8.0.2", "100.19.0"); err != nil {
		t.Fatal(err)
	}
	if err = a.Compatible(info, "7.0.1", "100.19.0"); err == nil {
		t.Fatal("cross-major restore accepted")
	}
	a.Config.Options.Quiesced = false
	if err = a.Dump(context.Background(), io.Discard); err == nil {
		t.Fatal("unquiesced snapshot accepted")
	}
}
