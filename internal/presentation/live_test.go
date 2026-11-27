package presentation

import (
	"bytes"
	"strings"
	"testing"
)

func TestLiveViewRedrawsInPlace(t *testing.T) {
	t.Setenv("TERM", "xterm")
	var out bytes.Buffer
	caps := &Capabilities{TTY: true, Width: 80}
	v := NewLiveView(&out, Options{OutCaps: caps})
	if !v.Supported() {
		t.Fatal("terminal not supported")
	}
	v.Draw("one\ntwo\n")
	v.Draw("three")
	want := "one\ntwo\n\x1b[2F\x1b[Jthree\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
	if NewLiveView(&out, Options{OutCaps: caps, JSON: true}).Supported() || NewLiveView(&out, Options{OutCaps: &Capabilities{}}).Supported() {
		t.Fatal("live view allowed for JSON or a non-terminal")
	}
	if footer := NewLiveView(&out, Options{OutCaps: caps, NoColor: true}).Footer("Updated\x1b[2J"); strings.Contains(footer, "\x1b") {
		t.Fatalf("unsafe footer %q", footer)
	}
}

func TestLiveViewCountsWrappedRows(t *testing.T) {
	if got := visualLines("short\n"+strings.Repeat("x", 25)+"\n\x1b[1mbold\x1b[0m\n", 10); got != 5 {
		t.Fatalf("got %d rows, want 5", got)
	}
}

func TestLiveViewRequiresCursorControls(t *testing.T) {
	opts := Options{OutCaps: &Capabilities{TTY: true}, NoColor: true}
	t.Setenv("TERM", "dumb")
	if NewLiveView(&bytes.Buffer{}, opts).Supported() {
		t.Fatal("live view allowed on TERM=dumb")
	}
	t.Setenv("TERM", "xterm")
	if !NewLiveView(&bytes.Buffer{}, opts).Supported() {
		t.Fatal("no-color disabled cursor redraw")
	}
}
