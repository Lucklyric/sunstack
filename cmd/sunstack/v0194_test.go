package main

// v0.19.4: as says when the session's title needs a /rename; clean also
// closes spawned sessions that released their agent and drops claims whose
// pane is closed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// as prints a rename block only when the session's title differs.
func TestAsRenameStep(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	sid := "00000000-0000-4000-8000-000000000941"
	cenv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	want := wantTitle(t, p, "reviewer", "web")

	claudeTitle176(t, env, sid, titleLine176(sid, "old-name"))
	r := sh(t, p, cenv, "as", "reviewer", "--task", "web")
	expect(t, r, 0, "as")
	requireContains(t, r.out, "===== rename =====", `titled "old-name"`, "/rename "+want)
	token := field(tokenRe, r.out)

	claudeTitle176(t, env, sid, titleLine176(sid, "old-name"), titleLine176(sid, want))
	r = sh(t, p, cenv, "as", "reviewer", "--token", token)
	expect(t, r, 0, "resume")
	if strings.Contains(r.out, "/rename") {
		t.Errorf("a session with the right title got a rename step:\n%s", r.out)
	}
}

func TestCleanReleasedAndStale(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	spawn := func(id, task string) string {
		r := sh(t, p, s.env, "spawn", id, "--task", task, "--tool", "claude", "--place", "here")
		expect(t, r, 0, "spawn "+id)
		return field(paneRe, r.out)
	}
	done := spawn("reviewer", "done")
	gone := spawn("builder.bob", "gone")

	// The stand-in in the done pane, found by its terminal.
	tty := strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", done, "#{pane_tty}"), "/dev/")
	pid := 0
	for i := 0; i < 50 && pid == 0; i++ {
		for _, e := range launches(t, s.env) {
			out, _ := exec.Command("/bin/ps", "-o", "tty=", "-p", strconv.Itoa(e.PID)).Output()
			got := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/"), "tty")
			if got != "" && strings.TrimPrefix(tty, "tty") == got {
				pid = e.PID
			}
		}
		if pid == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if pid == 0 {
		t.Fatalf("no stand-in on %s", tty)
	}
	sid := "00000000-0000-4000-8000-000000000942"
	transcript := filepath.Join(envValue(s.env, "HOME"), ".claude", "projects", "fixture", sid+".jsonl")
	must(t, os.MkdirAll(filepath.Dir(transcript), 0o755))
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	s.env = append(s.env, `SUNSTACK_TEST_PROCESS_TTYS={"`+strconv.Itoa(pid)+`":"`+tty+`"}`)
	claudeList(t, s.env, map[string]any{"pid": pid, "sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})

	// The reviewer session finishes and releases its agent; builder.bob's
	// pane is closed under its claim.
	pr, err := core.FindProject(p)
	must(t, err)
	claims, err := pr.Claims("reviewer")
	must(t, err)
	if len(claims) != 1 {
		t.Fatalf("reviewer claims: %d", len(claims))
	}
	// The session that releases marks its spawned pane with its ID.
	renv := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	expect(t, sh(t, p, renv, "release", "reviewer", "--token", claims[0].Token), 0, "release")
	if got := s.tmux(t, "show-options", "-p", "-v", "-t", done, "@sunstack_released"); got != sid {
		t.Errorf("released mark: %q, want %q", got, sid)
	}
	s.tmux(t, "kill-pane", "-t", gone)
	// Claude Code's title starts with a status mark.
	s.tmux(t, "select-pane", "-t", done, "-T", "✳ reviewer_done")

	// Active a moment ago: not closed yet.
	r := sh(t, p, s.env, "clean")
	expect(t, r, 2, "clean lists the stale claim")
	requireContains(t, r.stderr, "builder.bob_gone (pane "+gone+" closed)")
	if strings.Contains(r.stderr, "released reviewer") {
		t.Errorf("a session active a moment ago is listed:\n%s", r.stderr)
	}

	old := time.Now().Add(-20 * time.Minute)
	must(t, os.Chtimes(transcript, old, old))
	r = sh(t, p, s.env, "next", "--all")
	requireContains(t, r.out, "1 session(s) that released their agent", "released reviewer", "1 claim(s) whose pane is closed", "sunstack clean")

	// Another session started in that pane is not the one that released.
	s.tmux(t, "set-option", "-p", "-t", done, "@sunstack_released", "00000000-0000-4000-8000-000000000999")
	r = sh(t, p, s.env, "clean")
	if strings.Contains(r.stderr, "released reviewer") {
		t.Errorf("a pane whose session is not the releasing one is listed:\n%s", r.stderr)
	}
	stale := field(planRe, r.stderr)
	s.tmux(t, "set-option", "-p", "-t", done, "@sunstack_released", sid)

	r = sh(t, p, s.env, "clean")
	expect(t, r, 2, "clean lists both")
	code := field(planRe, r.stderr)
	if code == "" || code == stale {
		t.Fatalf("plan codes: %q then %q", stale, code)
	}
	// A --plan from an older list closes nothing.
	r = sh(t, p, s.env, "clean", "--yes", "--plan", stale)
	expect(t, r, 4, "clean with a stale plan")
	requireContains(t, r.stderr, "changed", "--plan "+code)
	requireContains(t, r.stderr, "reviewer_done (released reviewer", "builder.bob_gone (pane "+gone+" closed)")
	if strings.Contains(r.stderr, "✳") || strings.Contains(r.stderr, "closed) (pane") {
		t.Errorf("list names carry a status mark or a second pane:\n%s", r.stderr)
	}
	// The command it prints names the team, so it works from any folder.
	requireContains(t, r.stderr, "sunstack clean --root "+core.ShellQuote(pr.Root)+" --yes --plan "+code)
	r = sh(t, t.TempDir(), s.env, "clean", "--root", pr.Root, "--yes", "--plan", code)
	expect(t, r, 0, "clean --root --yes --plan")
	requireContains(t, r.out, "closed ", "released reviewer", "released builder.bob_gone (its pane is closed)")
	panes := s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}")
	if strings.Contains(panes+"\n", done+"\n") || !strings.Contains(panes+"\n", s.pane+"\n") {
		t.Errorf("panes after clean: %s (closed %s, kept %s)", panes, done, s.pane)
	}
	if claims, _ := pr.Claims("builder.bob"); len(claims) != 0 {
		t.Errorf("the stale claim is still there: %d", len(claims))
	}
	r = sh(t, p, s.env, "clean", "--yes")
	expect(t, r, 0, "nothing left")
	requireContains(t, r.out, "nothing to close")
}

var planRe = regexp.MustCompile(`--plan ([0-9a-f]{8})`)
