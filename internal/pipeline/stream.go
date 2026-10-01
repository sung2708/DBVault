package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

type Reader struct {
	Context context.Context
	Source  io.Reader
}

func (r Reader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Source.Read(p)
}

type Counter struct {
	Writer io.Writer
	N      int64
}

func (w *Counter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.N += int64(n)
	return n, err
}
func Hash(ctx context.Context, r io.Reader, w io.Writer) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), Reader{ctx, r})
	return hex.EncodeToString(h.Sum(nil)), n, err
}
