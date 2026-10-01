// Package exec is the only production package allowed to start native processes.
package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/fault"
	"github.com/sung2708/DBVault/internal/security"
)

type Spec struct {
	Executable string
	Args       []string
	Env        map[string]string
	UnsetEnv   []string
	Stdin      io.Reader
	Stdout     io.Writer
}
type Runner interface {
	LookPath(string) (string, error)
	Run(context.Context, Spec) error
}
type Native struct{ Redactor *security.Redactor }

func (n Native) LookPath(name string) (string, error) {
	p, err := osexec.LookPath(name)
	return p, fault.Wrap(fault.Dependency, "locate "+name, err)
}

type limited struct {
	data  []byte
	limit int
}

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.data) < b.limit {
		count := min(len(p), b.limit-len(b.data))
		b.data = append(b.data, p[:count]...)
	}
	return n, nil
}
func (n Native) Run(ctx context.Context, s Spec) error {
	cmd := osexec.CommandContext(ctx, s.Executable, s.Args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = make([]string, 0, len(os.Environ())+len(s.Env))
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		skip := false
		for _, name := range s.UnsetEnv {
			if strings.EqualFold(k, name) {
				skip = true
				break
			}
		}
		for name := range s.Env {
			if strings.EqualFold(k, name) {
				skip = true
				break
			}
		}
		if !skip {
			cmd.Env = append(cmd.Env, v)
		}
	}
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	stderr := &limited{limit: 64 << 10}
	cmd.Stderr = stderr
	activate, cleanup, err := prepareProcess(cmd)
	if err != nil {
		return fmt.Errorf("prepare private process scope: %w", err)
	}
	defer cleanup()
	err = cmd.Start()
	if err == nil {
		if err = activate(); err != nil {
			cmd.Process.Kill()
			cmd.Wait()
		} else {
			err = cmd.Wait()
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	r := n.Redactor
	if r == nil {
		r = security.New()
	}
	return r.Error(fmt.Errorf("%s failed: %w: %s", s.Executable, err, strings.TrimSpace(string(stderr.data))))
}
