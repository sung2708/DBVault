package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/update"
)

type fakeUpdateService struct {
	result update.Result
	err    error
	called bool
	force  bool
}

func (f *fakeUpdateService) Check(_ context.Context, installed string, force bool) (update.Result, error) {
	f.called = true
	f.force = force
	if f.result.InstalledVersion == "" {
		f.result.InstalledVersion = installed
	}
	return f.result, f.err
}

func TestUpdateCommandHelpAndDevelopmentJSON(t *testing.T) {
	for _, args := range [][]string{{"update", "--help"}, {"update", "check", "--help"}} {
		var out, errOut bytes.Buffer
		root := New(Build{Version: "dev"}, &out, &errOut)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Usage:", "Examples:", "--help"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v help missing %q: %s", args, want, out.String())
			}
		}
	}
	var out, errOut bytes.Buffer
	root := New(Build{Version: "dev"}, &out, &errOut)
	root.SetArgs([]string{"update", "check", "--output", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result update.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("stdout not pure JSON: %s (%v)", out.String(), err)
	}
	if result.State != update.Development || result.InstalledVersion != "dev" || errOut.Len() != 0 {
		t.Fatalf("result=%+v stderr=%q", result, errOut.String())
	}
}

func TestUpdateHumanOutputAndCacheFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	service := &fakeUpdateService{result: update.Result{InstalledVersion: "v0.6.0", LatestVersion: "v0.7.0", State: update.UpdateAvailable, UpdateAvailable: true, UpdateCommand: "go install github.com/sung2708/DBVault/cmd/dbvault@v0.7.0", ReleaseURL: "https://github.com/sung2708/DBVault/releases/tag/v0.7.0"}}
	root := newWithUpdateService(Build{Version: "v0.6.0"}, &out, &errOut, service)
	root.SetArgs([]string{"update", "check", "--force", "--no-color"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DBVault Update", "v0.6.0", "v0.7.0", "go install github.com/sung2708/DBVault/cmd/dbvault@v0.7.0", "Prebuilt binary"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human result missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b[") || !service.called || !service.force || errOut.Len() != 0 {
		t.Fatalf("unexpected output/service: called=%t force=%t stdout=%q stderr=%q", service.called, service.force, out.String(), errOut.String())
	}
}

func TestUpdateFailureUsesStructuredExistingErrorPolicy(t *testing.T) {
	for _, tc := range []struct {
		json bool
		want string
	}{
		{true, "\"installed_version\":\"v0.6.0\""},
		{false, "Could not check for updates"},
	} {
		var out, errOut bytes.Buffer
		service := &fakeUpdateService{result: update.Result{InstalledVersion: "v0.6.0", State: update.CheckFailed, Reason: "request timed out"}, err: errors.New("request timed out")}
		root := newWithUpdateService(Build{Version: "v0.6.0"}, &out, &errOut, service)
		args := []string{"update", "check"}
		if tc.json {
			args = append(args, "--output", "json")
		}
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Fatal("expected update check error")
		}
		if got := fault.ExitCode(err); got != 2 {
			t.Fatalf("exit code=%d, err=%v", got, err)
		}
		WriteError(&errOut, root, err)
		if out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) {
			t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
		}
		if tc.json {
			var record map[string]any
			if err := json.Unmarshal(errOut.Bytes(), &record); err != nil {
				t.Fatalf("stderr not pure JSON: %q (%v)", errOut.String(), err)
			}
		}
		if !tc.json && (strings.Contains(errOut.String(), "Usage:") || !strings.Contains(errOut.String(), "Installed") || !strings.Contains(errOut.String(), "Try again later")) {
			t.Fatalf("failure UX not concise: %s", errOut.String())
		}
	}
}
