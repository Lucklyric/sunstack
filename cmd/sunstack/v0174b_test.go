package main

// §37 review: only the exact trust menu is ever answered, and the watch
// never waits on a CLI that is gone.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrustWatchUnsafeScreens(t *testing.T) {
	for _, mode := range []string{"unsafelabel", "unsafeinput", "unsafechange", "unsafewrap", "unsafenofooter", "unsafefooter", "unsafetitle", "unsafeafter", "unsafeafterclaude"} {
		t.Run(mode, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			expect(t, sh(t, p, s.env, "prompts", "--codex-hooks", "on"), 0, "prompts")
			s.tmux(t, "set-environment", "-g", "SUNSTACK_TEST_MODE", mode)
			tool := "codex"
			if mode == "unsafetitle" || mode == "unsafeafterclaude" {
				tool = "claude"
				expect(t, sh(t, p, s.env, "prompts", "--claude-folders", "on"), 0, "prompts")
			}
			r := sh(t, p, s.env, "spawn", "reviewer", "--tool", tool, "--place", "here")
			expect(t, r, 0, "spawn")
			files, _ := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "input-*.jsonl"))
			for _, f := range files {
				if b, _ := os.ReadFile(f); len(strings.TrimSpace(string(b))) > 0 {
					t.Errorf("Enter was pressed on a screen that is not the exact trust menu: %s", b)
				}
			}
			if strings.Contains(r.out, "answered") {
				t.Errorf("reported an answer: %s", r.out)
			}
		})
	}
}

func TestTrustWatchExitedCLI(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	expect(t, sh(t, p, s.env, "prompts", "--codex-hooks", "on"), 0, "prompts")
	s.tmux(t, "set-environment", "-g", "SUNSTACK_TEST_MODE", "exit")
	start := time.Now()
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--tool", "codex", "--place", "here"), 0, "spawn")
	if d := time.Since(start); d > 9*time.Second {
		t.Errorf("spawn of a CLI that exited took %s", d)
	}
}

// §37 review: a draft with dialog-like words does not stall spawn.
func TestTrustWatchDraftText(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.tmux(t, "set-environment", "-g", "SUNSTACK_TEST_MODE", "drafttext")
	start := time.Now()
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--tool", "codex", "--place", "here"), 0, "spawn")
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("spawn took %s with no trust screen", d)
	}
}
