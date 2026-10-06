package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestAuthenticatedStream(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	for _, size := range []int{0, 1, blockSize, 3*blockSize + 7} {
		data := make([]byte, size)
		rand.Read(data)
		var out bytes.Buffer
		w, err := NewWriter(&out, key)
		if err != nil {
			t.Fatal(err)
		}
		for start := 0; start < len(data); {
			end := min(start+7919, len(data))
			if _, err := w.Write(data[start:end]); err != nil {
				t.Fatal(err)
			}
			start = end
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		encoded := bytes.Clone(out.Bytes())
		r, err := NewReader(bytes.NewReader(encoded), key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("roundtrip", err)
		}
		cases := [][]byte{encoded[:len(encoded)-1], append(bytes.Clone(encoded), 1)}
		altered := bytes.Clone(encoded)
		altered[len(altered)-1] ^= 1
		cases = append(cases, altered)
		for _, bad := range cases {
			r, err := NewReader(bytes.NewReader(bad), key)
			if err == nil {
				_, err = io.ReadAll(r)
			}
			if err == nil {
				t.Fatal("tamper/truncation accepted")
			}
		}
		wrong := bytes.Clone(key)
		wrong[0] ^= 1
		if _, err = NewReader(bytes.NewReader(encoded), wrong); err == nil {
			t.Fatal("wrong wrapping key accepted")
		}
	}
}
