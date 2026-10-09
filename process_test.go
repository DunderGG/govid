package main

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartReapedCollectsTheProcess(t *testing.T) {
	useFakeTool(t, "version")
	cmd := exec.Command(fakeToolPath(t), "--version")

	reaped, err := startReaped(cmd)
	if err != nil {
		t.Fatalf("startReaped: %v", err)
	}
	select {
	case <-reaped:
	case <-time.After(30 * time.Second):
		t.Fatal("the process was not collected after it exited")
	}
	// Wait sets ProcessState; a process that is only started never gets one.
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Errorf("ProcessState = %v, want the exited process", cmd.ProcessState)
	}
}

func TestStartReapedReportsAFailedStart(t *testing.T) {
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing-file-manager"))

	if reaped, err := startReaped(cmd); err == nil || reaped != nil {
		t.Errorf("startReaped(missing program) = %v, %v; want no channel and an error", reaped, err)
	}
}
