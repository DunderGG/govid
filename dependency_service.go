// dependency_service.go — Isolates binary discovery, dependency checks, and yt-dlp updater execution.
//
// Responsibilities:
//   - DependencyService: resolves bundled binary paths (bin/ beside the exe or
//     system PATH fallback), checks required tools are available, reports
//     where each tool was found and its version, and runs the yt-dlp
//     self-update command.
//   - JSRuntime: the JavaScript runtime (bin/deno, or deno, node, or bun on
//     PATH) yt-dlp needs for YouTube's player challenges, found once and
//     cached.
//   - UpdateCallbacks: bridges update events back to the UI layer without any
//     Fyne dependency.
//   - UpdateCLI for headless --update flag use.
package main

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DependencyService resolves bundled binary paths and checks tool availability.
// It has no UI or Fyne dependency.
type DependencyService struct {
	binDir string // absolute path to the bin/ directory beside the executable

	// isWritable reports whether files can be created in a directory; nil
	// means dirWritable. Replaced in tests.
	isWritable func(dir string) bool

	// The JavaScript runtime search, cached; nil until the first search.
	runtimeMu     sync.Mutex
	runtimeResult *jsRuntimeResult
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

// Version returns the version toolName (resolved via Resolve) reports, as
// runVersion does. Used to display the installed version alongside the
// latest available one.
func (svc *DependencyService) Version(toolName string) (string, error) {
	return runVersion(svc.Resolve(toolName), toolName)
}

// toolCommandTimeout bounds how long a query of a tool, such as its version
// or FFmpeg's filter list, may take. A freshly downloaded executable can be
// slow to start the first time while antivirus software scans it. A
// variable so that tests can shorten it.
var toolCommandTimeout = 15 * time.Second

// ytDlpUpdateTimeout bounds how long "yt-dlp -U" may take. It downloads the
// new yt-dlp, so a hung connection would otherwise leave the update, and
// the status saying so, running until GoVid exits. A variable so that tests
// can shorten it.
var ytDlpUpdateTimeout = 5 * time.Minute

// versionPattern finds a dotted version number, e.g. "8.1" in "ffmpeg
// version 8.1-essentials_build", "2.9.7" in "deno 2.9.7 (stable, …)", or
// yt-dlp's "2026.03.17".
var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// parseToolVersion returns the version a tool's version output names: the
// first dotted number on its first line. A line without one, such as a git
// build of ffmpeg's "ffmpeg version N-118000-gabc Copyright …", gives the
// word after "version", or else the whole line.
func parseToolVersion(output string) string {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	firstLine = strings.TrimSpace(firstLine)
	if match := versionPattern.FindString(firstLine); match != "" {
		return match
	}
	if _, after, found := strings.Cut(firstLine, "version "); found {
		if fields := strings.Fields(after); len(fields) > 0 {
			return fields[0]
		}
	}
	return firstLine
}

// locate returns the path of toolName and whether it is the one in binDir,
// which takes precedence over one on PATH. ok is false when neither exists.
func (svc *DependencyService) locate(toolName string) (path string, inBin, ok bool) {
	local := svc.LocalPath(toolName)
	if _, err := os.Stat(local); err == nil {
		return local, true, true
	}
	found, err := exec.LookPath(toolName)
	if err != nil {
		return "", false, false
	}
	return found, false, true
}

// versionArgs returns the arguments that make toolName print its version.
func versionArgs(toolName string) []string {
	if toolName == "ffmpeg" || toolName == "ffprobe" {
		return []string{"-version"}
	}
	return []string{"--version"}
}

// runVersion runs the executable at path with toolName's version arguments
// and returns the version it reports. It gives up after toolCommandTimeout.
func runVersion(path, toolName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolCommandTimeout)
	defer cancel()
	// newToolCommand, so that a timeout also kills the second process of the
	// Windows yt-dlp.exe, which would otherwise keep Output waiting on its pipe.
	cmd := newToolCommand(ctx, path, versionArgs(toolName)...)
	out, err := cmd.Output()
	if err != nil {
		err = commandError(ctx, err, toolCommandTimeout)
		return "", fmt.Errorf("%s %s failed: %w", toolName, strings.Join(versionArgs(toolName), " "), err)
	}
	version := parseToolVersion(string(out))
	if version == "" {
		return "", fmt.Errorf("%s did not report a version", toolName)
	}
	return version, nil
}

// InstalledTool is where a tool was found and the version it reports.
type InstalledTool struct {
	Path    string
	InBin   bool   // the copy in bin/, rather than one on PATH
	Version string // "" when it did not say
}

// Found reports whether the tool was found at all.
func (tool InstalledTool) Found() bool {
	return tool.Path != ""
}

// Source names where the tool was found: "bin/" or "PATH".
func (tool InstalledTool) Source() string {
	if tool.InBin {
		return "bin/"
	}
	return "PATH"
}

// Installed finds toolName (bin/ first, then PATH) and asks it for its
// version. It runs the tool, so call it off the UI thread.
func (svc *DependencyService) Installed(toolName string) InstalledTool {
	path, inBin, ok := svc.locate(toolName)
	if !ok {
		return InstalledTool{}
	}
	version, _ := runVersion(path, toolName)
	return InstalledTool{Path: path, InBin: inBin, Version: version}
}

// ── JavaScript runtime ────────────────────────────────────────────────────────

// JSRuntime is a JavaScript runtime yt-dlp can use to solve YouTube's player
// challenges. Without one, yt-dlp falls back to a deprecated YouTube client
// that may miss formats.
type JSRuntime struct {
	Name    string // yt-dlp's name for it: "deno", "node", or "bun"
	Path    string
	Version string
	InBin   bool // the Deno in bin/, rather than a runtime on PATH
}

// Arg returns the value of yt-dlp's --js-runtimes option for the runtime.
// The path is always given, since bin/ is not on PATH.
func (rt JSRuntime) Arg() string {
	return rt.Name + ":" + rt.Path
}

// Label describes the runtime for the log and the About window, e.g.
// "deno 2.9.7 (bin/)".
func (rt JSRuntime) Label() string {
	return fmt.Sprintf("%s %s (%s)", rt.Name, rt.Version, rt.source())
}

// source names where the runtime was found: "bin/" or "PATH".
func (rt JSRuntime) source() string {
	if rt.InBin {
		return "bin/"
	}
	return "PATH"
}

// jsRuntimeRule is the range of versions of a runtime yt-dlp supports, from
// its EJS wiki page (github.com/yt-dlp/yt-dlp/wiki/EJS); max is "" for no
// upper limit.
type jsRuntimeRule struct {
	name, min, max string
}

// jsRuntimeRules lists the runtimes GoVid looks for, in yt-dlp's order of
// preference. yt-dlp also supports QuickJS, which is rarely installed and
// can take minutes per challenge in older versions.
var jsRuntimeRules = []jsRuntimeRule{
	{name: "deno", min: "2.3.0"},
	{name: "node", min: "22.0.0"},
	{name: "bun", min: "1.2.11", max: "1.3.14"}, // deprecated by yt-dlp
}

// supports reports whether yt-dlp supports version of the runtime, and if
// not, why.
func (rule jsRuntimeRule) supports(version string) (bool, string) {
	switch {
	case compareVersions(version, rule.min) < 0:
		return false, fmt.Sprintf("too old for yt-dlp (needs %s or newer)", rule.min)
	case rule.max != "" && compareVersions(version, rule.max) > 0:
		return false, fmt.Sprintf("too new for yt-dlp (supports up to %s)", rule.max)
	default:
		return true, ""
	}
}

// jsRuntimeResult is the outcome of looking for a runtime.
type jsRuntimeResult struct {
	runtime JSRuntime
	found   bool
	skipped []string // runtimes that were found but cannot be used, and why
}

// JSRuntime returns the runtime yt-dlp should use: the Deno in bin/, or
// else the first supported deno, node, or bun on PATH. ok is false when
// there is none. The answer is cached; ResetJSRuntime forgets it after a
// runtime is installed. The first call runs each candidate, so call it off
// the UI thread.
func (svc *DependencyService) JSRuntime() (rt JSRuntime, ok bool) {
	result := svc.jsRuntime()
	return result.runtime, result.found
}

// JSRuntimeNotes returns why runtimes that were found cannot be used, e.g.
// "node 20.11.0 on PATH is too old for yt-dlp (needs 22.0.0 or newer)".
func (svc *DependencyService) JSRuntimeNotes() []string {
	return svc.jsRuntime().skipped
}

// ResetJSRuntime makes the next JSRuntime call look for a runtime again.
func (svc *DependencyService) ResetJSRuntime() {
	svc.runtimeMu.Lock()
	defer svc.runtimeMu.Unlock()
	svc.runtimeResult = nil
}

// jsRuntime returns the cached runtime search, searching first if needed.
func (svc *DependencyService) jsRuntime() jsRuntimeResult {
	svc.runtimeMu.Lock()
	defer svc.runtimeMu.Unlock()
	if svc.runtimeResult == nil {
		result := svc.findJSRuntime()
		svc.runtimeResult = &result
	}
	return *svc.runtimeResult
}

// findJSRuntime looks for a supported runtime: bin/deno first, then deno,
// node, and bun on PATH.
func (svc *DependencyService) findJSRuntime() jsRuntimeResult {
	var candidates []JSRuntime
	if local := svc.LocalPath("deno"); fileExists(local) {
		candidates = append(candidates, JSRuntime{Name: "deno", Path: local, InBin: true})
	}
	for _, rule := range jsRuntimeRules {
		if path, err := exec.LookPath(rule.name); err == nil {
			candidates = append(candidates, JSRuntime{Name: rule.name, Path: path})
		}
	}

	var result jsRuntimeResult
	for _, candidate := range candidates {
		version, err := runVersion(candidate.Path, candidate.Name)
		if err != nil {
			result.skipped = append(result.skipped, fmt.Sprintf("%s at %s could not be run: %v", candidate.Name, candidate.Path, err))
			continue
		}
		candidate.Version = version
		if ok, why := ruleFor(candidate.Name).supports(version); !ok {
			result.skipped = append(result.skipped, fmt.Sprintf("%s %s in %s is %s", candidate.Name, version, candidate.source(), why))
			continue
		}
		result.runtime, result.found = candidate, true
		return result
	}
	return result
}

// ruleFor returns the version rule of the runtime called name.
func ruleFor(name string) jsRuntimeRule {
	for _, rule := range jsRuntimeRules {
		if rule.name == name {
			return rule
		}
	}
	return jsRuntimeRule{name: name}
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

// runYtDlpUpdate runs "yt-dlp -U" and returns its output, stdout and stderr
// together. It kills yt-dlp, and returns an error that says so, after
// ytDlpUpdateTimeout. A failed update's error is typically an
// *exec.ExitError.
func (svc *DependencyService) runYtDlpUpdate() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ytDlpUpdateTimeout)
	defer cancel()
	out, err := newToolCommand(ctx, svc.Resolve("yt-dlp"), "-U").CombinedOutput()
	return out, commandError(ctx, err, ytDlpUpdateTimeout)
}

// RunUpdate executes 'yt-dlp -U' in a background goroutine and reports
// progress through cb. It returns immediately.
func (svc *DependencyService) RunUpdate(cb UpdateCallbacks) {
	go func() {
		out, err := svc.runYtDlpUpdate()

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
	out, err := svc.runYtDlpUpdate()
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
