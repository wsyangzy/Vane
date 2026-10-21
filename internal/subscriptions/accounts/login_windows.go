//go:build windows

package accounts

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func loginCommand(ctx context.Context, path string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".cmd" || ext == ".bat" {
		// cmd.exe does not use Go's normal Windows argv escaping. Adapter
		// arguments are fixed words; reject shell syntax rather than interpolate it.
		for _, arg := range append([]string{path}, args...) {
			if strings.ContainsAny(arg, "\"%&|<>^\r\n\x00") {
				cmd.Err = exec.ErrNotFound
				return cmd
			}
		}
		shell, err := exec.LookPath("cmd.exe")
		if err != nil {
			cmd.Err = err
			return cmd
		}
		line := `"` + shell + `" /d /v:off /s /c ""` + path + `"`
		for _, arg := range args {
			line += ` "` + arg + `"`
		}
		cmd.Path = shell
		cmd.SysProcAttr.CmdLine = line + `"`
	}
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
