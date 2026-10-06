package delta

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestDeltaChangesAndBounds(t *testing.T) {
	base := make([]byte, 5*BlockSize+19)
	rand.Read(base)
	for _, data := range [][]byte{base, append(bytes.Clone(base), []byte("extended")...), base[:len(base)-20], append([]byte("shift"), base...), nil} {
		current := bytes.Clone(data)
		if len(current) > BlockSize {
			current[BlockSize+5] ^= 1
		}
		var encoded bytes.Buffer
		if _, err := Encode(&encoded, bytes.NewReader(current), bytes.NewReader(base)); err != nil {
			t.Fatal(err)
		}
		var decoded bytes.Buffer
		if err := Decode(&decoded, bytes.NewReader(encoded.Bytes()), bytes.NewReader(base), int64(len(current))); err != nil || !bytes.Equal(decoded.Bytes(), current) {
			t.Fatal("roundtrip", err)
		}
		if err := Decode(&bytes.Buffer{}, bytes.NewReader(encoded.Bytes()[:encoded.Len()-1]), bytes.NewReader(base), int64(len(current))); err == nil {
			t.Fatal("truncation accepted")
		}
		if len(current) > 0 && Decode(&bytes.Buffer{}, bytes.NewReader(encoded.Bytes()), bytes.NewReader(base), int64(len(current)-1)) == nil {
			t.Fatal("size mismatch accepted")
		}
	}
	var encoded bytes.Buffer
	Encode(&encoded, bytes.NewReader(base), bytes.NewReader(base))
	if encoded.Len() > 100 {
		t.Fatal("unchanged blocks were stored")
	}
}
