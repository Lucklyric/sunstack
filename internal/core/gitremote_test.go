package core

import (
	"os"
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
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	old := gitWait
	gitWait = 200 * time.Millisecond
	defer func() { gitWait = old }()
	start := time.Now()
	if r := gitRemote(t.TempDir()); r != "" || time.Since(start) > 3*time.Second {
		t.Fatalf("gitRemote = %q after %s", r, time.Since(start))
	}
}
