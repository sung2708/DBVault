package pitr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/storage/local"
)

type baselineRunner struct {
	dumps   int
	version string
}

func (r *baselineRunner) LookPath(name string) (string, error) { return name, nil }
func (r *baselineRunner) Run(_ context.Context, spec runner.Spec) error {
	args := strings.Join(spec.Args, " ")
	if strings.Contains(args, "--version") {
		_, err := io.WriteString(spec.Stdout, "mysql 8.4.0")
		return err
	}
	if spec.Executable == "mysqldump" {
		r.dumps++
		_, err := io.WriteString(spec.Stdout, "-- CHANGE REPLICATION SOURCE TO SOURCE_LOG_FILE='binlog.000001', SOURCE_LOG_POS=4;\nCREATE DATABASE app;\n")
		return err
	}
	var out string
	switch {
	case strings.Contains(args, "SELECT @@server_uuid"):
		out = "source\t" + r.version
	case strings.Contains(args, "SELECT @@GTID_MODE"):
		out = "OFF"
	case strings.Contains(args, "SELECT DATE_FORMAT"):
		out = time.Now().UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Errorf("unexpected native command")
	}
	_, err := io.WriteString(spec.Stdout, out)
	return err
}

func TestNativeBaselineRefreshAndNoSilentReset(t *testing.T) {
	ctx := context.Background()
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fake := &baselineRunner{version: "8.4.0"}
	svc := Service{Store: store, Runner: fake, Config: config.Config{Database: config.Database{Type: "mysql"}, PITR: &config.PITR{}}}
	initial, err := svc.Backup(ctx, "incremental", 0)
	if err != nil || initial.Kind != "base" || fake.dumps != 1 {
		t.Fatal(initial, err, fake.dumps)
	}
	// A tiny interval is already due on the next invocation.
	fresh, err := svc.Backup(ctx, "incremental", time.Nanosecond)
	if err != nil || fresh.Kind != "base" || fresh.Name == initial.Name || fake.dumps != 2 {
		t.Fatal(fresh, err, fake.dumps)
	}
	fake.version = "8.4.1"
	if _, err = svc.Backup(ctx, "incremental", 0); err == nil || !strings.Contains(err.Error(), "identity/version changed") {
		t.Fatal("capture reset silently", err)
	}
	if fake.dumps != 2 {
		t.Fatal("unexpected full dump after capture failure")
	}
	if ok, _ := store.Exists(ctx, "dbvault_pitr.lock"); ok {
		t.Fatal("failed capture leaked lock")
	}
}

func retentionRecord(id int, at time.Time, parent *Record) Record {
	m := Record{Version: 1, Name: fmt.Sprintf("pitr_%032x.tar", id), Engine: "mysql", Identity: "source", ServerVersion: "8.4.0", Kind: "base", Start: "binlog.000001:4", End: "binlog.000001:4", From: at, Until: at, Hash: strings.Repeat("a", 64), Size: 1024}
	if parent != nil {
		m.Kind = "logs"
		m.Parent = parent.Name
		m.ParentHash = parent.Hash
		m.Start = parent.End
		m.From = parent.Until
		m.End = fmt.Sprintf("binlog.%06d:4", id)
	}
	return m
}

func TestNativeRetentionKeepsWholeChains(t *testing.T) {
	now := time.Now().UTC()
	old := retentionRecord(1, now.Add(-10*24*time.Hour), nil)
	child := retentionRecord(2, old.Until.Add(time.Hour), &old)
	branch := retentionRecord(3, child.Until.Add(time.Hour), &old)
	latest := retentionRecord(4, now, nil)
	other := retentionRecord(5, old.From, nil)
	other.Identity = "other-source"
	items := []Record{latest, branch, other, old, child}
	got, err := SelectCleanup(items, config.Retention{KeepCount: 1}, now)
	if err != nil || len(got) != 3 || got[2].Name != old.Name {
		t.Fatal(got, err)
	}
	got, err = SelectCleanup(items, config.Retention{KeepCount: 2}, now)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	// Equal baseline timestamps protect both roots, so deterministic parent
	// tie-breaking cannot cause cleanup to remove the selected active chain.
	tied := retentionRecord(6, latest.Until, nil)
	got, err = SelectCleanup([]Record{latest, tied}, config.Retention{KeepCount: 1}, now)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	// Age protects an old baseline whose newest log is still recent.
	child.Until = now.Add(-time.Hour)
	got, err = SelectCleanup([]Record{old, child, latest}, config.Retention{KeepDays: 2}, now)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if _, err = SelectCleanup([]Record{child, latest}, config.Retention{KeepCount: 1}, now); err == nil {
		t.Fatal("missing parent accepted")
	}
	child.ParentHash = strings.Repeat("b", 64)
	if _, err = SelectCleanup([]Record{old, child, latest}, config.Retention{}, now); err == nil {
		t.Fatal("broken chain accepted with disabled retention")
	}
	if _, err = SelectCleanup([]Record{latest, latest}, config.Retention{KeepCount: 1}, now); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestNativeRetentionRejectsCyclesAndLongChains(t *testing.T) {
	now := time.Now().UTC()
	a := retentionRecord(1, now, nil)
	b := retentionRecord(2, now, &a)
	a.Kind = "logs"
	a.Parent = b.Name
	a.ParentHash = b.Hash
	a.Start = b.End
	if _, err := SelectCleanup([]Record{a, b}, config.Retention{KeepCount: 1}, now); err == nil {
		t.Fatal("cycle accepted")
	}
	items := []Record{retentionRecord(10, now, nil)}
	for i := 1; i < 1025; i++ {
		items = append(items, retentionRecord(i+10, now, &items[i-1]))
	}
	if _, err := SelectCleanup(items, config.Retention{}, now); err == nil {
		t.Fatal("oversized cached ancestry accepted")
	}
}

func TestNativeAutomaticParentSelection(t *testing.T) {
	now := time.Now().UTC()
	base := retentionRecord(1, now.Add(-time.Hour), nil)
	child := retentionRecord(2, base.Until, &base)
	grandchild := retentionRecord(3, child.Until, &child)
	other := retentionRecord(4, now, nil)
	other.Identity = "other"
	got, at, err := selectParent([]Record{child, other, base, grandchild}, "source")
	if err != nil || got.Name != grandchild.Name || !at.Equal(base.Until) {
		t.Fatal(got, at, err)
	}
	newBase := retentionRecord(5, now, nil)
	// The newest baseline supersedes an older chain even if its tip has a later clock.
	grandchild.Until = now.Add(time.Hour)
	got, _, err = selectParent([]Record{grandchild, base, newBase, child}, "source")
	if err != nil || got.Name != newBase.Name {
		t.Fatal(got, err)
	}
	got, _, err = selectParent([]Record{base}, "unknown")
	if err != nil || got.Name != "" {
		t.Fatal(got, err)
	}
}

func TestNativeCleanupVerificationPreviewAndLock(t *testing.T) {
	ctx := context.Background()
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := Service{Store: store, Config: config.Config{Database: config.Database{Type: "mysql"}, Retention: config.Retention{KeepCount: 1}}}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "base.sql"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old, err := svc.publish(ctx, retentionRecord(1, now.Add(-48*time.Hour), nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.publish(ctx, retentionRecord(2, old.Until.Add(time.Hour), &old), dir)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := svc.publish(ctx, retentionRecord(3, now, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Cleanup(ctx, now, true)
	if err != nil || len(got) != 2 || got[0].Name != child.Name {
		t.Fatal(got, err)
	}
	if ok, _ := store.Exists(ctx, old.Name); !ok {
		t.Fatal("preview deleted data")
	}
	release, err := svc.lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Cleanup(ctx, now, false); err == nil {
		t.Fatal("cleanup ignored lock")
	}
	release()
	// Corruption of the retained recovery point must prevent all deletion.
	if err = store.Delete(ctx, latest.Name); err != nil {
		t.Fatal(err)
	}
	if err = store.Put(ctx, latest.Name, bytes.NewReader([]byte("corrupt")), 7); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Cleanup(ctx, now, false); err == nil {
		t.Fatal("corrupt retained archive accepted")
	}
	if ok, _ := store.Exists(ctx, child.Name); !ok {
		t.Fatal("deletion started before verification")
	}
	if err = store.Delete(ctx, latest.Name); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, latest.Name+".pitr.json"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.publish(ctx, retentionRecord(4, now.Add(time.Hour), nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.Cleanup(ctx, now, false)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for _, m := range got {
		for _, name := range []string{m.Name, m.Name + ".pitr.json"} {
			if ok, _ := store.Exists(ctx, name); ok {
				t.Fatal("expired object remains", name)
			}
		}
	}
	if ok, _ := store.Exists(ctx, "dbvault_pitr.lock"); ok {
		t.Fatal("native lock leaked")
	}
}
