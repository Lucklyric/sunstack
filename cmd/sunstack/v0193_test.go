package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// A spawned session whose shell drops the tmux variables (Codex with
// shell_environment_policy inherit = "core") keeps the pane spawn recorded
// when it takes its claim with the spawn token.
func TestResumeKeepsSpawnedPane(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	s := privateTmux(t, p, env)
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "keep", "--tool", "codex", "--place", "here")
	expect(t, r, 0, "spawn")
	pane := field(paneRe, r.out)
	pr, err := core.FindProject(p)
	must(t, err)
	claims, err := pr.Claims("reviewer")
	must(t, err)
	if len(claims) != 1 {
		t.Fatalf("claims after spawn: %d", len(claims))
	}
	asEnv := append(append([]string{}, env...), "CODEX_THREAD_ID=00000000-0000-4000-8000-000000000020")
	expect(t, sh(t, p, asEnv, "as", "reviewer", "--token", claims[0].Token, "--tool", "codex"), 0, "resume without tmux variables")
	claims, err = pr.Claims("reviewer")
	must(t, err)
	if len(claims) != 1 || claims[0].TmuxPane != pane || claims[0].TmuxSocket == "" {
		t.Fatalf("claim after resume: pane %q socket %q, want pane %s", claims[0].TmuxPane, claims[0].TmuxSocket, pane)
	}
	r = sh(t, p, s.env, "sessions")
	expect(t, r, 0, "sessions")
	if out := r.out; strings.Contains(out, "not in tmux") || !strings.Contains(out, "reviewer_keep") {
		t.Errorf("sessions after resume:\n%s", r.out)
	}
}

// Beside a pane too small to halve, spawn opens a window of its own.
func TestSpawnSmallPaneOpensWindow(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.tmux(t, "resize-window", "-t", s.pane, "-x", "100", "-y", "30")
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "small", "--tool", "claude", "--place", "here")
	expect(t, r, 0, "spawn beside a small pane")
	requireContains(t, r.out, "in a new tmux window")
	s.tmux(t, "resize-window", "-t", s.pane, "-x", "200", "-y", "60")
	r = sh(t, p, s.env, "spawn", "reviewer", "--task", "large", "--tool", "claude", "--place", "here")
	expect(t, r, 0, "spawn beside a large pane")
	requireContains(t, r.out, "in a pane beside this one")
}

// A command from a Codex shell without tmux variables still finds its pane.
func TestCodexCallerFindsPane(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	out := filepath.Join(t.TempDir(), "out")
	line := fmt.Sprintf("cd %q && env -u TMUX -u TMUX_PANE CODEX_THREAD_ID=00000000-0000-4000-8000-000000000030 SUNSTACK_TMUX_SOCKET=%q %q name probe > %q 2>&1; echo done >> %q",
		p, s.socket, bin, out, out)
	s.tmux(t, "send-keys", "-t", s.pane, "-l", line)
	s.tmux(t, "send-keys", "-t", s.pane, "Enter")
	eventually(t, "the command ran", func() bool {
		b, _ := os.ReadFile(out)
		return strings.Contains(string(b), "done")
	})
	if got := s.tmux(t, "display-message", "-p", "-t", s.pane, "#{@sunstack_label}"); got != "probe" {
		b, _ := os.ReadFile(out)
		t.Fatalf("label %q; output:\n%s", got, b)
	}
}
