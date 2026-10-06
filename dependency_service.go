// dependency_service.go — Isolates binary discovery, dependency checks, and yt-dlp updater execution.
//
// Responsibilities:
//   - DependencyService: resolves bundled binary paths (bin/ beside the exe or
//     system PATH fallback), checks required tools are available, and runs the
//     yt-dlp self-update command.
//   - UpdateCallbacks: bridges update events back to the UI layer without any
//     Fyne dependency.
//   - UpdateCLI for headless --update flag use.
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

	// isWritable reports whether files can be created in a directory; nil
	// means dirWritable. Replaced in tests.
	isWritable func(dir string) bool
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

// Check verifies that yt-dlp and ffmpeg, and the optional ffprobe, are
// reachable (bundled or in PATH). For each missing tool, onWarning is called
// with a human-readable message.
func (svc *DependencyService) Check(onWarning func(msg string)) {
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		if !svc.available(tool) {
			onWarning(fmt.Sprintf("[WARNING] '%s' not found in PATH or ./bin/. Please install it.", tool))
		}
	}
	// PPEngine uses ffprobe for frame counts and durations. It is not bundled,
	// and post-processing still works without it, just without a percentage.
	if !svc.available("ffprobe") {
		onWarning("[WARNING] Optional 'ffprobe' not found in PATH or ./bin/. Post-processing progress will not show a percentage.")
	}
}

// available reports whether toolName exists in binDir or on the system PATH.
func (svc *DependencyService) available(toolName string) bool {
	if _, err := os.Stat(svc.LocalPath(toolName)); err == nil {
		return true
	}
	_, err := exec.LookPath(toolName)
	return err == nil
}

// Version runs "<toolName> --version" (resolved via Resolve) and returns its
// trimmed output. Used to display the installed version alongside the latest
// available one.
func (svc *DependencyService) Version(toolName string) (string, error) {
	cmd := exec.Command(svc.Resolve(toolName), "--version")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version failed: %w", toolName, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ── yt-dlp updater ────────────────────────────────────────────────────────────

// UpdateCallbacks bridges yt-dlp update events to the UI layer. Every field
// must be set. Methods are called from a background goroutine; callers that require
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

		// If the subprocess exits non-zero, CombinedOutput() returns an error that is typically *exec.ExitError.
		out, err := cmd.CombinedOutput()

		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			cb.OnLog(line, colOutputLine)
		}

		if err != nil {
			cb.OnLog(fmt.Sprintf("[ERROR] Update failed: %v", err), colError)
			if hint := svc.updateFailureHint(); hint != "" {
				cb.OnLog("[ERROR] "+hint, colError)
				cb.OnStatus("Status: Update failed — the yt-dlp folder is not writable.")
			} else {
				cb.OnStatus("Status: Update failed.")
			}
			cb.OnFailure()
		} else {
			cb.OnLog("[SYSTEM] yt-dlp is up to date.", colSuccess)
			cb.OnStatus("Status: yt-dlp updated.")
			cb.OnSuccess()
		}
	}()
}

// UpdateCLI runs 'yt-dlp -U' synchronously and prints its output to stdout.
// Like RunUpdate, it updates the bundled yt-dlp when one exists. Used for the
// --update CLI flag; does not require a running Fyne application.
func (svc *DependencyService) UpdateCLI() error {
	fmt.Println("Updating yt-dlp...")
	cmd := exec.Command(svc.Resolve("yt-dlp"), "-U")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		if hint := svc.updateFailureHint(); hint != "" {
			return fmt.Errorf("yt-dlp update failed: %w\n%s", err, hint)
		}
		return fmt.Errorf("yt-dlp update failed: %w", err)
	}
	return nil
}

// updateFailureHint explains a failed update when the folder holding yt-dlp
// cannot be written to (for example under Program Files), since yt-dlp -U
// replaces the yt-dlp executable in place. It returns "" when the folder is
// writable or cannot be found.
func (svc *DependencyService) updateFailureHint() string {
	dir := svc.ytDlpDir()
	if dir == "" || svc.writable(dir) {
		return ""
	}
	return fmt.Sprintf("GoVid cannot write to %s, which yt-dlp needs to update itself. "+
		"Run GoVid as administrator once to update it, or move GoVid to a folder you can write to, such as one in your user folder.", dir)
}

// ytDlpDir returns the folder holding the yt-dlp that Resolve picks, or ""
// when yt-dlp cannot be found.
func (svc *DependencyService) ytDlpDir() string {
	path := svc.Resolve("yt-dlp")
	if !filepath.IsAbs(path) {
		found, err := exec.LookPath(path)
		if err != nil {
			return ""
		}
		path = found
	}
	return filepath.Dir(path)
}

// writable reports whether files can be created in dir.
func (svc *DependencyService) writable(dir string) bool {
	if svc.isWritable != nil {
		return svc.isWritable(dir)
	}
	return dirWritable(dir)
}

// dirWritable reports whether a file can be created in dir, by creating and
// removing one.
func dirWritable(dir string) bool {
	file, err := os.CreateTemp(dir, ".govid-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	file.Close()
	os.Remove(name)
	return true
}
