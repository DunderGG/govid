//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideWindow sets the Windows-specific process attributes to hide the child process console window.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	// CREATE_NO_WINDOW = 0x08000000
	cmd.SysProcAttr.CreationFlags = 0x08000000
}
