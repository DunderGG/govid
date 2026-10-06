//go:build !windows

package main

import (
	"os/exec"
	"runtime"
	"syscall"
)

// hideWindow is a no-op for non-Windows platforms.
func hideWindow(cmd *exec.Cmd) {
	// No action needed on macOS or Linux
}

// configureProcessTree makes cancelling cmd's context kill cmd and every
// process it started, and stops Wait from hanging on pipes a surviving
// descendant still holds open. Call it before cmd is started.
//
// cmd is started in its own process group, so its descendants (which inherit
// the group) can be killed together.
func configureProcessTree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		// A negative PID signals every process in the group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = processWaitDelay
}

// openFolderCommand returns an exec.Cmd that opens path in the platform file manager.
// macOS uses 'open'; Linux and other Unix-like systems use 'xdg-open'.
func openFolderCommand(path string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path)
	}
	return exec.Command("xdg-open", path)
}
