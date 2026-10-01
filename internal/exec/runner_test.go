package exec

import (
	"context"
	"errors"
	"fmt"
	"github.com/sung2708/DBVault/internal/security"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("DBVAULT_TEST_HELPER")
	switch mode {
	case "fail":
		fmt.Fprint(os.Stderr, os.Getenv("TEST_SECRET"))
		os.Exit(7)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "env":
		fmt.Fprint(os.Stdout, os.Getenv("PGSERVICE"))
		fmt.Fprint(os.Stdout, os.Getenv("PGHOSTADDR"))
		os.Exit(0)
	case "fork":
		child := osexec.Command(os.Args[0], "-test.run=TestHelperProcess")
		child.Env = append(os.Environ(), "DBVAULT_TEST_HELPER=pulse")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	case "pulse":
		os.WriteFile(os.Getenv("DBVAULT_TEST_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestCancellationTerminatesDescendants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned-child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (Native{}).Run(ctx, Spec{Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"DBVAULT_TEST_HELPER": "fork", "DBVAULT_TEST_PID_FILE": path}})
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	pid := 0
	for pid == 0 {
		b, e := os.ReadFile(path)
		if e == nil {
			pid, _ = strconv.Atoi(string(b))
			if pid > 0 {
				break
			}
		}
		select {
		case e := <-done:
			t.Fatal("parent exited before fork", e)
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("child readiness timeout")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runner hung on descendant pipes")
	}
	for processAlive(pid) {
		select {
		case <-deadline.C:
			t.Fatal("orphaned child", pid)
		case <-ticker.C:
		}
	}
}

func TestUnsetInheritedEnvironment(t *testing.T) {
	t.Setenv("PGSERVICE", "unexpected-service")
	t.Setenv("PGHOSTADDR", "198.51.100.23")
	var out strings.Builder
	n := Native{}
	err := n.Run(context.Background(), Spec{Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"DBVAULT_TEST_HELPER": "env"}, UnsetEnv: []string{"PGSERVICE", "PGHOSTADDR"}, Stdout: &out})
	if err != nil || out.Len() != 0 {
		t.Fatal("inherited environment not removed", out.String(), err)
	}
}
func TestNativeRedactionAndCancellation(t *testing.T) {
	secret := "SUPER_SECRET_DB_PASSWORD_12345"
	n := Native{Redactor: security.New(secret)}
	err := n.Run(context.Background(), Spec{Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"DBVAULT_TEST_HELPER": "fail", "TEST_SECRET": secret}})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("secret escaped", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = n.Run(ctx, Spec{Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Env: map[string]string{"DBVAULT_TEST_HELPER": "wait"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
