package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	secret := "SUPER_SECRET_DB_PASSWORD_12345"
	hook := "SUPER_SECRET_WEBHOOK_12345"
	r := New(secret, hook)
	cause := errors.New("root")
	err := r.Error(fmt.Errorf("%s %s postgres://name:other@host/db: %w", secret, hook, cause))
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), hook) || strings.Contains(err.Error(), "other") {
		t.Fatal(err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause lost")
	}
	var b bytes.Buffer
	r.Writer(&b).Write([]byte(secret))
	if strings.Contains(b.String(), secret) {
		t.Fatal("writer leaked")
	}
}

func TestJSONEscapedSecret(t *testing.T) {
	secret := "quoted\"secret\\with\nnewline"
	r := New(secret)
	var b bytes.Buffer
	encoder := json.NewEncoder(r.Writer(&b))
	if err := encoder.Encode(map[string]string{"error": secret}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "quoted") || strings.Contains(b.String(), "newline") {
		t.Fatal("escaped secret leaked", b.String())
	}
}
