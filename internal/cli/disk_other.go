//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package cli

import "fmt"

func availableBytes(string) (uint64, error) {
	return 0, fmt.Errorf("available-space query unsupported")
}
