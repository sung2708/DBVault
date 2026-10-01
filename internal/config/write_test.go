package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func initConfig() Config {
	c := Defaults()
	c.Version = "1"
	c.Database.Type = "sqlite"
	c.Database.Database = "app.db"
	c.Storage.Local.Path = "./backups"
	return c
}
func TestConcurrentConfigCreation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Write(context.Background(), path, initConfig(), false) == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("expected exactly one writer", success.Load())
	}
	if _, err := Load(path, Overrides{}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary files remain")
	}
	if runtime.GOOS != "windows" {
		stat, _ := os.Stat(path)
		if stat.Mode().Perm() != 0600 {
			t.Fatal("unsafe permissions")
		}
	}
}
func TestConfigWriteCancellationAndNonregular(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Write(ctx, path, initConfig(), false) == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancel created file")
	}
	if Write(context.Background(), filepath.Dir(path), initConfig(), true) == nil {
		t.Fatal("replaced directory")
	}
	if err := os.Symlink("missing.yaml", path); err == nil {
		if Write(context.Background(), path, initConfig(), true) == nil {
			t.Fatal("replaced symlink")
		}
	}
}
