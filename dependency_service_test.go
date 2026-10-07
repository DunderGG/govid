package main

import (
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolatePath points PATH at an empty directory so the real yt-dlp or FFmpeg
// installed on the developer's machine cannot leak into the test.
func isolatePath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestDependencyLocalPath(t *testing.T) {
	binDir := filepath.Join("opt", "govid", "bin")
	svc := &DependencyService{binDir: binDir}

	want := filepath.Join(binDir, "yt-dlp")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := svc.LocalPath("yt-dlp"); got != want {
		t.Errorf("LocalPath(yt-dlp) = %q, want %q", got, want)
	}

	// A name that already carries .exe is never double-suffixed.
	if got, want := svc.LocalPath("ffmpeg.exe"), filepath.Join(binDir, "ffmpeg.exe"); got != want {
		t.Errorf("LocalPath(ffmpeg.exe) = %q, want %q", got, want)
	}
}

func TestDependencyResolve(t *testing.T) {
	binDir := t.TempDir()
	svc := &DependencyService{binDir: binDir}
	bundled := installFakeTool(t, binDir, "yt-dlp")

	if got := svc.Resolve("yt-dlp"); got != bundled {
		t.Errorf("Resolve(yt-dlp) = %q, want bundled %q", got, bundled)
	}
	if got := svc.Resolve("ffmpeg"); got != "ffmpeg" {
		t.Errorf("Resolve(ffmpeg) = %q, want bare name for PATH lookup", got)
	}
}

func TestNewDependencyServiceUsesBinBesideExecutable(t *testing.T) {
	svc := NewDependencyService()
	want := filepath.Join(filepath.Dir(fakeToolPath(t)), "bin")
	if svc.binDir != want {
		t.Errorf("binDir = %q, want %q", svc.binDir, want)
	}
}

func TestDependencyCheck(t *testing.T) {
	tests := []struct {
		name         string
		bundled      []string
		wantWarnings []string
	}{
		{"all bundled", []string{"yt-dlp", "ffmpeg", "ffprobe"}, nil},
		{"ffmpeg missing", []string{"yt-dlp", "ffprobe"}, []string{"'ffmpeg' not found"}},
		{"optional ffprobe missing", []string{"yt-dlp", "ffmpeg"}, []string{"Optional 'ffprobe' not found"}},
		{"nothing installed", nil, []string{"'yt-dlp' not found", "'ffmpeg' not found", "'ffprobe' not found"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolatePath(t)
			binDir := t.TempDir()
			for _, tool := range tt.bundled {
				installFakeTool(t, binDir, tool)
			}
			svc := &DependencyService{binDir: binDir}

			var warnings []string
			svc.Check(func(msg string) { warnings = append(warnings, msg) })

			if len(warnings) != len(tt.wantWarnings) {
				t.Fatalf("warnings = %q, want %d warnings", warnings, len(tt.wantWarnings))
			}
			for i, want := range tt.wantWarnings {
				if !strings.Contains(warnings[i], want) || !strings.HasPrefix(warnings[i], "[WARNING]") {
					t.Errorf("warnings[%d] = %q, want [WARNING] containing %q", i, warnings[i], want)
				}
			}
		})
	}
}

func TestDependencyCheckFindsToolsOnPath(t *testing.T) {
	pathDir := t.TempDir()
	installFakeTool(t, pathDir, "yt-dlp")
	installFakeTool(t, pathDir, "ffmpeg")
	installFakeTool(t, pathDir, "ffprobe")
	t.Setenv("PATH", pathDir)

	svc := &DependencyService{binDir: t.TempDir()}
	svc.Check(func(msg string) { t.Errorf("unexpected warning: %s", msg) })
}

func TestDependencyVersion(t *testing.T) {
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	svc := &DependencyService{binDir: binDir}
	useFakeTool(t, "version")

	got, err := svc.Version("yt-dlp")
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if got != fakeToolVersion {
		t.Errorf("Version() = %q, want trimmed %q", got, fakeToolVersion)
	}
}

func TestDependencyVersionFailure(t *testing.T) {
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	svc := &DependencyService{binDir: binDir}
	useFakeTool(t, "fail")

	got, err := svc.Version("yt-dlp")
	if err == nil {
		t.Fatalf("Version() = %q, want error for failing tool", got)
	}
	if got != "" || !strings.Contains(err.Error(), "yt-dlp --version failed") {
		t.Errorf("Version() = %q, %v; want empty result and wrapped error", got, err)
	}
}

func TestDependencyVersionMissingTool(t *testing.T) {
	isolatePath(t)
	svc := &DependencyService{binDir: t.TempDir()}

	if got, err := svc.Version("yt-dlp"); err == nil {
		t.Errorf("Version() = %q, want error for missing tool", got)
	}
}

// updateResult captures what RunUpdate reported through its callbacks.
type updateResult struct {
	lines   []string
	status  string
	success bool
}

// runUpdateAndWait calls RunUpdate and blocks until it reports success or failure.
func runUpdateAndWait(t *testing.T, svc *DependencyService) updateResult {
	t.Helper()
	var result updateResult
	done := make(chan struct{})
	svc.RunUpdate(UpdateCallbacks{
		OnLog:     func(line string, _ color.Color) { result.lines = append(result.lines, line) },
		OnStatus:  func(msg string) { result.status = msg },
		OnSuccess: func() { result.success = true; close(done) },
		OnFailure: func() { close(done) },
	})

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("RunUpdate did not finish")
	}
	return result
}

func TestDependencyRunUpdateSuccess(t *testing.T) {
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "update")

	result := runUpdateAndWait(t, &DependencyService{binDir: binDir})

	if !result.success {
		t.Fatalf("update reported failure, log: %q", result.lines)
	}
	if result.status != "Status: yt-dlp updated." {
		t.Errorf("status = %q", result.status)
	}
	joined := strings.Join(result.lines, "\n")
	for _, want := range []string{"yt-dlp is up to date (stable@" + fakeToolVersion + ")", "[SYSTEM] yt-dlp is up to date."} {
		if !strings.Contains(joined, want) {
			t.Errorf("log missing %q, got:\n%s", want, joined)
		}
	}
}

func TestDependencyRunUpdateFailure(t *testing.T) {
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "fail")

	result := runUpdateAndWait(t, &DependencyService{binDir: binDir})

	if result.success {
		t.Fatal("update reported success for failing tool")
	}
	if result.status != "Status: Update failed." {
		t.Errorf("status = %q", result.status)
	}
	joined := strings.Join(result.lines, "\n")
	for _, want := range []string{"ERROR: fake tool failure", "[ERROR] Update failed:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("log missing %q, got:\n%s", want, joined)
		}
	}
}

func TestDependencyRunUpdateExplainsUnwritableFolder(t *testing.T) {
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "fail")
	svc := &DependencyService{binDir: binDir, isWritable: func(string) bool { return false }}

	result := runUpdateAndWait(t, svc)

	if result.success {
		t.Fatal("update reported success for failing tool")
	}
	if !strings.Contains(result.status, "not writable") {
		t.Errorf("status = %q, want it to say the folder is not writable", result.status)
	}
	joined := strings.Join(result.lines, "\n")
	if !strings.Contains(joined, "GoVid cannot write to "+binDir) {
		t.Errorf("log does not explain the unwritable folder:\n%s", joined)
	}

	err := svc.UpdateCLI()
	if err == nil || !strings.Contains(err.Error(), "cannot write to") {
		t.Errorf("UpdateCLI() = %v, want the unwritable-folder hint", err)
	}
}

func TestDirWritable(t *testing.T) {
	dir := t.TempDir()
	if !dirWritable(dir) {
		t.Error("dirWritable(temp dir) = false, want true")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dirWritable left %d file(s) behind", len(entries))
	}
	if dirWritable(filepath.Join(dir, "missing")) {
		t.Error("dirWritable(missing dir) = true, want false")
	}
}

func TestDependencyUpdateCLIUsesBundledYtDlp(t *testing.T) {
	// With an empty PATH, the update can only succeed via the bundled binary.
	isolatePath(t)
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "update")

	if err := (&DependencyService{binDir: binDir}).UpdateCLI(); err != nil {
		t.Fatalf("UpdateCLI() = %v, want nil", err)
	}
}

func TestDependencyUpdateCLIFailure(t *testing.T) {
	isolatePath(t)
	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "fail")

	err := (&DependencyService{binDir: binDir}).UpdateCLI()
	if err == nil {
		t.Fatal("UpdateCLI() = nil, want error for failing tool")
	}
	if got := exitCodeFromError(err); got == 0 {
		t.Errorf("exitCodeFromError(%v) = 0, want a failure code", err)
	}
}

func TestParseToolVersion(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"2026.03.17\n", "2026.03.17"},
		{"ffmpeg version 8.1-essentials_build-www.gyan.dev Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with gcc 15.2.0", "8.1"},
		{"ffmpeg version N-118000-g1a2b3c4 Copyright (c) 2000-2026 the FFmpeg developers", "N-118000-g1a2b3c4"},
		{"deno 2.9.7 (stable, release, x86_64-pc-windows-msvc)\nv8 14.1.146.11-rusty\ntypescript 5.9.2", "2.9.7"},
		{"v24.16.0\n", "24.16.0"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := parseToolVersion(tt.output); got != tt.want {
			t.Errorf("parseToolVersion(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}

// runtimeDirs returns a DependencyService whose bin/ holds a fake Deno when
// binDeno is set, with PATH holding fake copies of the runtimes named in
// onPath.
func runtimeDirs(t *testing.T, binDeno bool, onPath ...string) *DependencyService {
	t.Helper()
	svc := &DependencyService{binDir: t.TempDir()}
	if binDeno {
		installFakeTool(t, svc.binDir, "deno")
	}
	pathDir := t.TempDir()
	for _, name := range onPath {
		installFakeTool(t, pathDir, name)
	}
	t.Setenv("PATH", pathDir)
	return svc
}

func TestJSRuntimePrefersTheDenoInBin(t *testing.T) {
	svc := runtimeDirs(t, true, "node", "deno")

	rt, ok := svc.JSRuntime()

	if !ok || rt.Name != "deno" || !rt.InBin || rt.Path != svc.LocalPath("deno") || rt.Version != "2.9.7" {
		t.Errorf("JSRuntime() = %+v, %v; want the Deno in bin/", rt, ok)
	}
	if got := rt.Label(); got != "deno 2.9.7 (bin/)" {
		t.Errorf("Label() = %q", got)
	}
	if got := rt.Arg(); got != "deno:"+svc.LocalPath("deno") {
		t.Errorf("Arg() = %q", got)
	}
}

func TestJSRuntimeFindsNodeOnPath(t *testing.T) {
	svc := runtimeDirs(t, false, "node")

	rt, ok := svc.JSRuntime()

	if !ok || rt.Name != "node" || rt.InBin || rt.Version != "24.16.0" {
		t.Errorf("JSRuntime() = %+v, %v; want Node from PATH", rt, ok)
	}
}

func TestJSRuntimeSkipsVersionsYtDlpDoesNotSupport(t *testing.T) {
	tests := []struct {
		name, version, wantNote string
	}{
		{"node", "20.11.0", "node 20.11.0 in PATH is too old for yt-dlp (needs 22.0.0 or newer)"},
		{"deno", "2.2.0", "deno 2.2.0 in PATH is too old for yt-dlp (needs 2.3.0 or newer)"},
		{"bun", "1.4.0", "bun 1.4.0 in PATH is too new for yt-dlp (supports up to 1.3.14)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := runtimeDirs(t, false, tt.name)
			t.Setenv(fakeRuntimeVersionEnv, tt.version)

			rt, ok := svc.JSRuntime()

			if ok {
				t.Errorf("JSRuntime() = %+v, want none", rt)
			}
			if notes := svc.JSRuntimeNotes(); len(notes) != 1 || notes[0] != tt.wantNote {
				t.Errorf("JSRuntimeNotes() = %q, want %q", notes, tt.wantNote)
			}
		})
	}
}

func TestJSRuntimeIsCachedUntilReset(t *testing.T) {
	svc := runtimeDirs(t, false)
	if _, ok := svc.JSRuntime(); ok {
		t.Fatal("found a runtime in empty folders")
	}
	installFakeTool(t, svc.binDir, "deno")

	if _, ok := svc.JSRuntime(); ok {
		t.Error("the search was not cached")
	}
	svc.ResetJSRuntime()
	if rt, ok := svc.JSRuntime(); !ok || !rt.InBin {
		t.Errorf("JSRuntime() after ResetJSRuntime = %+v, %v; want the new Deno", rt, ok)
	}
}

func TestDependencyInstalled(t *testing.T) {
	isolatePath(t)
	svc := &DependencyService{binDir: t.TempDir()}
	if tool := svc.Installed("deno"); tool.Found() {
		t.Errorf("Installed(deno) = %+v, want not found", tool)
	}
	installFakeTool(t, svc.binDir, "deno")

	tool := svc.Installed("deno")

	if !tool.Found() || !tool.InBin || tool.Version != "2.9.7" || tool.Source() != "bin/" {
		t.Errorf("Installed(deno) = %+v, want 2.9.7 in bin/", tool)
	}
}
