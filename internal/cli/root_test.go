package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestArgumentGuards(t *testing.T) {
	for _, args := range [][]string{{"backup", "unexpected"}, {"restore", "--target", "backup.dump"}, {"verify"}, {"delete", "--target", "backup.dump"}, {"list", "--limit", "0"}} {
		var out bytes.Buffer
		r := New(Build{Version: "dev"}, &out, &out)
		r.SetArgs(args)
		if err := r.Execute(); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"backup", "--help"}, {"restore", "--help"}, {"version", "--json"}} {
		var out bytes.Buffer
		r := New(Build{Version: "dev", Commit: "unknown", Date: "unknown"}, &out, &out)
		r.SetArgs(args)
		if err := r.Execute(); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("missing output")
		}
		if strings.Contains(out.String(), "skip-verify") {
			t.Fatal("unsafe integrity bypass exposed")
		}
	}
}
