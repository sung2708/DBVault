package cli

import (
	"golang.org/x/sys/windows"
	"syscall"
)

func availableBytes(path string) (uint64, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	err = windows.GetDiskFreeSpaceEx(name, &available, &total, &free)
	return available, err
}
