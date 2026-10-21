//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// standIns puts a tmux and a ps on PATH: each answers at once with out, or
// blocks when out is "block", as a stuck tmux server or ps would.
func standIns(t *testing.T, sleep, tmux, ps string) {
	t.Helper()
	bin := t.TempDir()
	for name, out := range map[string]string{"tmux": tmux, "ps": ps} {
		body := "#!/bin/sh\necho '" + out + "'\n"
		if out == "block" {
			body = "#!/bin/sh\nexec " + sleep + " 30\n"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	old := cmdWait
	cmdWait = 200 * time.Millisecond
	t.Cleanup(func() { cmdWait = old })
}

// Every tmux and ps call on the snapshot path gives up after cmdWait.
func TestScanCallsTimeOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-ins are shell scripts")
	}
	sleep, err := exec.LookPath("sleep") // before PATH holds only the stand-ins
	if err != nil {
		t.Skip("no sleep")
	}
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("SUNSTACK_CODEX_SCAN", "")
	bound := func(name string, f func()) {
		t.Helper()
		start := time.Now()
		f()
		if took := time.Since(start); took < cmdWait || took > 4*time.Second {
			t.Errorf("%s returned after %s, want the %s timeout", name, took, cmdWait)
		}
	}
	sock := filepath.Join(t.TempDir(), "sock") // a socket path that exists
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	standIns(t, sleep, "block", "block")
	bound("tmuxPanes", func() { tmuxPanes() })
	bound("paneScreen", func() { paneScreen("", "%1") })
	bound("serverPID", func() { serverPID(sock) })
	bound("TmuxWhere", func() {
		if w := TmuxWhere(sock, "%1"); w != "tmux unavailable" {
			t.Errorf("TmuxWhere on a server that does not answer = %q", w)
		}
	})
	bound("paneGone", func() {
		if paneGone(&Live{TmuxSocket: sock, TmuxPane: "%1"}) {
			t.Error("a pane on a server that does not answer counts as gone")
		}
	})
	bound("paneRunsTool tmux", func() { paneRunsTool("", "%1", "claude") })
	bound("processTTY", func() { processTTY(1) })
	bound("processAlive", func() { processAlive(1) })
	bound("ProcStart", func() { ProcStart(1) })
	bound("procStartLocal", func() { procStartLocal(1) })
	bound("codexSessions", func() { codexSessions() })

	standIns(t, sleep, "ttys001", "block") // tmux answers, ps hangs
	bound("paneRunsTool ps", func() { paneRunsTool("", "%1", "claude") })
}
