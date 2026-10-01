package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sung2708/DBVault/internal/security"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Event struct {
	Operation    string  `json:"operation"`
	Status       string  `json:"status"`
	BackupID     string  `json:"backup_id,omitempty"`
	DatabaseType string  `json:"database_type"`
	DatabaseName string  `json:"database_name"`
	Size         int64   `json:"size,omitempty"`
	Duration     float64 `json:"duration_seconds,omitempty"`
	Error        string  `json:"error,omitempty"`
}
type Notifier interface {
	Send(context.Context, Event) error
}
type Slack struct {
	URL, Channel string
	Client       *http.Client
	Redactor     *security.Redactor
}

func (s *Slack) Send(ctx context.Context, e Event) error {
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("Slack webhook must be an HTTPS URL")
	}
	r := s.Redactor
	if r == nil {
		r = security.New(s.URL)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"text": r.Text(string(data)), "channel": r.Text(s.Channel)})
	if err != nil {
		return err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(payload))
	if err != nil {
		return r.Error(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return r.Error(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
