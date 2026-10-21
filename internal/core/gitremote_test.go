package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A git that never answers (macOS holding a protected folder for a service)
// must not stall the snapshot: gitRemote gives up after gitWait.
func TestGitRemoteTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-in git is a shell script")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	bin := t.TempDir()
	// A git that blocks: it must reach the sleep, so its path is absolute.
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec "+sleep+" 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	old := gitWait
	gitWait = 200 * time.Millisecond
	defer func() { gitWait = old }()
	start := time.Now()
	r := gitRemote(t.TempDir())
	if took := time.Since(start); r != "" || took < gitWait || took > 3*time.Second {
		t.Fatalf("gitRemote = %q after %s, want \"\" after the %s timeout", r, took, gitWait)
	}
}
