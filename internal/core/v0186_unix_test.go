//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A tmux that stops answering at the key press does not hold the hook
// review sweep, and so its lock (§46.4, review of v0.18.6).
func TestTrustAnswerGivesUpOnStuckTmux(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	bin := t.TempDir()
	body := `#!/bin/sh
case "$*" in
*send-keys*) exec ` + sleep + ` 30 ;;
esac
cat <<'SCREEN'
Hooks need review
2 hooks are new or changed.
Hooks can run outside the sandbox after you trust them.

  1. Review hooks
› 2. Trust all and continue
  3. Continue without trusting (hooks won't run)

enter confirm · esc skip
SCREEN
`
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := cmdWait
	cmdWait = 200 * time.Millisecond
	t.Cleanup(func() { cmdWait = old })
	if _, ok := parseMenu(paneScreen("sock", "%0"), trustScreens[0]); !ok {
		t.Fatal("the stand-in does not draw the known screen")
	}
	start := time.Now()
	if selectAnswer("sock", "%0", trustScreens[0]) {
		t.Error("answered through a tmux that never took the key")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the answer waited %s on a stuck tmux", d)
	}
}
