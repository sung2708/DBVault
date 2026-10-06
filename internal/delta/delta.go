// Package delta stores same-offset references to a verified previous logical dump.
// It does not archive WAL/binlog/oplog or provide point-in-time recovery.
package delta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const BlockSize = 64 << 10

var magic = []byte("DBVD0001")

func Encode(dst io.Writer, current io.Reader, base io.ReaderAt) (int64, error) {
	if _, err := dst.Write(magic); err != nil {
		return 0, err
	}
	buf := make([]byte, BlockSize)
	old := make([]byte, BlockSize)
	var offset int64
	for {
		n, err := io.ReadFull(current, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return offset, err
		}
		if n == 0 {
			break
		}
		header := make([]byte, 5)
		binary.BigEndian.PutUint32(header[1:], uint32(n))
		got, _ := base.ReadAt(old[:n], offset)
		if got == n && bytes.Equal(buf[:n], old[:n]) {
			header[0] = 1
		} else {
			header[0] = 2
		}
		if _, e := dst.Write(header); e != nil {
			return offset, e
		}
		if header[0] == 2 {
			if _, e := dst.Write(buf[:n]); e != nil {
				return offset, e
			}
		}
		offset += int64(n)
		if err != nil {
			break
		}
	}
	_, err := dst.Write(make([]byte, 5))
	return offset, err
}
func Decode(dst io.Writer, src io.Reader, base io.ReaderAt, expected int64) error {
	tag := make([]byte, len(magic))
	if _, err := io.ReadFull(src, tag); err != nil {
		return err
	}
	if !bytes.Equal(tag, magic) {
		return fmt.Errorf("invalid delta stream")
	}
	var offset int64
	buf := make([]byte, BlockSize)
	for {
		header := make([]byte, 5)
		if _, err := io.ReadFull(src, header); err != nil {
			return fmt.Errorf("truncated delta: %w", err)
		}
		n := int(binary.BigEndian.Uint32(header[1:]))
		if header[0] == 0 {
			if n != 0 || offset != expected {
				return fmt.Errorf("invalid delta size")
			}
			var b [1]byte
			if got, e := src.Read(b[:]); got != 0 || e != io.EOF {
				return fmt.Errorf("trailing delta data")
			}
			return nil
		}
		if n < 1 || n > BlockSize || offset+int64(n) > expected {
			return fmt.Errorf("invalid delta frame")
		}
		switch header[0] {
		case 1:
			if _, err := base.ReadAt(buf[:n], offset); err != nil {
				return fmt.Errorf("invalid delta reference: %w", err)
			}
		case 2:
			if _, err := io.ReadFull(src, buf[:n]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown delta frame")
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return err
		}
		offset += int64(n)
	}
}
