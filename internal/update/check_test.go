package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func releaseResponse(tag string, prerelease, draft bool) string {
	return fmt.Sprintf(`{"tag_name":%q,"html_url":%q,"prerelease":%t,"draft":%t,"published_at":"2026-09-30T12:00:00Z"}`, tag, "https://github.com/sung2708/DBVault/releases/tag/"+tag, prerelease, draft)
}

func testChecker(t *testing.T, body string, status int) (*Checker, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/releases/latest" {
			t.Errorf("unexpected API path: %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "DBVault/update-check" {
			t.Errorf("missing minimal user agent")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Checker{Client: srv.Client(), Endpoint: srv.URL + "/releases/latest", CachePath: filepath.Join(t.TempDir(), "cache.json"), Now: func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }}, &calls
}

func TestCheckSemVerStates(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            State
		available       bool
	}{
		{"v0.6.0", "v0.7.0", UpdateAvailable, true},
		{"v0.7.0", "v0.7.0", UpToDate, false},
		{"v0.8.0", "v0.7.0", InstalledNewer, false},
		{"v0.9.0", "v0.10.0", UpdateAvailable, true},
		{"v1.8.0", "v2.0.0", UpdateAvailable, true},
		{"v0.8.0", "v1.0.0", UpdateAvailable, true},
		{"local-build", "v0.7.0", VersionUnknown, false},
	} {
		t.Run(tc.current+" to "+tc.latest, func(t *testing.T) {
			checker, _ := testChecker(t, releaseResponse(tc.latest, false, false), http.StatusOK)
			got, err := checker.Check(context.Background(), tc.current, true)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.want || got.UpdateAvailable != tc.available {
				t.Fatalf("got %+v", got)
			}
			if got.MajorUpgrade != (tc.current == "v1.8.0") {
				t.Fatalf("major upgrade flag incorrect: %+v", got)
			}
			if tc.available && got.UpdateCommand != "go install "+ModulePath+"@"+tc.latest {
				t.Fatalf("unsafe/missing command: %q", got.UpdateCommand)
			}
		})
	}
}

func TestDevelopmentSkipsNetwork(t *testing.T) {
	checker, calls := testChecker(t, `{}`, http.StatusInternalServerError)
	got, err := checker.Check(context.Background(), "dev", false)
	if err != nil || got.State != Development || calls.Load() != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", got, err, calls.Load())
	}
}

func TestStableReleaseMetadataValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"prerelease", releaseResponse("v0.8.0-rc.1", true, false), http.StatusOK},
		{"draft", releaseResponse("v0.8.0", false, true), http.StatusOK},
		{"invalid semver", releaseResponse("v0.8.0.0", false, false), http.StatusOK},
		{"wrong URL", `{"tag_name":"v0.8.0","html_url":"https://evil.example/v0.8.0"}`, http.StatusOK},
		{"malformed", `{`, http.StatusOK},
		{"rate limited", `{"message":"rate limit"}`, http.StatusForbidden},
		{"http error", ``, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testChecker(t, tc.body, tc.status)
			if _, err := c.Latest(context.Background()); err == nil {
				t.Fatal("expected validation/request failure")
			}
		})
	}
}

type failingTransport struct{ err error }

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

func TestNetworkTimeoutAndCancellationAreDistinct(t *testing.T) {
	c := &Checker{Client: &http.Client{Transport: failingTransport{context.DeadlineExceeded}}}
	if _, err := c.Latest(context.Background()); err == nil || err.Error() != "request timed out" {
		t.Fatalf("timeout error=%v", err)
	}
	c.Client = &http.Client{Transport: failingTransport{context.Canceled}}
	if _, err := c.Latest(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
}

func TestCancellationWhileReadingResponseBodyIsPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); time.Sleep(20 * time.Millisecond); cancel() }()
	c := &Checker{Endpoint: server.URL}
	_, err := c.Latest(ctx)
	<-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("body cancellation error=%v", err)
	}
}

func TestReleaseAPIRejectsRedirectToOtherHost(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	c := &Checker{Client: source.Client(), Endpoint: source.URL}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("cross-host redirect was accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("request followed an untrusted redirect")
	}
}

func TestCacheHitExpiryForceAndCorruption(t *testing.T) {
	c, calls := testChecker(t, releaseResponse("v0.7.0", false, false), http.StatusOK)
	first, err := c.Check(context.Background(), "v0.6.0", false)
	if err != nil || first.CacheHit || calls.Load() != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := c.Check(context.Background(), "v0.6.0", false)
	if err != nil || !second.CacheHit || calls.Load() != 1 {
		t.Fatalf("second=%+v err=%v calls=%d", second, err, calls.Load())
	}
	forced, err := c.Check(context.Background(), "v0.6.0", true)
	if err != nil || forced.CacheHit || calls.Load() != 2 {
		t.Fatalf("forced=%+v err=%v calls=%d", forced, err, calls.Load())
	}
	c.Now = func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
	expired, err := c.Check(context.Background(), "v0.6.0", false)
	if err != nil || expired.CacheHit || calls.Load() != 3 {
		t.Fatalf("expired=%+v err=%v calls=%d", expired, err, calls.Load())
	}
	if err := os.WriteFile(c.CachePath, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Check(context.Background(), "v0.6.0", false); err != nil || calls.Load() != 4 {
		t.Fatalf("corrupt cache was trusted: calls=%d err=%v", calls.Load(), err)
	}
}

func TestCacheIsValidJSON(t *testing.T) {
	c, _ := testChecker(t, releaseResponse("v0.7.0", false, false), http.StatusOK)
	if _, err := c.Check(context.Background(), "v0.6.0", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		t.Fatal(err)
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("invalid cache JSON: %v", err)
	}
	if !validCache(entry, entry.CheckedAt.Add(time.Second)) {
		t.Fatalf("invalid cache entry: %+v", entry)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(c.CachePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("cache permissions=%#o, want 0600", info.Mode().Perm())
		}
	}
}
