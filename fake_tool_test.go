package main

// fake_tool_test.go — Lets the test binary stand in for external tools.
//
// When GOVID_FAKE_TOOL is set, TestMain runs the requested fake behaviour
// instead of the test suite and exits. Tests point a service at the test
// binary (directly, or via installFakeTool under the expected tool name) and
// select the behaviour with useFakeTool, so no real yt-dlp or FFmpeg is needed.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// fakeToolEnv selects the fake tool behaviour in the child process.
const fakeToolEnv = "GOVID_FAKE_TOOL"

// fakeToolVersion is the version string the "version" mode reports.
const fakeToolVersion = "2026.09.01"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeToolEnv); mode != "" {
		os.Exit(runFakeTool(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runFakeTool implements the fake tool modes and returns the exit code.
func runFakeTool(mode string, args []string) int {
	switch mode {
	case "version":
		if !slices.Contains(args, "--version") {
			fmt.Fprintf(os.Stderr, "fake tool: expected --version, got %q\n", args)
			return 4
		}
		fmt.Printf("  %s  \n", fakeToolVersion)
		return 0
	case "update":
		if !slices.Contains(args, "-U") {
			fmt.Fprintf(os.Stderr, "fake tool: expected -U, got %q\n", args)
			return 4
		}
		fmt.Println("Current version: stable@" + fakeToolVersion)
		fmt.Println("yt-dlp is up to date (stable@" + fakeToolVersion + ")")
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "ERROR: fake tool failure")
		return 1
	}
	fmt.Fprintf(os.Stderr, "fake tool: unknown mode %q\n", mode)
	return 3
}

// useFakeTool makes child processes started by this test run the given fake
// mode. It uses t.Setenv, so tests calling it cannot run in parallel.
func useFakeTool(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(fakeToolEnv, mode)
}

// fakeToolPath returns the path of the running test binary, which acts as
// the fake tool when started with fakeToolEnv set.
func fakeToolPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return exe
}

// installFakeTool places a copy of the test binary in dir under toolName
// (with .exe appended on Windows) and returns its path.
func installFakeTool(t *testing.T, dir, toolName string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	dst := filepath.Join(dir, toolName)

	// Copy rather than hard-link: on Windows a link to the running test
	// binary shares its lock and cannot be removed by t.TempDir cleanup.
	in, err := os.Open(fakeToolPath(t))
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		t.Fatalf("create fake tool: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("copy fake tool: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close fake tool: %v", err)
	}
	return dst
}
