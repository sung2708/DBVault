package recoverypostgres

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/database"
	runner "github.com/sung2708/DBVault/internal/exec"
)

type fakeDocker struct {
	specs      []runner.Spec
	inspection string
	major      int
}

func (*fakeDocker) LookPath(string) (string, error) { return "docker", nil }
func (f *fakeDocker) Run(_ context.Context, s runner.Spec) error {
	f.specs = append(f.specs, s)
	out := ""
	switch s.Args[0] {
	case "image":
		out = "sha256:" + strings.Repeat("a", 64)
	case "create":
		out = strings.Repeat("b", 64)
	case "inspect":
		out = f.inspection
	case "exec":
		major := f.major
		if major == 0 {
			major = 16
		}
		joined := strings.Join(s.Args, " ")
		if strings.Contains(joined, "--version") {
			out = fmt.Sprintf("PostgreSQL %d.4", major)
		} else if strings.Contains(joined, "SHOW server_version_num") {
			out = fmt.Sprintf("%d0004", major)
		} else if strings.Contains(joined, "BEGIN READ ONLY") {
			out = "2"
		}
	}
	if s.Stdout != nil {
		_, err := io.WriteString(s.Stdout, out)
		return err
	}
	return nil
}

func TestMislabeledImageVersionIsRejectedBeforeRestore(t *testing.T) {
	target, f := fixture(t)
	ctx := context.Background()
	if err := target.CheckImage(ctx, "160004", "pg_dump (PostgreSQL) 16.4"); err != nil {
		t.Fatal(err)
	}
	f.major = 17
	if err := target.Start(ctx); err == nil {
		t.Fatal("mislabeled image accepted")
	}
	if target.ID == "" {
		t.Fatal("lost owned target identity after compatibility failure")
	}
}
func fixture(t *testing.T) (*Target, *fakeDocker) {
	t.Helper()
	f := &fakeDocker{}
	target, err := New("recovered", "production", f)
	if err != nil {
		t.Fatal(err)
	}
	f.inspection = fmt.Sprintf(`[{"Id":%q,"Image":%q,"Config":{"Labels":{"io.dbvault.recovery":%q}},"HostConfig":{"NetworkMode":"none","Privileged":false},"Mounts":[{"Type":"volume","Destination":"/var/lib/postgresql/data"}]}]`, strings.Repeat("b", 64), "sha256:"+strings.Repeat("a", 64), target.token)
	return target, f
}

func TestNamesAndImageChecks(t *testing.T) {
	for _, name := range []string{"production", "postgres", "template0", "template1", "", "../production", "a;drop", "a\n", "UPPER", strings.Repeat("x", 64)} {
		if _, err := New(name, "production", &fakeDocker{}); err == nil {
			t.Fatalf("unsafe database accepted: %q", name)
		}
	}
	target, f := fixture(t)
	if err := target.CheckImage(context.Background(), "160004", "pg_dump (PostgreSQL) 16.4"); err != nil {
		t.Fatal(err)
	}
	if target.Image != "postgres:16-bookworm" || len(f.specs) != 1 || f.specs[0].Args[0] != "image" || target.ID != "" {
		t.Fatal("image preflight mutated target", f.specs)
	}
	if err := target.CheckImage(context.Background(), "160004", "pg_dump (PostgreSQL) 18.1"); err == nil {
		t.Fatal("inconsistent source tool accepted")
	}
}

func TestOwnedContainerLifecycleAndSecretArguments(t *testing.T) {
	target, f := fixture(t)
	ctx := context.Background()
	if err := target.CheckImage(ctx, "160004", "pg_dump (PostgreSQL) 16.4"); err != nil {
		t.Fatal(err)
	}
	if err := target.Start(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := target.ValidateRecovery(ctx)
	if err != nil || v.Objects != 2 || v.Method != ValidationMethod {
		t.Fatal(v, err)
	}
	if err := target.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	for _, s := range f.specs {
		joined := strings.Join(s.Args, " ")
		if strings.Contains(joined, target.Password) {
			t.Fatal("password in process arguments")
		}
		if s.Args[0] == "create" && (!strings.Contains(joined, "--network=none") || !strings.Contains(joined, "--pull=never") || strings.Contains(joined, "--publish") || strings.Contains(joined, "--privileged")) {
			t.Fatal("unsafe create", s.Args)
		}
	}
	last := f.specs[len(f.specs)-1]
	if strings.Join(last.Args, " ") != "rm --force --volumes "+target.ID {
		t.Fatal("cleanup not by exact ID", last.Args)
	}
}

func TestChangedOwnershipOrIsolationBlocksRestoreAndCleanup(t *testing.T) {
	for _, mutation := range []func(string) string{
		func(s string) string { return strings.ReplaceAll(s, `"NetworkMode":"none"`, `"NetworkMode":"host"`) },
		func(s string) string { return strings.ReplaceAll(s, `"Privileged":false`, `"Privileged":true`) },
		func(s string) string { return strings.ReplaceAll(s, `"Type":"volume"`, `"Type":"bind"`) },
		func(s string) string { return `[]` },
		func(s string) string {
			return strings.ReplaceAll(s, `"Privileged":false`, `"Privileged":false,"PortBindings":{"5432/tcp":[{"HostPort":"5432"}]}`)
		},
		func(s string) string { return strings.ReplaceAll(s, strings.Repeat("b", 64), strings.Repeat("c", 64)) },
		func(s string) string {
			return strings.ReplaceAll(s, `"Mounts":`, `"NetworkSettings":{"Networks":{"bridge":{}}},"Mounts":`)
		},
	} {
		target, f := fixture(t)
		ctx := context.Background()
		if err := target.CheckImage(ctx, "160004", "pg_dump (PostgreSQL) 16.4"); err != nil {
			t.Fatal(err)
		}
		if err := target.Start(ctx); err != nil {
			t.Fatal(err)
		}
		f.inspection = mutation(f.inspection)
		before := len(f.specs)
		if err := target.Restore(ctx, strings.NewReader("archive"), database.RestoreOptions{}); err == nil {
			t.Fatal("restore accepted changed target")
		}
		if err := target.Cleanup(ctx); err == nil {
			t.Fatal("cleanup accepted changed target")
		}
		for _, s := range f.specs[before:] {
			if s.Args[0] == "exec" || s.Args[0] == "rm" {
				t.Fatal("changed target mutated")
			}
		}
	}
}
