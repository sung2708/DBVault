package local

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLifecycle(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	if err = p.Put(ctx, "db.dump", strings.NewReader("abc"), 3); err != nil {
		t.Fatal(err)
	}
	if err = p.Put(ctx, "db.dump", strings.NewReader("evil"), 4); err == nil {
		t.Fatal("overwrote backup")
	}
	r, err := p.Get(ctx, "db.dump")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "abc" {
		t.Fatal(string(b), err)
	}
	items, err := p.List(ctx, "db")
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if err = p.Delete(ctx, "db.dump"); err != nil {
		t.Fatal(err)
	}
	if ok, err := p.Exists(ctx, "db.dump"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestConcurrentPublicationNeverOverwrites(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var wg sync.WaitGroup
	result := make(chan error, 2)
	for _, data := range []string{"first", "second"} {
		wg.Add(1)
		go func(data string) {
			defer wg.Done()
			result <- p.Put(context.Background(), "same", strings.NewReader(data), int64(len(data)))
		}(data)
	}
	wg.Wait()
	close(result)
	success := 0
	for err := range result {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("%d concurrent writers succeeded", success)
	}
}
func TestKeys(t *testing.T) {
	for _, k := range []string{"../evil", "..\\evil", "/etc/passwd", "C:\\evil", "folder/file", "name:stream", "CON", "NUL.txt", ".tmp", "file.", "file ", "bad*name", "bad?name", "bad\nname", "COM¹.txt"} {
		if ValidKey(k) == nil {
			t.Errorf("accepted %q", k)
		}
	}
}

type broken struct{}

func (broken) Read(p []byte) (int, error) { copy(p, "partial"); return 7, errors.New("read failure") }
func TestFailureCleanup(t *testing.T) {
	dir := t.TempDir()
	p, _ := New(dir)
	defer p.Close()
	if err := p.Put(context.Background(), "bad.dump", broken{}, -1); err == nil {
		t.Fatal("expected failure")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("incomplete file survived", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Put(ctx, "cancel.dump", strings.NewReader("data"), 4); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Skipf("host does not allow symlinks: %v", err)
	}
	p, _ := New(dir)
	defer p.Close()
	if r, err := p.Get(context.Background(), "escape"); err == nil {
		r.Close()
		t.Fatal("escaped root")
	}
}
func BenchmarkPut(b *testing.B) {
	p, _ := New(b.TempDir())
	defer p.Close()
	data := strings.Repeat("x", 1<<20)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		p.Put(context.Background(), "bench", strings.NewReader(data), int64(len(data)))
		p.Delete(context.Background(), "bench")
	}
}
