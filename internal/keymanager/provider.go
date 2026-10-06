// Package keymanager resolves versioned encryption IDs without storing remote
// wrapping keys in the application. Endpoint choices come only from configuration.
package keymanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/encryption"
)

func endpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid key provider endpoint")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return fmt.Errorf("key provider requires HTTPS (HTTP allowed only on loopback)")
	}
	return nil
}

func Resolve(ctx context.Context, c *config.Encryption, id string) (encryption.Wrapper, error) {
	if c == nil {
		return nil, fmt.Errorf("encryption key ID is not configured")
	}
	p, ok := c.Providers[id]
	if !ok {
		return nil, fmt.Errorf("managed encryption key ID is not configured")
	}
	if p.Endpoint != "" {
		if err := endpoint(p.Endpoint); err != nil {
			return nil, err
		}
	}
	switch p.Type {
	case "aws-kms":
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(p.Region))
		if err != nil {
			return nil, fmt.Errorf("load KMS credentials")
		}
		client := kms.NewFromConfig(cfg, func(o *kms.Options) {
			if p.Endpoint != "" {
				o.BaseEndpoint = aws.String(p.Endpoint)
			}
		})
		return &kmsWrapper{client: client, key: p.Key}, nil
	case "vault-transit":
		token := os.Getenv(p.TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("Vault token environment variable is unset")
		}
		mount := p.Mount
		if mount == "" {
			mount = "transit"
		}
		if strings.ContainsAny(p.Key, "/\\?#") || strings.ContainsAny(mount, "\\?#") || strings.Contains(mount, "..") {
			return nil, fmt.Errorf("invalid Vault key or mount")
		}
		client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		return &vaultWrapper{client: client, endpoint: strings.TrimRight(p.Endpoint, "/") + "/v1/" + strings.Trim(mount, "/"), key: p.Key, token: token, namespace: p.Namespace}, nil
	}
	return nil, fmt.Errorf("unsupported managed encryption provider")
}

type kmsAPI interface {
	Encrypt(context.Context, *kms.EncryptInput, ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(context.Context, *kms.DecryptInput, ...func(*kms.Options)) (*kms.DecryptOutput, error)
}
type kmsWrapper struct {
	client kmsAPI
	key    string
}

func kmsContext(aad []byte) map[string]string {
	h := sha256.Sum256(aad)
	return map[string]string{"dbvault": hex.EncodeToString(h[:])}
}
func (w *kmsWrapper) Wrap(ctx context.Context, data, aad []byte) ([]byte, error) {
	r, err := w.client.Encrypt(ctx, &kms.EncryptInput{KeyId: aws.String(w.key), Plaintext: data, EncryptionContext: kmsContext(aad)})
	if err != nil {
		return nil, fmt.Errorf("KMS encryption failed")
	}
	return r.CiphertextBlob, nil
}
func (w *kmsWrapper) Unwrap(ctx context.Context, data, aad []byte) ([]byte, error) {
	r, err := w.client.Decrypt(ctx, &kms.DecryptInput{KeyId: aws.String(w.key), CiphertextBlob: data, EncryptionContext: kmsContext(aad)})
	if err != nil {
		return nil, fmt.Errorf("KMS decryption failed")
	}
	return r.Plaintext, nil
}

type vaultWrapper struct {
	client                          *http.Client
	endpoint, key, token, namespace string
}

func (w *vaultWrapper) call(ctx context.Context, action string, data, aad []byte) ([]byte, error) {
	field := "plaintext"
	value := base64.StdEncoding.EncodeToString(data)
	if action == "decrypt" {
		field = "ciphertext"
		value = string(data)
	}
	payload, _ := json.Marshal(map[string]string{field: value, "associated_data": base64.StdEncoding.EncodeToString(aad)})
	req, err := http.NewRequestWithContext(ctx, "POST", w.endpoint+"/"+action+"/"+url.PathEscape(w.key), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("invalid Vault request")
	}
	req.Header.Set("X-Vault-Token", w.token)
	req.Header.Set("Content-Type", "application/json")
	if w.namespace != "" {
		req.Header.Set("X-Vault-Namespace", w.namespace)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Vault request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Vault %s failed (HTTP %d)", action, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (128<<10)+1))
	if err != nil || len(body) > 128<<10 {
		return nil, fmt.Errorf("invalid Vault response size")
	}
	var result struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
			Plaintext  string `json:"plaintext"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil, fmt.Errorf("invalid Vault response")
	}
	if action == "encrypt" {
		if !strings.HasPrefix(result.Data.Ciphertext, "vault:v") {
			return nil, fmt.Errorf("invalid Vault ciphertext")
		}
		return []byte(result.Data.Ciphertext), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("invalid Vault plaintext")
	}
	return decoded, nil
}
func (w *vaultWrapper) Wrap(ctx context.Context, data, aad []byte) ([]byte, error) {
	return w.call(ctx, "encrypt", data, aad)
}
func (w *vaultWrapper) Unwrap(ctx context.Context, data, aad []byte) ([]byte, error) {
	return w.call(ctx, "decrypt", data, aad)
}
