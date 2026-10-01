package storage

import (
	"testing"
)

func TestNamespace(t *testing.T) {
	for _, p := range []string{"../outside", "a/../b", "a\\b", "a\nb"} {
		if _, err := NewNamespace(p); err == nil {
			t.Fatal("unsafe prefix accepted", p)
		}
	}
	n, err := NewNamespace("backups/app/")
	if err != nil {
		t.Fatal(err)
	}
	object, err := n.Object("db.dump.gz")
	if err != nil || object != "backups/app/db.dump.gz" {
		t.Fatal(object, err)
	}
	if name, ok := n.Name(object); !ok || name != "db.dump.gz" {
		t.Fatal(name, ok)
	}
	if _, err = n.Object("../evil"); err == nil {
		t.Fatal("escaped namespace")
	}
	if _, ok := n.Name("other/object"); ok {
		t.Fatal("cross-prefix object accepted")
	}
}
