package recoveryserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	runner "github.com/sung2708/DBVault/internal/exec"
)

type inspectRunner struct {
	entry    map[string]any
	commands []string
}

func (r *inspectRunner) LookPath(name string) (string, error) { return name, nil }
func (r *inspectRunner) Run(_ context.Context, s runner.Spec) error {
	r.commands = append(r.commands, s.Args[0])
	if s.Args[0] == "inspect" {
		data, _ := json.Marshal([]any{r.entry})
		_, err := s.Stdout.Write(data)
		return err
	}
	return nil
}
func TestOwnershipAndIsolationBeforeCleanup(t *testing.T) {
	for _, engine := range []string{"mysql", "mongodb"} {
		for _, change := range []string{"", "label", "image", "network", "ports", "binds", "mount", "privileged"} {
			t.Run(engine+"/"+change, func(t *testing.T) {
				r := &inspectRunner{}
				target, err := New(engine, "recovered", "source", r)
				if err != nil {
					t.Fatal(err)
				}
				target.ID = strings.Repeat("b", 64)
				target.imageID = "sha256:" + strings.Repeat("a", 64)
				target.docker = "docker"
				host := map[string]any{"NetworkMode": "none"}
				cfg := map[string]any{"Labels": map[string]string{"io.dbvault.recovery": target.token}}
				mount := "/var/lib/mysql"
				if engine == "mongodb" {
					mount = "/data/db"
				}
				entry := map[string]any{"Id": target.ID, "Image": target.imageID, "Config": cfg, "HostConfig": host, "Mounts": []any{map[string]string{"Type": "volume", "Destination": mount}}}
				switch change {
				case "label":
					cfg["Labels"] = map[string]string{"io.dbvault.recovery": "foreign"}
				case "image":
					entry["Image"] = "foreign"
				case "network":
					host["NetworkMode"] = "bridge"
				case "ports":
					host["PortBindings"] = map[string]any{"3306/tcp": []any{}}
				case "binds":
					host["Binds"] = []string{"/tmp:/tmp"}
				case "mount":
					entry["Mounts"] = []any{map[string]string{"Type": "bind", "Destination": mount}}
				case "privileged":
					host["Privileged"] = true
				}
				r.entry = entry
				err = target.Cleanup(context.Background())
				if change == "" {
					if err != nil || !strings.Contains(strings.Join(r.commands, ","), "rm") {
						t.Fatal("owned cleanup failed", err)
					}
				} else {
					if err == nil || strings.Contains(strings.Join(r.commands, ","), "rm") {
						t.Fatal("foreign or changed target removed", change, err)
					}
				}
			})
		}
	}
}

type imageRunner struct {
	inspectRunner
	image string
}

func (r *imageRunner) Run(_ context.Context, s runner.Spec) error {
	if s.Args[0] != "image" {
		return fmt.Errorf("unexpected mutation")
	}
	r.image = s.Args[len(s.Args)-1]
	_, err := io.WriteString(s.Stdout, "sha256:"+strings.Repeat("a", 64))
	return err
}
func TestImageSeriesAndInvalidNames(t *testing.T) {
	for _, engine := range []string{"mysql", "mongodb"} {
		for _, name := range []string{"source", "mysql", "admin", "../escape", "UPPER"} {
			if _, err := New(engine, name, "source", &inspectRunner{}); err == nil {
				t.Fatal("unsafe name accepted", engine, name)
			}
		}
		r := &imageRunner{}
		target, err := New(engine, "recovered", "source", r)
		if err != nil {
			t.Fatal(err)
		}
		version, tool, image := "8.4.11", "mysqldump Ver 8.4.11", "mysql:8.4"
		if engine == "mongodb" {
			version, tool, image = "8.0.15", "100.13.0", "mongo:8.0"
		}
		if err = target.CheckImage(context.Background(), version, tool); err != nil || r.image != image {
			t.Fatal(r.image, err)
		}
	}
}
