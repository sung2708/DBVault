package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/fault"
)

func helpPaths(c *cobra.Command, path []string) [][]string {
	result := [][]string{append([]string(nil), path...)}
	for _, child := range c.Commands() {
		if !child.Hidden {
			result = append(result, helpPaths(child, append(append([]string(nil), path...), child.Name()))...)
		}
	}
	return result
}

func TestHelpTreeDiscoverability(t *testing.T) {
	t.Setenv("DB_PASSWORD", "help-secret-password")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "help-secret-cloud")
	t.Setenv("NO_COLOR", "1")
	r := New(Build{Version: "dev"}, &bytes.Buffer{}, &bytes.Buffer{})
	for _, path := range helpPaths(r, nil) {
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			// Both navigation routes must render without loading a missing config.
			for _, viaHelp := range []bool{false, true} {
				var out, stderr bytes.Buffer
				root := New(Build{Version: "dev"}, &out, &stderr)
				args := append(append([]string(nil), path...), "--help")
				if viaHelp {
					args = append([]string{"help"}, path...)
				}
				args = append(args, "--config", filepath.Join(t.TempDir(), "missing.yaml"), "--json")
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				text := out.String()
				for _, content := range []string{"Usage:", "Examples:", "--help"} {
					if !strings.Contains(text, content) {
						t.Errorf("missing %q in %s", content, text)
					}
				}
				for _, forbidden := range []string{"help-secret-password", "help-secret-cloud", "\x1b[", "--skip-verify", "--storage", "--yes"} {
					if strings.Contains(text, forbidden) {
						t.Errorf("unexpected %q in help", forbidden)
					}
				}
				if stderr.Len() != 0 {
					t.Errorf("help wrote stderr: %s", stderr.String())
				}
				cmd, _, err := root.Find(path)
				if err != nil {
					t.Fatal(err)
				}
				if cmd.Short == "" || cmd.Long == "" || cmd.Use == "" || cmd.Example == "" {
					t.Fatal("missing help metadata", cmd.CommandPath())
				}
			}
		})
	}
}

func TestRootAndParentNavigation(t *testing.T) {
	var out bytes.Buffer
	r := New(Build{}, &out, &out)
	r.SetArgs([]string{"--help"})
	if err := r.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Core Commands:", "Operations:", "Configuration:", "Other:", "dbvault.yaml", "backup", "restore", "verify", "inspect", "list", "delete", "cleanup", "test", "config", "schedule", "version", "help"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing root navigation %q", text)
		}
	}
	out.Reset()
	r = New(Build{}, &out, &out)
	r.SetArgs([]string{"schedule", "--help"})
	if err := r.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"add", "list", "remove", "enable", "disable", "foreground", "Ctrl+C", "Restart", "CRON_TZ"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing schedule navigation %q", text)
		}
	}
}

func TestActionableInputErrors(t *testing.T) {
	cases := []struct {
		args       []string
		want, help string
	}{
		{[]string{"bakcup"}, "backup", "dbvault --help"},
		{[]string{"schedule", "ad"}, "add", "dbvault schedule --help"},
		{[]string{"restore"}, "dbvault list", "dbvault restore --help"},
		{[]string{"verify"}, "--target is required", "dbvault verify --help"},
		{[]string{"delete", "--target", "backup.dump.gz"}, "--confirm", "dbvault delete --help"},
		{[]string{"restore", "backup.dump.gz"}, "accepts flags", "dbvault restore --help"},
		{[]string{"schedule", "add"}, "required flag", "dbvault schedule add --help"},
		{[]string{"backup", "--compression", "secret-invalid-value"}, "none, gzip, or zstd", "dbvault backup --help"},
		{[]string{"backup", "--timeout", "secret-invalid-value"}, "a duration", "dbvault backup --help"},
		{[]string{"list", "--limit", "secret-invalid-value"}, "an integer", "dbvault list --help"},
		{[]string{"list", "--json=secret-invalid-value"}, "true or false", "dbvault list --help"},
		{[]string{"backup", "--type", "incremental"}, "only full", "dbvault backup --help"},
		{[]string{"backup", "--storage", "s3"}, "unknown flag", "dbvault backup --help"},
		{[]string{"list", "--output", "xml"}, "--output must be text or json", "dbvault list --help"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			r := New(Build{}, &out, &stderr)
			r.SetArgs(tc.args)
			c, err := r.ExecuteContextC(context.Background())
			if err == nil || fault.ExitCode(err) == 0 {
				t.Fatal("invalid input succeeded")
			}
			if c == nil {
				c = r
			}
			WriteError(&stderr, c, err)
			for _, want := range []string{"Error:", "Usage:", tc.want, tc.help} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("missing %q in %s", want, stderr.String())
				}
			}
			if strings.Contains(stderr.String(), "secret-invalid-value") || out.Len() != 0 {
				t.Fatal("secret leaked or error polluted stdout")
			}
		})
	}
}

func TestHelpDefaultsAndDestructiveBehavior(t *testing.T) {
	for _, tc := range []struct {
		name      string
		required  []string
		forbidden []string
	}{
		{"backup", []string{"unset: configuration", "only full", "--dry-run"}, []string{`default "gzip"`}},
		{"restore", []string{"--target <backup-name>", "--confirm", "--dry-run", "overwrite", "PostgreSQL/MongoDB"}, nil},
		{"delete", []string{"--confirm", "--dry-run", "Permanently"}, nil},
		{"cleanup", []string{"immediately", "no confirmation flag", "newest", "--dry-run"}, []string{"--confirm"}},
	} {
		var out bytes.Buffer
		r := New(Build{}, &out, &out)
		r.SetArgs([]string{tc.name, "--help"})
		if err := r.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.required {
			if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(want)) {
				t.Errorf("%s missing %q", tc.name, want)
			}
		}
		for _, text := range tc.forbidden {
			if strings.Contains(out.String(), text) {
				t.Errorf("%s misleading %q", tc.name, text)
			}
		}
	}
}
