package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

const ManagedAlgorithm = "aes256-gcm-stream-v2"

// Wrapper keeps the wrapping key in a remote key service. Only a fresh data key
// is returned to the backup process; wrapped data keys are stored in the stream.
type Wrapper interface {
	Wrap(context.Context, []byte, []byte) ([]byte, error)
	Unwrap(context.Context, []byte, []byte) ([]byte, error)
}

func NewManagedWriter(ctx context.Context, dst io.Writer, wrapper Wrapper, binding []byte) (*Writer, error) {
	data := make([]byte, 32)
	defer clear(data)
	prefix := make([]byte, 4)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	if _, err := rand.Read(prefix); err != nil {
		return nil, err
	}
	aad := append(append([]byte("DBVE0002"), prefix...), binding...)
	wrapped, err := wrapper.Wrap(ctx, data, aad)
	if err != nil {
		return nil, err
	}
	if len(wrapped) == 0 || len(wrapped) > 64<<10 {
		return nil, fmt.Errorf("invalid wrapped data key size")
	}
	header := make([]byte, 16)
	copy(header, "DBVE0002")
	binary.BigEndian.PutUint32(header[8:12], uint32(len(wrapped)))
	copy(header[12:], prefix)
	if err := write(dst, header); err != nil {
		return nil, err
	}
	if err := write(dst, wrapped); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(data)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Writer{dst: dst, a: a, prefix: prefix, buf: make([]byte, 0, blockSize)}, nil
}

func NewManagedReader(ctx context.Context, src io.Reader, wrapper Wrapper, binding []byte) (*Reader, error) {
	header := make([]byte, 16)
	if _, err := io.ReadFull(src, header); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[8:12])
	if string(header[:8]) != "DBVE0002" || n == 0 || n > 64<<10 {
		return nil, fmt.Errorf("invalid managed encryption header")
	}
	wrapped := make([]byte, n)
	if _, err := io.ReadFull(src, wrapped); err != nil {
		return nil, err
	}
	aad := append(append([]byte("DBVE0002"), header[12:]...), binding...)
	data, err := wrapper.Unwrap(ctx, wrapped, aad)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	if len(data) != 32 {
		return nil, fmt.Errorf("invalid unwrapped data key size")
	}
	block, _ := aes.NewCipher(data)
	a, _ := cipher.NewGCM(block)
	return &Reader{src: src, a: a, prefix: header[12:]}, nil
}
