package presentation

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/security"
)

func TestTruncateMiddleKeepsBothEnds(t *testing.T) {
	name := "orders_20261101_020000_" + strings.Repeat("a", 40) + ".dump.gz"
	got := TruncateMiddle(name, 24, "…")
	if ansi.StringWidth(got) != 24 || !strings.HasPrefix(got, "orders_2026") || !strings.HasSuffix(got, ".dump.gz") || !strings.Contains(got, "…") {
		t.Fatal(got)
	}
	if TruncateMiddle("short", 24, "...") != "short" {
		t.Fatal("short name changed")
	}
}

func TestBackupChoicesAlignAndFitWidth(t *testing.T) {
	long := "orders_20261101_020000_" + strings.Repeat("b", 60) + ".dump.gz"
	items := []metadata.Manifest{
		{Name: "a.dump.gz", BackupType: "full", CreatedAt: time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC), Database: metadata.Database{Engine: "postgres", Name: "orders"}, Pipeline: metadata.Pipeline{Stored: 512}},
		{Name: long, BackupType: "incremental", CreatedAt: time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC), Database: metadata.Database{Engine: "postgres", Name: "orders_archive"}, Pipeline: metadata.Pipeline{Stored: 3 << 30}, Encryption: &metadata.Encryption{}},
	}
	for _, width := range []int{120, 80, 60} {
		picker := BackupChoices(items, width, true)
		header, choices := picker.Header, picker.Choices
		if !strings.HasPrefix(header, "CREATED (UTC)") || !strings.HasSuffix(header, "NAME") {
			t.Fatal(header)
		}
		nameColumn := strings.Index(header, "NAME")
		for _, c := range choices {
			if ansi.StringWidth(c.Label) > width-pickerIndent {
				t.Fatalf("width %d: %q is %d columns", width, c.Label, ansi.StringWidth(c.Label))
			}
			if ansi.StringWidth(c.Label[:strings.LastIndex(c.Label, "  ")+2]) != nameColumn {
				t.Fatalf("width %d: name column misaligned in %q", width, c.Label)
			}
		}
		if !strings.HasSuffix(choices[1].Label, "dump.gz") || !strings.Contains(choices[1].Label, "orders_2") || choices[1].Value != long {
			t.Fatal(choices[1].Label)
		}
		// Columns give way in order: source first, then encryption and type.
		if (width >= 120) != strings.Contains(header, "SOURCE") || (width >= 80) != (strings.Contains(choices[1].Label, "incremental") && strings.Contains(choices[1].Label, "yes")) {
			t.Fatalf("width %d: unexpected columns in %q / %q", width, header, choices[1].Label)
		}
	}
	// Too narrow for every column: the name still keeps its minimum width.
	choices := BackupChoices(items, 30, false).Choices
	if !strings.Contains(choices[1].Label, "orders_") || !strings.Contains(choices[1].Label, "...") {
		t.Fatal(choices[1].Label)
	}
}

func TestBackupPickerFiltersByFullName(t *testing.T) {
	name := "orders_" + strings.Repeat("middle_", 16) + "archive.dump.gz"
	picker := BackupChoices([]metadata.Manifest{{Name: "other.dump.gz"}, {Name: name}}, 80, true)
	p := &TerminalPrompter{}
	selected := picker.Choices[0].Value
	field := p.backupSelect("Choose backup", picker.Choices, &selected)
	field.WithWidth(80)
	field.WithKeyMap(huh.NewDefaultKeyMap())
	field.Focus()
	field.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	field.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Paste: true})
	field.Update(tea.KeyMsg{Type: tea.KeyEnter})
	field.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected != name {
		t.Fatalf("selected %q, want %q", selected, name)
	}
	if _, rows, _ := strings.Cut(field.View(), "\n"); !strings.Contains(rows, picker.Choices[1].Label) || strings.Contains(rows, name) {
		t.Fatalf("unexpected option display: %q", rows)
	}
}

func TestBackupChoicesMoveSharedSourceOutOfRows(t *testing.T) {
	items := []metadata.Manifest{{Name: "a.gz", Database: metadata.Database{Engine: "sqlite", Name: "app.db"}}, {Name: "b.gz", Database: metadata.Database{Engine: "sqlite", Name: "app.db"}}}
	picker := BackupChoices(items, 80, false)
	if picker.Source != "sqlite/app.db" || strings.Contains(picker.Header, "SOURCE") || strings.Contains(picker.Choices[0].Label, "app.db") {
		t.Fatal(picker)
	}
}

func TestPromptWarningIsMarkedWithoutColor(t *testing.T) {
	var out bytes.Buffer
	t.Setenv("NO_COLOR", "1")
	p := &TerminalPrompter{Out: &out, Redactor: security.New("secret-value")}
	p.Warning("--clean drops objects in secret-value\x1b[2J")
	if got := out.String(); !strings.HasPrefix(got, "[WARN] --clean") || strings.Contains(got, "\x1b") || strings.Contains(got, "secret-value") {
		t.Fatal(got)
	}
}

func TestRestorePlanOmitsEmptySelectors(t *testing.T) {
	plan := RestorePlan{Manifest: metadata.Manifest{Name: "b.db.gz", Database: metadata.Database{Engine: "sqlite", Name: "app.db"}}, NewDatabase: true}
	plan.Destination.Type, plan.Destination.Database = "sqlite", "restore.db"
	text := FormatFields(plan.Fields(), 0)
	if strings.Contains(text, "Tables") || strings.Contains(text, "Safety") || strings.Contains(text, "Server") || !strings.Contains(text, "Mode          Create new database") || plan.Warnings() != nil {
		t.Fatal(text)
	}
}

func progressRenderer(width int, unicode bool, now *time.Time) *Renderer {
	var out, err bytes.Buffer
	caps := &Capabilities{TTY: true, Color: true, Unicode: unicode, Width: width}
	return New(&out, &err, Options{OutCaps: caps, ErrCaps: caps, Now: func() time.Time { return *now }})
}

func TestProgressThroughputAndETA(t *testing.T) {
	enableColorEnv(t)
	now := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	r := progressRenderer(120, true, &now)
	r.Step("restore.write")
	for i := 1; i <= 4; i++ {
		now = now.Add(time.Second)
		r.Observe(app.Event{Stage: "restore.write", State: "progress", Bytes: int64(i) * 10 << 20, Total: 100 << 20})
	}
	line := r.progressLine()
	for _, want := range []string{"40.00 MiB / 100.00 MiB", "40%", "████░░░░░░", "10.00 MiB/s", "ETA 6s"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
	if ansi.StringWidth(line) > 120 {
		t.Fatal("line exceeds width")
	}

	// Unknown totals show throughput but never a percentage or ETA.
	now = now.Add(time.Second)
	r.Step("backup.stream")
	for i := 1; i <= 3; i++ {
		now = now.Add(time.Second)
		r.Observe(app.Event{Stage: "backup.stream", State: "progress", Bytes: int64(i) << 20})
	}
	line = r.progressLine()
	if !strings.Contains(line, "1.00 MiB/s") || strings.Contains(line, "%") || strings.Contains(line, "ETA") {
		t.Fatal(line)
	}
}

func TestProgressHidesRateForShortStagesAndFitsNarrowWidth(t *testing.T) {
	now := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	r := progressRenderer(80, false, &now)
	r.Step("verify")
	now = now.Add(time.Second)
	r.Observe(app.Event{Stage: "verify", State: "progress", Bytes: 5 << 20, Total: 10 << 20})
	if line := r.progressLine(); strings.Contains(line, "/s") || strings.Contains(line, "ETA") || !strings.Contains(line, "[#####-----]") {
		t.Fatal("short stage showed rate or wrong ASCII bar:", line)
	}
	for _, width := range []int{80, 60, 40, 30} {
		now = now.Add(3 * time.Second)
		r.errCaps.Width = width
		r.Observe(app.Event{Stage: "verify", State: "progress", Bytes: 6 << 20, Total: 10 << 20})
		if line := r.progressLine(); ansi.StringWidth(line) > width {
			t.Fatalf("width %d: %q", width, line)
		}
	}
}
