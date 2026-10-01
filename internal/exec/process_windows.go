package exec

import (
	"fmt"
	"golang.org/x/sys/windows"
	osexec "os/exec"
	"syscall"
	"unsafe"
)

// Start suspended so even an immediate fork cannot escape assignment to the
// private kill-on-close job. Nested jobs are supported on Windows 8 and newer.
func prepareProcess(cmd *osexec.Cmd) (func() error, func(), error) {
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, nil, e
	}
	cleanup := func() { windows.CloseHandle(job) }
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); e != nil {
		cleanup()
		return nil, nil, e
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	cmd.Cancel = func() error {
		e := windows.TerminateJobObject(job, 1)
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return e
	}
	activate := func() error {
		process, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if e != nil {
			return e
		}
		defer windows.CloseHandle(process)
		if e = windows.AssignProcessToJobObject(job, process); e != nil {
			return fmt.Errorf("assign process to private job: %w", e)
		}
		snapshot, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
		if e != nil {
			return e
		}
		defer windows.CloseHandle(snapshot)
		entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
		for e = windows.Thread32First(snapshot, &entry); e == nil; e = windows.Thread32Next(snapshot, &entry) {
			if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
				continue
			}
			thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if e != nil {
				return e
			}
			_, e = windows.ResumeThread(thread)
			windows.CloseHandle(thread)
			return e
		}
		return fmt.Errorf("suspended process primary thread unavailable: %w", e)
	}
	return activate, cleanup, nil
}
