//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

func hideClientCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
