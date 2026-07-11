// dependency_service.go — Isolates binary discovery, dependency checks, and yt-dlp updater execution.
//
// Responsibilities:
//   - DependencyService: resolves bundled binary paths (bin/ beside the exe or
//     system PATH fallback), checks required tools are available, and runs the
//     yt-dlp self-update command.
//   - UpdateCallbacks: bridges update events back to the UI layer without any
//     Fyne dependency.
//   - Package-level UpdateYtDlpCLI for headless --update flag use.
package main

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DependencyService resolves bundled binary paths and checks tool availability.
// It has no UI or Fyne dependency.
type DependencyService struct {
	binDir string // absolute path to the bin/ directory beside the executable
}

// NewDependencyService returns a DependencyService pointed at the bin/
// directory beside the running executable.
func NewDependencyService() *DependencyService {
	binDir := filepath.Join(".", "bin")
	if exePath, err := os.Executable(); err == nil {
		binDir = filepath.Join(filepath.Dir(exePath), "bin")
	}
	return &DependencyService{binDir: binDir}
}

// ── Binary resolution ─────────────────────────────────────────────────────────

// LocalPath returns the absolute path to toolName inside binDir.
// On Windows, .exe is appended when not already present.
// The returned path may or may not exist on disk.
func (svc *DependencyService) LocalPath(toolName string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(toolName, ".exe") {
		toolName += ".exe"
	}
	return filepath.Join(svc.binDir, toolName)
}

// Resolve returns the path to use for toolName: the bundled binary in binDir
// when it exists on disk, otherwise the bare name for system PATH lookup.
func (svc *DependencyService) Resolve(toolName string) string {
	path := svc.LocalPath(toolName)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return toolName
}

// ── Dependency check ──────────────────────────────────────────────────────────

// Check verifies that yt-dlp and ffmpeg are reachable (bundled or in PATH).
// For each missing tool, onWarning is called with a human-readable message.
func (svc *DependencyService) Check(onWarning func(msg string)) {
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		_, localErr := os.Stat(svc.LocalPath(tool))
		_, pathErr := exec.LookPath(tool)
		if localErr != nil && pathErr != nil {
			onWarning(fmt.Sprintf("[WARNING] '%s' not found in PATH or ./bin/. Please install it.", tool))
		}
	}
}

// ── yt-dlp updater ────────────────────────────────────────────────────────────

// UpdateCallbacks bridges yt-dlp update events to the UI layer.
// Methods are called from a background goroutine; callers that require
// UI-thread safety must wrap them accordingly (e.g. via fyne.Do internally
// in appendOutput / updateStatus).
type UpdateCallbacks struct {
	// OnLog is called for each line of yt-dlp output and for system messages.
	OnLog func(line string, col color.Color)
	// OnStatus is called to update the short status label.
	OnStatus func(msg string)
	// OnSuccess is called when yt-dlp exits without error.
	OnSuccess func()
	// OnFailure is called when yt-dlp exits with an error.
	OnFailure func()
}

// RunUpdate executes 'yt-dlp -U' in a background goroutine and reports
// progress through cb. It returns immediately.
func (svc *DependencyService) RunUpdate(cb UpdateCallbacks) {
	go func() {
		ytDlpPath := svc.Resolve("yt-dlp")
		cmd := exec.Command(ytDlpPath, "-U")
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()

		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if cb.OnLog != nil {
				cb.OnLog(line, colOutputLine)
			}
		}

		if err != nil {
			if cb.OnLog != nil {
				cb.OnLog(fmt.Sprintf("[ERROR] Update failed: %v", err), colError)
			}
			if cb.OnStatus != nil {
				cb.OnStatus("Status: Update failed.")
			}
			if cb.OnFailure != nil {
				cb.OnFailure()
			}
		} else {
			if cb.OnLog != nil {
				cb.OnLog("[SYSTEM] yt-dlp is up to date.", colSuccess)
			}
			if cb.OnStatus != nil {
				cb.OnStatus("Status: yt-dlp updated.")
			}
			if cb.OnSuccess != nil {
				cb.OnSuccess()
			}
		}
	}()
}

// UpdateYtDlpCLI runs 'yt-dlp -U' synchronously and prints its output to
// stdout. Used for the --update CLI flag; does not require a running Fyne
// application.
func UpdateYtDlpCLI() {
	fmt.Println("Updating yt-dlp...")
	cmd := exec.Command("yt-dlp", "-U")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
