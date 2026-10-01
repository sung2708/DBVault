package toolresolve

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type lookupFunc func(string) (string, error)

func (f lookupFunc) LookPath(s string) (string, error) { return f(s) }

func fakeTools(t *testing.T, dir, engine string) map[string]string {
	t.Helper()
	names := required[engine]
	result := map[string]string{}
	for _, name := range names {
		p := filepath.Join(dir, executable(name, runtime.GOOS))
		mode := os.FileMode(0600)
		if runtime.GOOS != "windows" {
			mode = 0700
		}
		if err := os.WriteFile(p, []byte("fixture"), mode); err != nil {
			t.Fatal(err)
		}
		result[name] = p
	}
	return result
}

func TestDiscoverDirectoryWithSpacesAndPartialToolset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "PostgreSQL 18", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	want := fakeTools(t, dir, "postgres")
	got, err := DiscoverInDirectory("postgres", dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range want {
		if got[name] != path {
			t.Fatalf("%s: %q != %q", name, got[name], path)
		}
	}
	if err := os.Remove(want["psql"]); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverInDirectory("postgres", dir); err == nil {
		t.Fatal("accepted partial installation")
	}
}

func TestResolveExplicitWinsAndNeverFallsBackWhenMissing(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit tool")
	mode := os.FileMode(0600)
	if runtime.GOOS != "windows" {
		mode = 0700
	}
	if err := os.WriteFile(explicit, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	lookup := lookupFunc(func(string) (string, error) { lookups++; return "fallback", nil })
	got, err := Resolve("pg_dump", explicit, lookup)
	if err != nil || got != explicit || lookups != 0 {
		t.Fatal(got, err, lookups)
	}
	if err := os.Remove(explicit); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("pg_dump", explicit, lookup); err == nil {
		t.Fatal("missing explicit executable silently fell back")
	}
}

func TestResolveUsesPathLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pg_dump")
	mode := os.FileMode(0600)
	if runtime.GOOS != "windows" {
		mode = 0700
	}
	if err := os.WriteFile(path, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("pg_dump", "", lookupFunc(func(string) (string, error) { return path, nil }))
	if err != nil || got != path {
		t.Fatal(got, err)
	}
}
