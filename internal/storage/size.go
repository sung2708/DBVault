package storage

import (
	"fmt"
	"io"
)

// SizedReader detects truncation and excess bytes before final publication.
func SizedReader(r io.Reader, size int64) io.Reader {
	if size < 0 {
		return r
	}
	return &sizedReader{source: r, size: size}
}

type sizedReader struct {
	source  io.Reader
	size, n int64
}

func (r *sizedReader) Read(p []byte) (int, error) {
	n, e := r.source.Read(p)
	r.n += int64(n)
	if r.n > r.size {
		return n, fmt.Errorf("object exceeds declared size")
	}
	if e == io.EOF && r.n != r.size {
		return n, fmt.Errorf("object is shorter than declared size")
	}
	return n, e
}
