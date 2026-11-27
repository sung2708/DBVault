package database

import (
	"errors"
	"testing"

	"github.com/sung2708/DBVault/internal/fault"
)

func TestCompareRows(t *testing.T) {
	source := map[string]int64{"public.a": 3, "public.b": 5}
	check, err := CompareRows(source, "snapshot", map[string]int64{"public.a": 3, "public.b": 5, "public.extra": 1})
	if err != nil || check.Matched != 2 || len(check.Differences) != 0 {
		t.Fatal(check, err)
	}
	check, err = CompareRows(source, "separate_transaction", map[string]int64{"public.a": 3, "public.b": 6})
	if err != nil || check.Matched != 1 || len(check.Differences) != 1 || check.Differences[0].Restored != 6 {
		t.Fatal("approximate counts must warn, not fail:", check, err)
	}
	var typed *fault.Error
	if _, err = CompareRows(source, "snapshot", map[string]int64{"public.a": 3, "public.b": 6}); !errors.As(err, &typed) || typed.Kind != fault.Integrity {
		t.Fatal("snapshot difference accepted")
	}
	check, err = CompareRows(source, "separate_transaction", map[string]int64{"public.a": 3})
	if err != nil || len(check.Missing) != 1 || check.Missing[0] != "public.b" {
		t.Fatal("separate-transaction missing table must warn:", check, err)
	}
	if _, err := CompareRows(source, "snapshot", map[string]int64{"public.a": 3}); !errors.As(err, &typed) || typed.Kind != fault.Integrity {
		t.Fatal("snapshot missing table accepted")
	}
}

func TestParsePairs(t *testing.T) {
	got, err := ParsePairs("public.a\x003\x00odd\"name\x0042\x00")
	if err != nil || got["public.a"] != 3 || got["odd\"name"] != 42 {
		t.Fatal(got, err)
	}
	if got, err := ParsePairs(""); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"a\x00", "a\x00x\x00", "a\x00-1\x00"} {
		if _, err := ParsePairs(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
