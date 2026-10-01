package security

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var credentialURL = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`)

type Redactor struct{ secrets []string }

func New(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		if s != "" {
			r.secrets = append(r.secrets, s)
			encoded, _ := json.Marshal(s)
			if string(encoded[1:len(encoded)-1]) != s {
				r.secrets = append(r.secrets, string(encoded[1:len(encoded)-1]))
			}
		}
	}
	return r
}
func (r *Redactor) Text(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	return credentialURL.ReplaceAllString(s, "${1}[REDACTED]@")
}

type safeError struct {
	text  string
	cause error
}

func (e *safeError) Error() string { return e.text }
func (e *safeError) Unwrap() error { return e.cause }
func (r *Redactor) Error(err error) error {
	if err == nil {
		return nil
	}
	return &safeError{r.Text(err.Error()), err}
}

// Writer redacts complete log records. slog calls Write once per record.
func (r *Redactor) Writer(w io.Writer) io.Writer { return &writer{r, w} }

type writer struct {
	r *Redactor
	w io.Writer
}

func (w *writer) Write(p []byte) (int, error) {
	_, err := fmt.Fprint(w.w, w.r.Text(string(p)))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
