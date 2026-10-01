package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduleCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	cfg := filepath.Join("..", "..", "configs", "sqlite.yaml")
	run := func(args ...string) (string, error) {
		var b bytes.Buffer
		r := New(Build{Version: "test"}, &b, &b)
		r.SetArgs(append([]string{"schedule", "--state", path, "--json"}, args...))
		e := r.Execute()
		return b.String(), e
	}
	if _, e := run("add", "--id", "daily", "--cron", "0 2 * * *", "--config", cfg); e != nil {
		t.Fatal(e)
	}
	if _, e := run("add", "--id", "daily", "--cron", "0 2 * * *", "--config", cfg); e == nil {
		t.Fatal("duplicate accepted")
	}
	if _, e := run("disable", "--id", "daily"); e != nil {
		t.Fatal(e)
	}
	if s, e := run("list"); e != nil || !strings.Contains(s, `"enabled":false`) {
		t.Fatal(s, e)
	}
	if _, e := run("enable", "--id", "daily"); e != nil {
		t.Fatal(e)
	}
	if _, e := run("remove", "--id", "daily"); e != nil {
		t.Fatal(e)
	}
	if _, e := run("remove", "--id", "daily"); e == nil {
		t.Fatal("nonexistent schedule removed")
	}
}
