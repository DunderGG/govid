// process.go — Starting external tools as killable process trees.
//
// yt-dlp starts its own ffmpeg child for merging and trimming, and the
// Windows yt-dlp.exe is a PyInstaller bundle that itself runs as more than
// one process. exec.CommandContext on its own kills only the direct child on
// cancel, which would leave the rest of the tree running and holding the
// output file open. newToolCommand wires cancellation to the per-OS
// configureProcessTree (sys_windows.go, sys_others.go) instead.
//
// newOutputScanner and drainOutput read a running tool's output so that a
// long line cannot leave the tool blocked on a full pipe.
package main

import (
	"bufio"
	"context"
	"io"
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

// maxOutputLine is the longest line read from a running tool's output.
// bufio.Scanner's default limit is 64 KiB.
const maxOutputLine = 1 << 20

// newOutputScanner returns a scanner for a running tool's output that
// accepts lines up to maxOutputLine bytes. If it stops early anyway (a
// longer line, or a read error), drain the reader with drainOutput.
func newOutputScanner(output io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 0, 64*1024), maxOutputLine)
	return scanner
}

// drainOutput reads output to the end and discards it, calling onRead (when
// not nil) after each read. A tool whose output pipe is no longer read
// blocks once the pipe is full, and then never exits, so Wait never
// returns: cmd.WaitDelay only applies once the process has exited.
func drainOutput(output io.Reader, onRead func()) {
	buf := make([]byte, 32*1024)
	for {
		n, err := output.Read(buf)
		if n > 0 && onRead != nil {
			onRead()
		}
		if err != nil {
			return
		}
	}
}
