package pipeline

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestHash(t *testing.T) {
	hash, n, err := Hash(context.Background(), strings.NewReader("abc"), io.Discard)
	if err != nil || n != 3 || hash != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal(hash, n, err)
	}
}
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Hash(ctx, strings.NewReader("abc"), io.Discard)
	if err != context.Canceled {
		t.Fatal(err)
	}
}
func BenchmarkHash(b *testing.B) {
	data := strings.Repeat("x", 1<<20)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		Hash(context.Background(), strings.NewReader(data), io.Discard)
	}
}
