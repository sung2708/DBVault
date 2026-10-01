//go:build !windows

package exec

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

func processAlive(pid int) bool {
	if b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); e == nil {
		if end := strings.LastIndexByte(string(b), ')'); end >= 0 {
			fields := strings.Fields(string(b[end+1:]))
			if len(fields) > 0 && fields[0] == "Z" {
				return false
			}
		}
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
