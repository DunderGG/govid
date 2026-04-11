//go:build !windows

package main

import "os/exec"

// hideWindow is a no-op for non-Windows platforms.
func hideWindow(cmd *exec.Cmd) {
	// No action needed on macOS or Linux
}
