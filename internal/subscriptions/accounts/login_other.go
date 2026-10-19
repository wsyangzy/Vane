//go:build !windows

package accounts

import (
	"context"
	"os/exec"
)

func loginCommand(ctx context.Context, path string, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, path, args...)
}

func trackLoginProcess(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
