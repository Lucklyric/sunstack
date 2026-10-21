//go:build !windows

package core

import (
	"fmt"
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

// script puts one stand-in on PATH with a raw shell body.
func scripts(t *testing.T, bodies map[string]string) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range bodies {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	old := cmdWait
	cmdWait = 200 * time.Millisecond
	t.Cleanup(func() { cmdWait = old })
}

// A timeout is "unknown", never a conclusion that changes what a session or
// lock is taken to be.
func TestTimeoutIsUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stand-ins are shell scripts")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	t.Setenv("TMUX", "")
	sock := filepath.Join(t.TempDir(), "sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	block := "exec " + sleep + " 30"

	// The default server lists its panes but cannot be named: its panes are
	// left out, so no later command reaches another server's pane by ID.
	scripts(t, map[string]string{"tmux": `case "$*" in *list-sessions*) ` + block + ` ;; *) printf '/dev/ttys001\t%%1\ts:0.0\t\n' ;; esac`})
	if got := tmuxPanes(); len(got) != 0 {
		t.Errorf("unnamed default server's panes kept: %v", got)
	}

	// tmux answers, ps hangs: unknown, not "not running".
	scripts(t, map[string]string{"tmux": "echo /dev/ttys001", "ps": block})
	if runs, known := paneTool(sock, "%1", "claude"); runs || known {
		t.Errorf("paneTool = %v, %v; want unknown", runs, known)
	}

	// A server that does not answer is not a restarted one; one that answers
	// with another pid is.
	scripts(t, map[string]string{"tmux": block})
	if serverChanged(&Live{TmuxSocket: sock, TmuxPane: "%1", TmuxServer: "123"}) {
		t.Error("a server that does not answer counted as restarted")
	}
	scripts(t, map[string]string{"tmux": "echo 999"})
	if !serverChanged(&Live{TmuxSocket: sock, TmuxPane: "%1", TmuxServer: "123"}) {
		t.Error("a server with another pid not counted as restarted")
	}

	// A legacy lock of a live owner: UTC start differs, local start times
	// out. The lock is kept.
	live := exec.Command(sleep, "30")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	defer live.Process.Kill()
	scripts(t, map[string]string{"ps": `if [ "$TZ" = UTC ]; then echo 'Tue Oct 21 10:00:00 2026'; else ` + block + `; fi`})
	if ownerDead([]byte(fmt.Sprintf("pid %d start Tue_Oct_21_04:00:00_2026", live.Process.Pid))) {
		t.Error("a live legacy owner was taken as dead when its local start time timed out")
	}
}
