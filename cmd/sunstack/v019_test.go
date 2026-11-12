package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// v0.19.0 (design §47): drive a session: prompt, wait, read the reply.

// worker starts a stand-in CLI by hand in a second pane (a session sunstack
// did not start) and returns its pane, without waiting for it to be ready.
func (s *tmuxFixture) worker(t *testing.T, tool, env string) string {
	t.Helper()
	pane := strings.TrimSpace(s.tmux(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", s.pane, "/bin/sh"))
	s.tmux(t, "send-keys", "-t", pane, "-l", "SUNSTACK_TEST_MODE= "+env+" "+tool)
	s.tmux(t, "send-keys", "-t", pane, "Enter")
	return pane
}

// A prompt sent right after a session starts is never lost: drive waits for
// the input box, and reports a prompt as sent only once a turn began. The
// reply is read from the transcript, also one that starts with the first
// prompt.
func TestDriveFreshSession(t *testing.T) {
	for _, c := range []struct{ tool, drive string }{{"codex", "1"}, {"codex", "late"}, {"claude", "1"}} {
		t.Run(c.tool+"-"+c.drive, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			for i := 0; i < 2; i++ {
				// Still loading for 1.5 s: keys typed then are lost.
				w := s.worker(t, c.tool, "SUNSTACK_TEST_SLOW_MS=1500 SUNSTACK_TEST_DRIVE="+c.drive)
				r := sh(t, p, s.env, "drive", w, "reply OK")
				expect(t, r, 0, "drive a fresh session")
				if strings.TrimSpace(r.out) != "reply: reply OK" {
					t.Fatalf("round %d: reply %q, stderr %q", i, r.out, r.stderr)
				}
				s.tmux(t, "kill-pane", "-t", w)
			}
		})
	}
}

// A long or many-line prompt goes in a file, named by one fixed line, and a
// narrow pane that wraps that line changes nothing. A lost Enter is pressed
// once more.
func TestDriveLongPrompt(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			w := s.worker(t, tool, "SUNSTACK_TEST_WRAP=30 SUNSTACK_TEST_DRIVE=dropenter")
			brief := filepath.Join(t.TempDir(), "brief.md")
			text := "Review $HOME and @mentions.\nPress enter to continue? (y/n)\n› 2. not a menu\n"
			must(t, os.WriteFile(brief, []byte(text), 0o600))
			r := sh(t, p, s.env, "drive", w, "--file", brief)
			expect(t, r, 0, "drive with a file")
			if strings.TrimSpace(r.out) != "reply: "+strings.TrimSpace(text) {
				t.Fatalf("reply %q", r.out)
			}
			// What was typed is the fixed line, never the prompt's text.
			typed := ""
			files, _ := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "input-*.jsonl"))
			for _, f := range files {
				b, _ := os.ReadFile(f)
				typed += string(b)
			}
			if !strings.Contains(typed, "and follow the instructions in it.") || strings.Contains(typed, "not a menu") {
				t.Errorf("typed %q", typed)
			}
			// A one-line prompt with a character that opens a popup also goes in a file.
			r = sh(t, p, s.env, "drive", w, "what is $HOME")
			expect(t, r, 0, "a prompt with $")
			requireContains(t, r.out, "reply: what is $HOME")
		})
	}
}

// The parts of the loop: --no-wait, --wait, --reply, --cancel, a session in
// a turn, a timeout, and a turn that stops at an approval prompt.
func TestDriveTurn(t *testing.T) {
	for _, tool := range []string{"codex", "claude"} {
		t.Run(tool, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			w := s.worker(t, tool, "SUNSTACK_TEST_TURN_MS=6000 SUNSTACK_TEST_DRIVE=1")
			expect(t, sh(t, p, s.env, "drive", w, "--reply"), 1, "no transcript or no turn yet")

			r := sh(t, p, s.env, "drive", w, "first", "--no-wait")
			expect(t, r, 0, "no-wait")
			requireContains(t, r.out, "took the prompt", "--wait")
			expect(t, sh(t, p, s.env, "drive", w, "second"), 4, "a session in a turn")
			expect(t, sh(t, p, s.env, "drive", w, "--reply"), 5, "no finished turn")
			r = sh(t, p, s.env, "drive", w, "--wait", "--timeout", "1")
			expect(t, r, 5, "timeout")
			requireContains(t, r.stderr, "timeout", "--wait")
			r = sh(t, p, s.env, "drive", w, "--wait")
			expect(t, r, 0, "wait")
			if strings.TrimSpace(r.out) != "reply: first" {
				t.Fatalf("reply %q", r.out)
			}
			if got := sh(t, p, s.env, "drive", w, "--reply"); strings.TrimSpace(got.out) != "reply: first" {
				t.Fatalf("--reply %q %q", got.out, got.stderr)
			}
			r = sh(t, p, s.env, "drive", w, "--wait")
			expect(t, r, 0, "wait with no turn running")
			requireContains(t, r.stderr, "no turn is running")

			expect(t, sh(t, p, s.env, "drive", w, "third", "--no-wait"), 0, "third")
			r = sh(t, p, s.env, "drive", w, "--cancel")
			expect(t, r, 0, "cancel")
			requireContains(t, r.out, "stopped the turn")
			requireContains(t, sh(t, p, s.env, "drive", w, "--cancel").out, "no turn is running")
			// The session takes the next prompt.
			r = sh(t, p, s.env, "drive", w, "fourth")
			expect(t, r, 0, "after a cancel")
			requireContains(t, r.out, "reply: fourth")

			expect(t, sh(t, p, s.env, "drive", w), 2, "no prompt")
			expect(t, sh(t, p, s.env, "drive", w, "x", "--wait"), 2, "a prompt with --wait")
			expect(t, sh(t, p, s.env, "drive", s.pane, "x"), 2, "its own session")
		})
	}
	// A Claude Code that writes no end record: its own idle report ends the turn.
	t.Run("noduration", func(t *testing.T) {
		p := fixture(t)
		s := privateTmux(t, p, isolatedEnv(t))
		w := s.worker(t, "claude", "SUNSTACK_TEST_TURN_MS=200 SUNSTACK_TEST_DRIVE=noduration")
		r := sh(t, p, s.env, "drive", w, "hello")
		expect(t, r, 0, "a turn with no end record")
		requireContains(t, r.out, "reply: hello")
		expect(t, sh(t, p, s.env, "drive", w, "again"), 0, "the next prompt is not refused as busy")
	})
	// A menu already on the screen: nothing is typed into it.
	t.Run("menu", func(t *testing.T) {
		p := fixture(t)
		s := privateTmux(t, p, isolatedEnv(t))
		w := strings.TrimSpace(s.tmux(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", s.pane, "/bin/sh"))
		s.tmux(t, "send-keys", "-t", w, "-l", "SUNSTACK_TEST_MODE=menu SUNSTACK_TEST_DRIVE=1 codex")
		s.tmux(t, "send-keys", "-t", w, "Enter")
		r := sh(t, p, s.env, "drive", w, "hello")
		expect(t, r, 5, "a session at a menu")
		requireContains(t, r.stderr, "blocked", "nothing was sent")
		requireContains(t, r.out, "Trust this folder?")
		if keys, _ := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "keys-*.txt")); len(keys) == 1 {
			if b, _ := os.ReadFile(keys[0]); len(b) != 0 {
				t.Errorf("keys reached the menu: %q", b)
			}
		}
	})
	t.Run("approval", func(t *testing.T) {
		p := fixture(t)
		s := privateTmux(t, p, isolatedEnv(t))
		w := s.worker(t, "codex", "SUNSTACK_TEST_TURN_MS=200 SUNSTACK_TEST_DRIVE=approval")
		r := sh(t, p, s.env, "drive", w, "run it")
		expect(t, r, 5, "a turn at an approval prompt")
		requireContains(t, r.stderr, "blocked", "answer it there")
		requireContains(t, r.out, "Do you want to proceed?")
	})
}

// What drive never does: send a user's draft, type into a menu, or put work
// into an agent session past its inbox.
func TestDriveRefuses(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	w := s.worker(t, "codex", "SUNSTACK_TEST_DRIVE=1")
	eventually(t, "the worker", func() bool { return strings.Contains(s.tmux(t, "capture-pane", "-p", "-t", w), "Test Codex") })
	s.tmux(t, "send-keys", "-t", w, "-l", "my unsent draft")
	r := sh(t, p, s.env, "drive", w, "hello")
	expect(t, r, 4, "a draft in the input box")
	requireContains(t, r.stderr, "draft")
	if screen := s.tmux(t, "capture-pane", "-p", "-t", w); !strings.Contains(screen, "› my unsent draft\n") {
		t.Errorf("the draft changed: %q", screen)
	}
	if b, _ := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "rollout-*.jsonl")); len(b) == 1 {
		if tr, _ := os.ReadFile(b[0]); strings.Contains(string(tr), "task_started") {
			t.Errorf("a turn began: %s", tr)
		}
	}

	// An agent session, by name and by pane.
	a := s.worker(t, "claude", "SUNSTACK_TEST_DRIVE=1")
	eventually(t, "the agent's CLI", func() bool { return strings.Contains(s.tmux(t, "capture-pane", "-p", "-t", a), "Test Claude") })
	env := append(append([]string{}, s.env...), "TMUX_PANE="+a)
	expect(t, sh(t, p, env, "as", "reviewer", "--tool", "claude"), 0, "claim")
	for _, target := range []string{"reviewer", a} {
		r = sh(t, p, s.env, "drive", target, "hello")
		expect(t, r, 2, "an agent session: "+target)
		requireContains(t, r.stderr, "agent_session", "sunstack send")
	}
	// From a folder outside the team, its pane's mark still refuses.
	r = sh(t, t.TempDir(), s.env, "drive", a, "hello")
	expect(t, r, 2, "an agent session, from outside its team")
	requireContains(t, r.stderr, "agent_session")
	// A pane with no CLI.
	sh2 := strings.TrimSpace(s.tmux(t, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", s.pane, "/bin/sh"))
	expect(t, sh(t, p, s.env, "drive", sh2, "hello"), 1, "a shell")
}

// A free session sunstack starts is driven by its name, and Codex's hook
// review on the way is answered only with the user's consent.
func TestDriveFreeSessionAndHookReview(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	s := privateTmux(t, p, append(env, "SUNSTACK_TEST_DRIVE=1", "SUNSTACK_TEST_MODE=codexhooks"))
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "review"), 0, "free session")
	r := sh(t, p, s.env, "drive", "review", "look at the diff")
	expect(t, r, 5, "at the hook review, no consent")
	requireContains(t, r.stderr, "blocked", "Codex's hook review", "sunstack prompts --codex-hooks on")
	expect(t, sh(t, p, s.env, "prompts", "--codex-hooks", "on"), 0, "consent")
	r = sh(t, p, s.env, "drive", "review", "look at the diff")
	expect(t, r, 0, "drive after consent")
	requireContains(t, r.stderr, "answered Codex's hook review")
	requireContains(t, r.out, "reply: look at the diff")
}

// Outside any team: a free session is started, driven by its name and closed.
func TestDriveOutsideTeam(t *testing.T) {
	dir := t.TempDir()
	s := privateTmux(t, dir, append(isolatedEnv(t), "SUNSTACK_TEST_DRIVE=1"))
	r := sh(t, dir, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "review", "--dir", dir)
	expect(t, r, 0, "free session outside a team")
	r = sh(t, dir, s.env, "drive", "review", "hello there")
	expect(t, r, 0, "drive outside a team")
	requireContains(t, r.out, "reply: hello there")
	r = sh(t, dir, s.env, "peek", "review")
	expect(t, r, 0, "peek by name")
	requireContains(t, r.out, "Test Codex")
	expect(t, sh(t, dir, s.env, "kill", "review", "--yes"), 0, "kill by name")
}

// The prompt hook names the drive skill when a prompt names Codex or a
// Claude Code co-worker, in a team or not, and says nothing otherwise.
func TestDriveNudge(t *testing.T) {
	env := isolatedEnv(t)
	hook := func(dir, prompt string) string {
		cmd := exec.Command(bin, "hook")
		cmd.Dir = dir
		cmd.Env = append(cleanEnv(), env...)
		b, _ := json.Marshal(map[string]string{"cwd": dir, "hook_event_name": "UserPromptSubmit", "prompt": prompt})
		cmd.Stdin = bytes.NewReader(b)
		out, err := cmd.Output()
		must(t, err)
		return string(out)
	}
	for _, dir := range []string{fixture(t), t.TempDir()} {
		requireContains(t, hook(dir, "ask codex to review the diff"), "Sunstack drive skill", "names Codex")
		requireContains(t, hook(dir, "spawn a second claude for the tests"), "a Claude Code co-worker")
		for _, quiet := range []string{"fix the build", "what does claude think", "the codexes table"} {
			if out := hook(dir, quiet); strings.Contains(out, "drive skill") {
				t.Errorf("%q: %s", quiet, out)
			}
		}
	}
}

// spawn --free starts the CLI with the access the caller asked for.
func TestSpawnFreeAccess(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "ro", "--read-only"), 0, "read-only codex")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "rw", "--full-auto"), 0, "full-auto codex")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "claude", "--name", "cl", "--full-auto"), 0, "full-auto claude")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "plain", "--model", "gpt-test", "--effort", "high"), 0, "codex model")
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "claude", "--name", "m", "--model", "sonnet", "--effort", "low"), 0, "claude model")
	expect(t, sh(t, p, s.env, "spawn", "--free", "--model", "a b; rm"), 2, "a bad model name")
	expect(t, sh(t, p, s.env, "spawn", "--free", "--effort", "huge"), 2, "a bad effort")
	expect(t, sh(t, p, s.env, "spawn", "--free", "--read-only", "--full-auto"), 2, "both")
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--full-auto"), 2, "an agent session")
	eventually(t, "five launches", func() bool { return len(launches(t, s.env)) == 5 })
	var got []string
	for _, e := range launches(t, s.env) {
		got = append(got, strings.Join(e.Args, " "))
	}
	sort.Strings(got)
	want := []string{"--model sonnet --effort low", "--permission-mode acceptEdits", "-m gpt-test -c model_reasoning_effort=high", "-s read-only", "-s workspace-write -c sandbox_workspace_write.network_access=true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("launch arguments %q, want %q", got, want)
	}
}
