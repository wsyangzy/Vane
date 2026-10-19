//go:build windows

package accounts

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func loginCommand(ctx context.Context, path string, args []string) *exec.Cmd {
	var cmd *exec.Cmd
	if strings.HasSuffix(strings.ToLower(path), ".cmd") {
		// All arguments are fixed adapter arguments, never user-entered strings.
		cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", `""`+path+`" `+strings.Join(args, " ")+`"`)
	} else {
		cmd = exec.CommandContext(ctx, path, args...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// The login CLI and its helpers belong to one job. Cancellation/timeout and
// completion close this job, so npm shims cannot leave an orphaned login.
func trackLoginProcess(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { windows.CloseHandle(job) }) }, nil
}
