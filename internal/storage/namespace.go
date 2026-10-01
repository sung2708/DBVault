package storage

import (
	"fmt"
	"path"
	"strings"
)

// Namespace maps flat backup IDs into a configured object prefix, without
// allowing names to escape that prefix or injecting URI/path components.
type Namespace struct{ Prefix string }

func NewNamespace(prefix string) (Namespace, error) {
	p := strings.Trim(prefix, "/")
	if strings.ContainsAny(p, "\\\x00\r\n") || (p != "" && (path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../"))) {
		return Namespace{}, fmt.Errorf("storage prefix must be a normalized relative object prefix")
	}
	if p != "" {
		p += "/"
	}
	return Namespace{p}, nil
}
func FlatKey(key string) error {
	if key == "" || key == "." || key == ".." || strings.HasPrefix(key, ".") || strings.ContainsAny(key, "/\\:\x00\r\n") {
		return fmt.Errorf("object key must be a flat backup filename")
	}
	return nil
}
func (n Namespace) Object(key string) (string, error) {
	if err := FlatKey(key); err != nil {
		return "", err
	}
	return n.Prefix + key, nil
}
func (n Namespace) Name(object string) (string, bool) {
	if !strings.HasPrefix(object, n.Prefix) {
		return "", false
	}
	name := strings.TrimPrefix(object, n.Prefix)
	return name, FlatKey(name) == nil
}
