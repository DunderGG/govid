//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// hideWindow is a no-op for non-Windows platforms.
func hideWindow(cmd *exec.Cmd) {
	// No action needed on macOS or Linux
}

// openFolderCommand returns an exec.Cmd that opens path in the platform file manager.
// macOS uses 'open'; Linux and other Unix-like systems use 'xdg-open'.
func openFolderCommand(path string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path)
	}
	return exec.Command("xdg-open", path)
}
