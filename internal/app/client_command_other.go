//go:build !windows

package app

import "os/exec"

func hideClientCommandWindow(_ *exec.Cmd) {}
