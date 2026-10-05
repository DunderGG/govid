package main

import (
	"image/color"
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
		{"all bundled", []string{"yt-dlp", "ffmpeg"}, nil},
		{"ffmpeg missing", []string{"yt-dlp"}, []string{"'ffmpeg' not found"}},
		{"nothing installed", nil, []string{"'yt-dlp' not found", "'ffmpeg' not found"}},
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
