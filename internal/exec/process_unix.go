//go:build !windows

package exec

import (
	"errors"
	"os"
	osexec "os/exec"
	"syscall"
)

func prepareProcess(cmd *osexec.Cmd) (func() error, func(), error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	terminate := func() error {
		if cmd.Process == nil {
			return nil
		}
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	cmd.Cancel = terminate
	return func() error { return nil }, func() { terminate() }, nil
}
