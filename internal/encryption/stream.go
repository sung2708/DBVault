// Package encryption implements versioned, bounded-memory authenticated streams.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const Algorithm = "aes256-gcm-stream-v1"
const blockSize = 64 << 10

var magic = []byte("DBVE0001")

func Key(env string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv(env))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("encryption key environment variable must contain a base64-encoded 32-byte key")
	}
	return key, nil
}

type Writer struct {
	dst    io.Writer
	a      cipher.AEAD
	prefix []byte
	seq    uint64
	buf    []byte
	closed bool
}

func NewWriter(dst io.Writer, key []byte, context ...[]byte) (*Writer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption requires a 32-byte wrapping key")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, 4)
	if _, err = rand.Read(prefix); err != nil {
		return nil, err
	}
	// Each backup uses a fresh random 256-bit data key, wrapped by the configured key.
	data := make([]byte, 32)
	defer clear(data)
	if _, err = rand.Read(data); err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := append([]byte{}, magic...)
	for _, value := range context {
		aad = append(aad, value...)
	}
	wrapped := a.Seal(nil, nonce, data, aad)
	header := append(append(append(append([]byte{}, magic...), nonce...), wrapped...), prefix...)
	if err = write(dst, header); err != nil {
		return nil, err
	}
	db, _ := aes.NewCipher(data)
	da, _ := cipher.NewGCM(db)
	return &Writer{dst: dst, a: da, prefix: prefix, buf: make([]byte, 0, blockSize)}, nil
}
func (w *Writer) frame(data []byte) error {
	if w.seq == ^uint64(0) {
		return fmt.Errorf("encrypted stream too long")
	}
	nonce := make([]byte, 12)
	copy(nonce, w.prefix)
	binary.BigEndian.PutUint64(nonce[4:], w.seq)
	w.seq++
	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, uint32(len(data)))
	sealed := w.a.Seal(nil, nonce, data, n)
	if err := write(w.dst, n); err != nil {
		return err
	}
	return write(w.dst, sealed)
}
func (w *Writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("encrypted writer is closed")
	}
	total := 0
	for len(p) > 0 {
		n := min(len(p), blockSize-len(w.buf))
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		total += n
		if len(w.buf) == blockSize {
			if err := w.frame(w.buf); err != nil {
				return total, err
			}
			w.buf = w.buf[:0]
		}
	}
	return total, nil
}
func (w *Writer) Close() error {
	if w.closed {
		return fmt.Errorf("encrypted writer is closed")
	}
	w.closed = true
	if len(w.buf) > 0 {
		if err := w.frame(w.buf); err != nil {
			return err
		}
	}
	return w.frame(nil)
}

type Reader struct {
	src      io.Reader
	a        cipher.AEAD
	prefix   []byte
	seq      uint64
	buf      []byte
	finished bool
}

func NewReader(src io.Reader, key []byte, context ...[]byte) (*Reader, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption requires a 32-byte wrapping key")
	}
	header := make([]byte, 8+12+48+4)
	if _, err := io.ReadFull(src, header); err != nil {
		return nil, err
	}
	if string(header[:8]) != string(magic) {
		return nil, fmt.Errorf("unsupported encrypted stream")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	aad := append([]byte{}, magic...)
	for _, value := range context {
		aad = append(aad, value...)
	}
	data, err := a.Open(nil, header[8:20], header[20:68], aad)
	if err != nil {
		return nil, fmt.Errorf("cannot unwrap backup data key")
	}
	defer clear(data)
	db, _ := aes.NewCipher(data)
	da, _ := cipher.NewGCM(db)
	return &Reader{src: src, a: da, prefix: header[68:]}, nil
}

func write(dst io.Writer, data []byte) error {
	n, err := dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}
	if r.finished {
		return 0, io.EOF
	}
	nbytes := make([]byte, 4)
	if _, err := io.ReadFull(r.src, nbytes); err != nil {
		return 0, fmt.Errorf("missing authenticated stream terminator: %w", err)
	}
	n := binary.BigEndian.Uint32(nbytes)
	if n > blockSize || r.seq == ^uint64(0) {
		return 0, fmt.Errorf("invalid encrypted frame")
	}
	sealed := make([]byte, int(n)+r.a.Overhead())
	if _, err := io.ReadFull(r.src, sealed); err != nil {
		return 0, err
	}
	nonce := make([]byte, 12)
	copy(nonce, r.prefix)
	binary.BigEndian.PutUint64(nonce[4:], r.seq)
	r.seq++
	data, err := r.a.Open(nil, nonce, sealed, nbytes)
	if err != nil {
		return 0, fmt.Errorf("encrypted frame authentication failed")
	}
	if n == 0 {
		var extra [1]byte
		got, err := r.src.Read(extra[:])
		if got != 0 || err != io.EOF {
			return 0, fmt.Errorf("trailing encrypted stream data")
		}
		r.finished = true
		return 0, io.EOF
	}
	r.buf = data
	return r.Read(p)
}
