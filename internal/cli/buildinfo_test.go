package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildMetadataSources(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.6.1"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "true"}, {Key: "vcs.time", Value: "2026-01-01T00:00:00Z"}}}
	b := resolveBuild(Build{}, info)
	if b.Version != "v0.6.1" || b.Commit != "abc123-dirty" || b.Date != "unknown" {
		t.Fatal(b)
	}
	info.Main.Version = "(devel)"
	if b = resolveBuild(Build{}, info); b.Version != "dev" {
		t.Fatal("checkout pretends to be release", b)
	}
	release := Build{Version: "v1.0.1", Commit: "release-sha", Date: "2026-10-01T00:00:00Z"}
	if b = resolveBuild(release, info); b != release {
		t.Fatal("linker metadata lost", b)
	}
	if b = resolveBuild(Build{}, nil); b.Version != "dev" || b.Commit != "unknown" || b.Date != "unknown" {
		t.Fatal(b)
	}
}
