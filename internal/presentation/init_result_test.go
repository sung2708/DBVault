package presentation

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/onboarding"
)

func TestInitResultKeepsNextCommandsOnSeparateLines(t *testing.T) {
	var out, stderr bytes.Buffer
	r := New(&out, &stderr, Options{NoColor: true})
	result := onboarding.Result{Path: `D:\configs\my config.yaml`, Database: "postgres", DatabaseName: "production", Storage: "local", StorageLocation: "./backups", Compression: "gzip", PasswordEnv: "DBVAULT_DB_PASSWORD", PasswordInstructions: "Set DBVAULT_DB_PASSWORD on this operating system"}
	if err := r.Result("init", result); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Configuration created", "production", "Not tested", "\n  dbvault config --config ", "\n  dbvault test --config ", "\n  dbvault backup --config ", "DBVAULT_DB_PASSWORD", "Set DBVAULT_DB_PASSWORD on this operating system"} {
		if !strings.Contains(out.String(), required) {
			t.Fatal("missing readable setup output", required, out.String())
		}
	}
	if strings.Contains(out.String(), `D:\\configs`) || strings.Contains(out.String(), "\x1b[") || stderr.Len() != 0 {
		t.Fatal("escaped path/ANSI or misplaced output", out.String())
	}
	var quiet bytes.Buffer
	r = New(&quiet, &stderr, Options{Quiet: true})
	if err := r.Result("init", result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(quiet.String(), "Next:") {
		t.Fatal("quiet mode emitted hints")
	}
}
