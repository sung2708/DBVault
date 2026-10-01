package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Marshal serializes the runtime schema, omitting empty optional fields.
func Marshal(c Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Storage.Type != "local" {
		c.Storage.Local = Local{}
	}
	var node yaml.Node
	if err := node.Encode(c); err != nil {
		return nil, err
	}
	pruneEmpty(&node)
	return yaml.Marshal(&node)
}

func pruneEmpty(n *yaml.Node) bool {
	if n.Kind == yaml.MappingNode {
		var fields []*yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			if !pruneEmpty(n.Content[i+1]) {
				fields = append(fields, n.Content[i], n.Content[i+1])
			}
		}
		n.Content = fields
		return len(fields) == 0
	}
	if n.Kind == yaml.SequenceNode {
		return len(n.Content) == 0
	}
	return n.Value == "" || (n.Tag == "!!bool" && n.Value == "false") || (n.Tag == "!!int" && n.Value == "0")
}

// Write publishes a complete private file. A hard link prevents racing creators
// from replacing an existing config; explicit replacement uses rename.
// The parent must already exist. Existing Windows ACLs remain operator-managed.
func Write(ctx context.Context, path string, c Config, force bool) error {
	data, err := Marshal(c)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stat, err := os.Lstat(path); err == nil {
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("configuration destination must be a regular file, not a directory or symlink")
		}
		if !force {
			return fmt.Errorf("configuration already exists; choose another --config path or use --force")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".dbvault-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if force {
		err = os.Rename(f.Name(), path)
	} else {
		err = os.Link(f.Name(), path)
	}
	if os.IsExist(err) {
		return fmt.Errorf("configuration already exists; choose another --config path or use --force")
	}
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}
