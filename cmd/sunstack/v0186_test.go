package main

import (
	"strings"
	"testing"
)

// v0.18.6 (design §46): a session can be given a new name.

// A session sunstack did not start gets a label on its pane, and a label
// alone never makes it one that clean may close.
func TestNamePlainSession(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	opt := func(pane, name string) string {
		return s.tmux(t, "display-message", "-p", "-t", pane, "#{"+name+"}")
	}

	expect(t, sh(t, p, s.env, "name", "Fix Login"), 2, "invalid name")
	expect(t, sh(t, p, s.env, "name"), 2, "no name")
	r := sh(t, p, s.env, "name", "fix-login")
	expect(t, r, 0, "name")
	requireContains(t, r.out, "this session is now called fix-login")
	if got := opt(s.pane, "@sunstack_label") + " " + opt(s.pane, "pane_title") + " [" + opt(s.pane, "@sunstack_launch") + "]"; got != "fix-login fix-login []" {
		t.Errorf("marks %q", got)
	}
	r = sh(t, p, s.env, "name", "auth")
	expect(t, r, 0, "name again")
	requireContains(t, r.out, "now called auth (was fix-login)")
	requireContains(t, sh(t, p, s.env, "name", "auth").out, "already called auth")
	if out := sh(t, p, s.env, "clean").out; strings.Contains(out, "auth") {
		t.Errorf("clean lists a session sunstack did not start: %s", out)
	}

	// The name finds the session in a team too, and a free session cannot take it.
	expect(t, sh(t, p, s.env, "name", "auth", "login"), 0, "name by the session's name")
	if got := opt(s.pane, "@sunstack_label"); got != "login" {
		t.Errorf("label %q", got)
	}
	expect(t, sh(t, p, s.env, "name", "login", "auth"), 0, "and back")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "auth"), 4, "a free session with a taken name")

	// A free session, named from another pane; two sessions never share a name.
	r = sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "review")
	expect(t, r, 0, "free session")
	free := field(paneRe, r.out)
	expect(t, sh(t, p, s.env, "name", "review", "auth"), 4, "name taken")
	r = sh(t, p, s.env, "name", "review", "audit")
	expect(t, r, 0, "name a free session")
	requireContains(t, r.out, "the session in pane "+free+" is now called audit (was review)")
	if got := opt(free, "@sunstack_label") + " " + opt(free, "pane_title"); got != "audit audit" {
		t.Errorf("free marks %q", got)
	}
	expect(t, sh(t, p, s.env, "name", "audit"), 4, "the free session's name is taken")
	expect(t, sh(t, p, s.env, "kill", "audit", "--yes"), 0, "kill by the new name")
}

// An agent session's name is <id>_<task>: naming it changes the task label
// and keeps the claim.
func TestNameAgentSession(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	claim := sh(t, p, s.env, "as", "reviewer", "--tool", "claude")
	expect(t, claim, 0, "claim")
	tok := field(tokenRe, claim.out)

	expect(t, sh(t, p, s.env, "name", "a-label-too-long"), 2, "task label too long")
	r := sh(t, p, s.env, "name", "tests")
	expect(t, r, 0, "name")
	requireContains(t, r.out, "this session is now called reviewer_tests (was reviewer)")
	if got := s.tmux(t, "display-message", "-p", "-t", s.pane, "#{pane_title}"); got != "reviewer_tests" {
		t.Errorf("pane title %q", got)
	}
	requireContains(t, sh(t, p, s.env, "team").out, "reviewer_tests")
	// The token still works, and keeps the new label.
	r = sh(t, p, s.env, "as", "reviewer", "--token", tok)
	expect(t, r, 0, "resume")
	requireContains(t, sh(t, p, s.env, "team").out, "reviewer_tests")

	// Another session of the agent cannot take the same name, and nobody
	// names an agent session from outside.
	r = sh(t, p, s.env, "spawn", "reviewer", "--task", "docs", "--tool", "claude", "--place", "here")
	expect(t, r, 0, "second session")
	expect(t, sh(t, p, s.env, "name", "docs"), 4, "task taken")
	expect(t, sh(t, p, s.env, "name", "reviewer_docs", "other"), 2, "agent session named from outside")
}

// A Claude Code that takes --name starts already titled with the session's
// Sunstack name; Codex, and a Claude Code without the option, start as before.
func TestSpawnTitlesClaude(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	s := privateTmux(t, p, append(env, "SUNSTACK_TEST_CLI_NAME=1"))
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--task", "tests", "--tool", "claude", "--place", "here"), 0, "spawn")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "claude", "--name", "review"), 0, "free claude")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "cx"), 0, "free codex")
	eventually(t, "three launches", func() bool { return len(launches(t, s.env)) == 3 })
	var named []string
	for _, e := range launches(t, s.env) {
		for i, a := range e.Args {
			if a == "--name" && i+1 < len(e.Args) {
				named = append(named, e.Args[i+1])
			}
		}
	}
	if got := strings.Join(named, " "); len(named) != 2 || !strings.Contains(got, "reviewer_tests") || !strings.Contains(got, "review") {
		t.Errorf("--name on %v, want reviewer_tests and review only", named)
	}
}

// Codex's hook review is answered in any pane, also one sunstack did not
// start and long after it started, and only on the exact known screen.
func TestSweepHookReviews(t *testing.T) {
	for _, c := range []struct {
		mode  string
		known bool
	}{{"codexhooks", true}, {"unsafelabel", false}} {
		t.Run(c.mode, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			s.tmux(t, "send-keys", "-t", s.pane, "-l", "SUNSTACK_TEST_MODE="+c.mode+" codex")
			s.tmux(t, "send-keys", "-t", s.pane, "Enter")
			eventually(t, "the hook review", func() bool {
				return strings.Contains(s.tmux(t, "capture-pane", "-p", "-t", s.pane), "enter confirm")
			})
			r := sh(t, p, s.env, "prompts", "sweep")
			expect(t, r, 0, "sweep before consent")
			if c.known {
				requireContains(t, r.out, "pane "+s.pane, "waits at Codex's hook review", "sunstack prompts --codex-hooks on")
			} else {
				requireContains(t, r.out, "not the known one")
			}
			expect(t, sh(t, p, s.env, "prompts", "--codex-hooks", "on"), 0, "consent")
			r = sh(t, p, s.env, "prompts", "sweep")
			expect(t, r, 0, "sweep")
			got := choices174(t, s.env)
			if !c.known {
				requireContains(t, r.out, "not the known one")
				if len(got) != 0 {
					t.Errorf("answered an unknown screen: %v", got)
				}
				return
			}
			requireContains(t, r.out, "answered Codex's hook review: Trust all and continue")
			eventually(t, "the answer", func() bool { return len(choices174(t, s.env)) == 1 })
			if got = choices174(t, s.env); got[0] != "Trust all and continue" {
				t.Errorf("choices %v", got)
			}
			requireContains(t, sh(t, p, s.env, "prompts", "sweep").out, "no Codex session waits")
		})
	}
	// A pane that only shows the words is left alone.
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	expect(t, sh(t, p, s.env, "prompts", "--codex-hooks", "on"), 0, "consent")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "printf 'Hooks need review\\n'; exec cat")
	s.tmux(t, "send-keys", "-t", s.pane, "Enter")
	eventually(t, "the words", func() bool {
		return strings.Contains(s.tmux(t, "capture-pane", "-p", "-t", s.pane), "\nHooks need review")
	})
	requireContains(t, sh(t, p, s.env, "prompts", "sweep").out, "no Codex session waits")
}
