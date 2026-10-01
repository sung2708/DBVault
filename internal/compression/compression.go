package compression

import (
	"compress/gzip"
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
)

type Compressor interface {
	Extension() string
	Compress(io.Writer) (io.WriteCloser, error)
	Decompress(io.Reader) (io.ReadCloser, error)
}
type codec struct {
	kind  string
	level int
}

func New(kind string, level int) (Compressor, error) {
	switch kind {
	case "none", "gzip", "zstd":
	default:
		return nil, fmt.Errorf("unsupported compression")
	}
	if level < 1 || level > 9 {
		return nil, fmt.Errorf("compression level must be 1..9")
	}
	return codec{kind, level}, nil
}
func (c codec) Extension() string {
	switch c.kind {
	case "gzip":
		return ".gz"
	case "zstd":
		return ".zst"
	}
	return ""
}

type noClose struct{ io.Writer }

func (noClose) Close() error { return nil }
func (c codec) Compress(w io.Writer) (io.WriteCloser, error) {
	switch c.kind {
	case "gzip":
		return gzip.NewWriterLevel(w, c.level)
	case "zstd":
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(c.level)), zstd.WithEncoderConcurrency(1))
	}
	return noClose{w}, nil
}
func (c codec) Decompress(r io.Reader) (io.ReadCloser, error) {
	switch c.kind {
	case "gzip":
		return gzip.NewReader(r)
	case "zstd":
		d, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20))
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	}
	return io.NopCloser(r), nil
}
