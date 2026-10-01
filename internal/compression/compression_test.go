package compression

import (
	"bytes"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	raw := bytes.Repeat([]byte("data\x00\xff abcdefghijklmnopqrstuvwxyz\n"), 1000)
	for _, kind := range []string{"none", "gzip", "zstd"} {
		t.Run(kind, func(t *testing.T) {
			c, err := New(kind, 6)
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			w, err := c.Compress(&b)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := c.Decompress(&b)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			result, err := io.ReadAll(r)
			if err != nil || !bytes.Equal(raw, result) {
				t.Fatal("round trip failed", err)
			}
		})
	}
}
func TestCorrupt(t *testing.T) {
	for _, kind := range []string{"gzip", "zstd"} {
		c, _ := New(kind, 6)
		r, err := c.Decompress(bytes.NewReader([]byte("invalid archive")))
		if err == nil {
			defer r.Close()
			_, err = io.Copy(io.Discard, r)
		}
		if err == nil {
			t.Fatal("corruption accepted", kind)
		}
	}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestWriteFailure(t *testing.T) {
	for _, kind := range []string{"gzip", "zstd"} {
		c, _ := New(kind, 6)
		w, err := c.Compress(failing{})
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := w.Write([]byte("data"))
		closeErr := w.Close()
		if writeErr == nil && closeErr == nil {
			t.Fatal("lost output error", kind)
		}
	}
}
func BenchmarkCompression(b *testing.B) {
	raw := bytes.Repeat([]byte("streaming database fixture\n"), 4096)
	for _, kind := range []string{"gzip", "zstd"} {
		b.Run(kind, func(b *testing.B) {
			c, _ := New(kind, 6)
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				w, _ := c.Compress(io.Discard)
				w.Write(raw)
				w.Close()
			}
		})
	}
}
