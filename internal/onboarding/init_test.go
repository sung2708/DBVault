package onboarding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/config"
)

type fakePrompt struct {
	values   map[string]string
	confirms map[string]bool
	calls    []string
	messages []string
	secret   string
	cancel   bool
}

func (p *fakePrompt) answer(title, fallback string) (string, error) {
	p.calls = append(p.calls, title)
	if p.cancel {
		return "", context.Canceled
	}
	if v, ok := p.values[title]; ok {
		return v, nil
	}
	return fallback, nil
}
func (p *fakePrompt) Select(_ context.Context, title string, _ []Choice, fallback string) (string, error) {
	return p.answer(title, fallback)
}
func (p *fakePrompt) Input(_ context.Context, title, fallback string) (string, error) {
	return p.answer(title, fallback)
}
func (p *fakePrompt) Secret(_ context.Context, title string) (string, error) {
	return p.answer(title, p.secret)
}
func (p *fakePrompt) Confirm(_ context.Context, title string, fallback bool) (bool, error) {
	p.calls = append(p.calls, title)
	if p.cancel {
		return false, context.Canceled
	}
	if value, ok := p.confirms[title]; ok {
		return value, nil
	}
	return fallback, nil
}
func (p *fakePrompt) Message(s string) { p.messages = append(p.messages, s) }

func TestGeneratedConfigurations(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "mongodb", "sqlite"} {
		for _, backend := range []string{"local", "s3", "gcs", "azure"} {
			t.Run(engine+"/"+backend, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.yaml")
				p := &fakePrompt{values: map[string]string{
					"Database": engine, "Database name": "production", "SQLite database file": "./app.db", "Username": "dbvault",
					"Where should backups be stored?": backend, "Bucket": "backups", "AWS region": "us-east-1", "Azure container": "backups", "Azure account name": "account",
				}}
				result, err := Run(context.Background(), Options{Path: path, PathProvided: true, Prompt: p})
				if err != nil {
					t.Fatal(err)
				}
				c, err := config.Load(path, config.Overrides{})
				if err != nil {
					t.Fatal(err)
				}
				if c.Database.Type != engine || c.Storage.Type != backend || result.Tested {
					t.Fatal(c, result)
				}
				if engine != "sqlite" && c.Database.Port != config.DefaultPort(engine) {
					t.Fatal("wrong port", c.Database.Port)
				}
				data, _ := os.ReadFile(path)
				if strings.Contains(string(data), "extra_flags") || strings.Contains(string(data), "webhook") {
					t.Fatal("excessive optional fields", string(data))
				}
				if engine == "sqlite" {
					for _, call := range p.calls {
						if call == "Host" || call == "Port" || call == "Username" || strings.Contains(call, "Password") {
							t.Fatal("irrelevant SQLite question", call)
						}
					}
				}
			})
		}
	}
}

func TestFlagsSkipPromptsAndNonInteractive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	v := map[string]string{"database": "postgres", "storage": "local", "database-name": "prod", "user": "backup", "host": "db.example", "password-env": "MY_DB_PASSWORD", "port": "6543", "compression": "zstd"}
	p := &fakePrompt{}
	_, err := Run(context.Background(), Options{Values: v, Path: path, PathProvided: true, Prompt: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range p.calls {
		if call == "Database" || call == "Host" || call == "Port" || call == "Username" || call == "Database name" {
			t.Fatal("flag did not skip prompt", call)
		}
	}
	c, err := config.Load(path, config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Port != 6543 || c.Compression.Type != "zstd" || c.Database.PasswordEnv != "MY_DB_PASSWORD" {
		t.Fatal(c)
	}
	_, err = Run(context.Background(), Options{Values: v, Path: path, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Options{Values: map[string]string{"database": "postgres"}, Path: path})
	if err == nil || !strings.Contains(err.Error(), "--storage") {
		t.Fatal(err)
	}
}

func TestOverwriteAndCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	v := map[string]string{"database": "sqlite", "storage": "local", "database-name": "app.db"}
	if _, err := Run(context.Background(), Options{Values: v, Path: path}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	_, err := Run(context.Background(), Options{Values: v, Path: path})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatal(err)
	}
	p := &fakePrompt{values: map[string]string{"Configuration already exists": "cancel"}}
	_, err = Run(context.Background(), Options{Values: v, Path: path, PathProvided: true, Prompt: p})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("cancel altered config")
	}
	v["database-name"] = "other.db"
	if _, err = Run(context.Background(), Options{Values: v, Path: path, Force: true}); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "new.yaml")
	_, err = Run(context.Background(), Options{Path: missing, Prompt: &fakePrompt{cancel: true}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("cancel created file")
	}
}

func TestTransientPasswordAndTestFailure(t *testing.T) {
	t.Setenv("DBVAULT_DB_PASSWORD", "")
	secret := "transient-never-save-password"
	p := &fakePrompt{secret: secret, confirms: map[string]bool{"Create configuration anyway?": true}}
	v := map[string]string{"database": "postgres", "storage": "local", "database-name": "prod", "user": "backup"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	called := false
	_, err := Run(context.Background(), Options{Values: v, Path: path, PathProvided: true, Prompt: p, Test: true, TestProvided: true, TestConnection: func(_ context.Context, _ config.Config, password string) error {
		called = true
		if password != secret {
			t.Fatal("missing transient password")
		}
		return fmt.Errorf("connection failed with %s", password)
	}})
	if err != nil || !called {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data)+strings.Join(p.messages, "\n"), secret) {
		t.Fatal("secret leaked")
	}
	if os.Getenv("DBVAULT_DB_PASSWORD") != "" {
		t.Fatal("mutated environment")
	}
	_, err = Run(context.Background(), Options{Values: v, Path: filepath.Join(t.TempDir(), "config.yaml"), Test: true})
	if err == nil || !strings.Contains(err.Error(), "environment variable") {
		t.Fatal(err)
	}
}

func TestValidationBeforeWrite(t *testing.T) {
	for _, overrides := range []map[string]string{{"port": "wrong"}, {"port": "70000"}, {"password-env": "not a valid name"}, {"compression": "incremental"}, {"host": "postgres://user:secret@host"}} {
		v := map[string]string{"database": "postgres", "storage": "local", "database-name": "prod", "user": "backup"}
		for k, value := range overrides {
			v[k] = value
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		_, err := Run(context.Background(), Options{Values: v, Path: path})
		if err == nil {
			t.Fatal("accepted invalid flags", overrides)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid config written")
		}
	}
}

func TestNonInteractiveCloudsAndApplicableFlags(t *testing.T) {
	for _, backend := range []string{"local", "s3", "gcs", "azure"} {
		v := map[string]string{"database": "sqlite", "database-name": "app.db", "storage": backend}
		switch backend {
		case "s3":
			v["bucket"] = "backups"
			v["region"] = "us-east-1"
		case "gcs":
			v["bucket"] = "backups"
		case "azure":
			v["container"] = "backups"
			v["account-name"] = "account"
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		if _, err := Run(context.Background(), Options{Values: v, Path: path}); err != nil {
			t.Fatal(backend, err)
		}
		v["user"] = "irrelevant"
		if _, err := Run(context.Background(), Options{Values: v, Path: path, Force: true}); err == nil {
			t.Fatal("accepted SQLite network flag")
		}
	}
}

func TestConnectionSuccessAndExistingReview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	v := map[string]string{"database": "sqlite", "database-name": "app.db", "storage": "local"}
	called := false
	result, err := Run(context.Background(), Options{Values: v, Path: path, Test: true, TestConnection: func(_ context.Context, c config.Config, password string) error {
		called = true
		if c.Database.Type != "sqlite" || password != "" {
			t.Fatal("unexpected SQLite credentials")
		}
		return nil
	}})
	if err != nil || !called || !result.Tested {
		t.Fatal(result, err)
	}
	p := &reviewPrompt{fakePrompt: fakePrompt{}}
	if _, err := Run(context.Background(), Options{Values: v, Path: path, PathProvided: true, Prompt: p}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.messages, "\n"), "Configuration Summary") {
		t.Fatal("existing review was not displayed")
	}
}

func TestDiscoveredToolPathsAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	bin := filepath.Join(t.TempDir(), "PostgreSQL 18", "bin")
	want := map[string]string{"pg_dump": filepath.Join(bin, "pg_dump.exe"), "pg_restore": filepath.Join(bin, "pg_restore.exe"), "psql": filepath.Join(bin, "psql.exe")}
	values := map[string]string{"database": "postgres", "database-name": "prod", "user": "backup", "storage": "local"}
	called := false
	_, err := Run(context.Background(), Options{Values: values, Path: path, DiscoverNativeTools: func(_ context.Context, engine, dir string) (map[string]string, error) {
		called = true
		if engine != "postgres" || dir != "" {
			t.Fatal(engine, dir)
		}
		return want, nil
	}})
	if err != nil || !called {
		t.Fatal(err, called)
	}
	c, err := config.Load(path, config.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	for name, toolPath := range want {
		if c.Database.Tools[name] != toolPath {
			t.Fatalf("%s: %q != %q", name, c.Database.Tools[name], toolPath)
		}
	}
}

func TestPasswordEnvironmentInstructionsByOS(t *testing.T) {
	windows := PasswordEnvironmentInstructions("DB_PASSWORD", "windows")
	if !strings.Contains(windows, "$env:DB_PASSWORD") || !strings.Contains(windows, "Read-Host") {
		t.Fatal(windows)
	}
	for _, osName := range []string{"linux", "darwin"} {
		text := PasswordEnvironmentInstructions("DB_PASSWORD", osName)
		if !strings.Contains(text, "read -s DB_PASSWORD") || !strings.Contains(text, "export DB_PASSWORD") {
			t.Fatal(osName, text)
		}
	}
}

type reviewPrompt struct {
	fakePrompt
	reviewed bool
}

func TestEmptySecretAndCancellationDuringTest(t *testing.T) {
	t.Setenv("DBVAULT_DB_PASSWORD", "")
	v := map[string]string{"database": "postgres", "database-name": "prod", "user": "backup", "storage": "local"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	_, err := Run(context.Background(), Options{Values: v, Path: path, PathProvided: true, Prompt: &fakePrompt{}, Test: true, TestProvided: true})
	if err == nil || !strings.Contains(err.Error(), "nonempty password") {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("empty secret wrote config")
	}
	v = map[string]string{"database": "sqlite", "database-name": "app.db", "storage": "local"}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = Run(ctx, Options{Values: v, Path: path, Test: true, TestConnection: func(context.Context, config.Config, string) error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled test wrote config")
	}
}

func (p *reviewPrompt) Select(ctx context.Context, title string, choices []Choice, fallback string) (string, error) {
	if title == "Configuration already exists" {
		if !p.reviewed {
			p.reviewed = true
			return "review", nil
		}
		return "overwrite", nil
	}
	return p.fakePrompt.Select(ctx, title, choices, fallback)
}
