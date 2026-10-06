package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/schedule"
)

func TestScheduledRecoveryAndMetricsEndpoint(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.sqlite")
	cfg := filepath.Join(dir, "config.yaml")
	recovery := filepath.Join(dir, "recovery")
	os.Mkdir(recovery, 0700)
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE fixture(value TEXT); INSERT INTO fixture VALUES('kept')"); err != nil {
		t.Fatal(err)
	}
	text := "version: '1'\ndatabase:\n  type: sqlite\n  database: " + filepath.ToSlash(source) + "\nstorage:\n  local:\n    path: " + filepath.ToSlash(filepath.Join(dir, "backups")) + "\nmetrics:\n  record_operations: true\n"
	if err = os.WriteFile(cfg, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	root := New(Build{}, &out, &stderr)
	root.SetArgs([]string{"backup", "--config", cfg, "--json"})
	if err = root.Execute(); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "jobs.json")
	root = New(Build{}, &out, &stderr)
	root.SetArgs([]string{"schedule", "add", "--id", "drill", "--cron", "0 3 * * *", "--config", cfg, "--operation", "recovery", "--recovery-dir", recovery, "--state", state})
	if err = root.Execute(); err == nil {
		t.Fatal("missing recurring authorization accepted")
	}
	root = New(Build{}, &out, &stderr)
	root.SetArgs([]string{"schedule", "add", "--id", "drill", "--cron", "0 3 * * *", "--config", cfg, "--operation", "recovery", "--recovery-dir", recovery, "--confirm", "--state", state})
	if err = root.Execute(); err != nil {
		t.Fatal(err)
	}
	jobs, err := schedule.Load(state)
	if err != nil || len(jobs.Jobs) != 1 {
		t.Fatal(jobs, err)
	}
	out.Reset()
	stderr.Reset()
	root = New(Build{}, &out, &stderr)
	root.PersistentFlags().Set("output", "json")
	o := &options{}
	if err = o.executeScheduledJob(root, context.Background(), jobs.Jobs[0]); err != nil {
		t.Fatal(err, stderr.String())
	}
	var result app.DrillResult
	if err = json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "passed" || result.TargetState != "removed" {
		t.Fatal(result, err, out.String())
	}
	files, _ := os.ReadDir(recovery)
	if len(files) != 0 {
		t.Fatal("successful scheduled target not removed")
	}
	var value string
	if err = db.QueryRow("SELECT value FROM fixture").Scan(&value); err != nil || value != "kept" {
		t.Fatal("source changed", err)
	}
	// Exercise the real command's HTTP server and graceful cancellation.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root = New(Build{}, io.Discard, io.Discard)
	root.SetArgs([]string{"metrics", "--config", cfg, "--listen", address})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	client := &http.Client{Timeout: time.Second}
	var response *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get("http://" + address + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(data), `operation="recovery",status="success"} 1`) {
		t.Fatal(string(data), err)
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("metrics server did not stop")
	}
}

func TestEncryptionKeygenRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.txt")
	var out bytes.Buffer
	root := New(Build{}, &out, &out)
	root.SetArgs([]string{"encryption", "keygen", "--file", path})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		t.Fatal("invalid key")
	}
	if strings.Contains(out.String(), strings.TrimSpace(string(data))) {
		t.Fatal("key leaked")
	}
	root = New(Build{}, &out, &out)
	root.SetArgs([]string{"encryption", "keygen", "--file", path})
	if err := root.Execute(); err == nil {
		t.Fatal("overwrote key")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, data) {
		t.Fatal("key changed")
	}
}
