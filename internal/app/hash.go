package app

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

type hashWriter struct {
	w io.Writer
	h hash.Hash
	N int64
}

func newHashWriter(w io.Writer) *hashWriter { return &hashWriter{w: w, h: sha256.New()} }
func (w *hashWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.h.Write(p[:n])
	w.N += int64(n)
	return n, err
}
func (w *hashWriter) digest() string { return hex.EncodeToString(w.h.Sum(nil)) }
