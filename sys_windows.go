//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
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

// configureProcessTree makes cancelling cmd's context kill cmd and every
// process it started, and stops Wait from hanging on pipes a surviving
// descendant still holds open. Call it before cmd is started.
func configureProcessTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return killProcessTree(cmd.Process)
	}
	cmd.WaitDelay = processWaitDelay
}

// killProcessTree kills process and all of its descendants with taskkill /T.
func killProcessTree(process *os.Process) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(process.Pid))
	hideWindow(kill)
	if err := kill.Run(); err != nil {
		// taskkill fails when the process has already exited or cannot be
		// found; killing the direct child still lets Wait return.
		return process.Kill()
	}
	return nil
}

// openFolderCommand returns an exec.Cmd that opens path in Windows Explorer.
func openFolderCommand(path string) *exec.Cmd {
	return exec.Command("explorer", path)
}

// freeDiskBytes returns the number of bytes available to the current user
// on the volume holding path.
func freeDiskBytes(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("checking free space on %s: %w", path, err)
	}
	return available, nil
}
