//go:build !windows

package service

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func routingQuotaCommand(ctx context.Context, path string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, "-p", "/usage")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}

func trackRoutingQuotaProcess(cmd *exec.Cmd) (func(), error) {
	var once sync.Once
	return func() { once.Do(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }) }, nil
}

func secureRoutingQuotaDirectory(path string) error { return os.Chmod(path, 0700) }
