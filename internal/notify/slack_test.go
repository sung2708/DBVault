package notify

import (
	"context"
	"errors"
	"github.com/sung2708/DBVault/internal/security"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlackRedactionAndFailure(t *testing.T) {
	secret := "SUPER_SECRET_DB_PASSWORD_12345"
	var payload string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		payload = string(b)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	hook := srv.URL + "/SUPER_SECRET_WEBHOOK_12345"
	s := &Slack{URL: hook, Client: srv.Client(), Redactor: security.New(secret, hook, "SUPER_SECRET_WEBHOOK_12345")}
	err := s.Send(context.Background(), Event{Operation: "backup", Status: "failed", Error: secret + " " + hook})
	if err == nil {
		t.Fatal("failure not reported")
	}
	if strings.Contains(payload, secret) || strings.Contains(payload, hook) || strings.Contains(err.Error(), hook) {
		t.Fatal("credential leaked", payload, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.Send(ctx, Event{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
func TestSlackSuccessAndValidation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	s := &Slack{URL: srv.URL, Client: srv.Client()}
	if err := s.Send(context.Background(), Event{Operation: "restore", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	s.URL = "http://example.invalid/secret"
	if err := s.Send(context.Background(), Event{}); err == nil {
		t.Fatal("insecure webhook accepted")
	}
}
