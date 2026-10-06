// process.go — Starting external tools as killable process trees.
//
// yt-dlp starts its own ffmpeg child for merging and trimming, and the
// Windows yt-dlp.exe is a PyInstaller bundle that itself runs as more than
// one process. exec.CommandContext on its own kills only the direct child on
// cancel, which would leave the rest of the tree running and holding the
// output file open. newToolCommand wires cancellation to the per-OS
// configureProcessTree (sys_windows.go, sys_others.go) instead.
package main

import (
	"context"
	"os/exec"
	"time"
)

// processWaitDelay bounds how long Wait keeps waiting for a tool's output
// pipes to close after the tool has exited or been cancelled. A grandchild
// that inherited the pipes could otherwise keep Wait blocked indefinitely.
const processWaitDelay = 3 * time.Second

// newToolCommand returns a command for an external tool that runs without a
// console window and whose whole process tree is killed when ctx is done.
func newToolCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	hideWindow(cmd)
	configureProcessTree(cmd)
	return cmd
}
