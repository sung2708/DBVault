package keymanager

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/encryption"
)

func TestRemoteProvidersAndAuthenticatedStream(t *testing.T) {
	for _, kind := range []string{"vault-transit", "aws-kms"} {
		t.Run(kind, func(t *testing.T) {
			key := make([]byte, 32)
			rand.Read(key)
			block, _ := aes.NewCipher(key)
			a, _ := cipher.NewGCM(block)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil {
					w.WriteHeader(400)
					return
				}
				encrypt := r.URL.Path == "/v1/transit/encrypt/test"
				field, aadfield, outfield := "plaintext", "associated_data", "ciphertext"
				if kind == "vault-transit" {
					if r.Header.Get("X-Vault-Token") != "secret-token" {
						w.WriteHeader(403)
						return
					}
					if !encrypt {
						field = "ciphertext"
						outfield = "plaintext"
					}
				}
				if kind == "aws-kms" {
					if r.Header.Get("Authorization") == "" || input["KeyId"] != "test" {
						w.WriteHeader(403)
						return
					}
					encrypt = r.Header.Get("X-Amz-Target") == "TrentService.Encrypt"
					field = "Plaintext"
					if !encrypt {
						field = "CiphertextBlob"
					}
					aadfield = "EncryptionContext"
					outfield = "CiphertextBlob"
					if !encrypt {
						outfield = "Plaintext"
					}
				}
				value, _ := input[field].(string)
				var aad []byte
				if kind == "aws-kms" {
					aad, _ = json.Marshal(input[aadfield])
				} else {
					v, _ := input[aadfield].(string)
					aad, _ = base64.StdEncoding.DecodeString(v)
				}
				if kind == "vault-transit" && !encrypt {
					value = value[len("vault:v1:"):]
				}
				data, err := base64.StdEncoding.DecodeString(value)
				if err != nil {
					w.WriteHeader(400)
					return
				}
				if encrypt {
					nonce := make([]byte, a.NonceSize())
					rand.Read(nonce)
					data = append(nonce, a.Seal(nil, nonce, data, aad)...)
				} else {
					if len(data) < a.NonceSize() {
						w.WriteHeader(400)
						return
					}
					data, err = a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], aad)
					if err != nil {
						w.WriteHeader(400)
						return
					}
				}
				encoded := base64.StdEncoding.EncodeToString(data)
				var output any = map[string]any{outfield: encoded}
				if kind == "vault-transit" {
					if encrypt {
						encoded = "vault:v1:" + encoded
					}
					output = map[string]any{"data": map[string]any{outfield: encoded}}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(output)
			}))
			defer server.Close()
			t.Setenv("VAULT_TEST_TOKEN", "secret-token")
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			cfg := &config.Encryption{KeyID: "remote", Providers: map[string]config.ManagedKey{"remote": {Type: kind, Key: "test", Region: "us-east-1", Endpoint: server.URL, TokenEnv: "VAULT_TEST_TOKEN"}}}
			wrapper, err := Resolve(context.Background(), cfg, "remote")
			if err != nil {
				t.Fatal(err)
			}
			binding := []byte("backup identity")
			plain := bytes.Repeat([]byte("secret data"), 20000)
			var encrypted bytes.Buffer
			writer, err := encryption.NewManagedWriter(context.Background(), &encrypted, wrapper, binding)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = writer.Write(plain); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			reader, err := encryption.NewManagedReader(context.Background(), bytes.NewReader(encrypted.Bytes()), wrapper, binding)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(got, plain) {
				t.Fatal("remote round trip", err)
			}
			if _, err = encryption.NewManagedReader(context.Background(), bytes.NewReader(encrypted.Bytes()), wrapper, []byte("different identity")); err == nil {
				t.Fatal("metadata mismatch accepted")
			}
			corrupted := append([]byte(nil), encrypted.Bytes()...)
			corrupted[len(corrupted)-1] ^= 1
			reader, err = encryption.NewManagedReader(context.Background(), bytes.NewReader(corrupted), wrapper, binding)
			if err == nil {
				_, err = io.ReadAll(reader)
			}
			if err == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
}
func TestProviderEndpointRestrictions(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?token=secret", "file:///tmp/key"} {
		if endpoint(raw) == nil {
			t.Fatal("unsafe endpoint", raw)
		}
	}
}
