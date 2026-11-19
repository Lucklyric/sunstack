package main

// v0.19.6: clean --yes --plan closes only the list the user saw, a free
// session that took an agent is checked as that agent, kill never widens a
// named --root to the whole host, and the side-by-side split leaves room for
// the divider.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// A pane exactly twice the minimum width has no room for the divider.
func TestSpawnSplitNeedsDivider(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.tmux(t, "resize-window", "-t", s.pane, "-x", "160", "-y", "30")
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "narrow", "--tool", "claude", "--place", "here")
	expect(t, r, 0, "spawn beside a 160-column pane")
	requireContains(t, r.out, "in a new tmux window")
	s.tmux(t, "resize-window", "-t", s.pane, "-x", "161", "-y", "30")
	r = sh(t, p, s.env, "spawn", "reviewer", "--task", "wide", "--tool", "claude", "--place", "here")
	expect(t, r, 0, "spawn beside a 161-column pane")
	requireContains(t, r.out, "in a pane beside this one")
}

// kill with a --root that is no team fails, even when a free session of
// that name runs elsewhere on the host.
func TestKillBadRootStaysInScope(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	r := sh(t, p, s.env, "spawn", "--free", "--tool", "claude", "--name", "scratch", "--place", "here")
	expect(t, r, 0, "spawn scratch")
	pane := field(paneRe, r.out)
	r = sh(t, p, s.env, "kill", "scratch", "--yes", "--root", filepath.Join(t.TempDir(), "nowhere"))
	if r.code == 0 {
		t.Fatalf("kill with a bad --root closed something:\n%s%s", r.out, r.stderr)
	}
	if panes := s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}"); !strings.Contains(panes+"\n", pane+"\n") {
		t.Errorf("the free session's pane %s was closed: %s", pane, panes)
	}
}

// A free session holding an agent claim is not closed as a free session;
// idle long enough, it is listed as that agent's session, and a taken
// message holds it back.
func TestCleanFreePaneWithClaim(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	r := sh(t, p, s.env, "spawn", "--free", "--tool", "claude", "--name", "scratch", "--place", "here")
	expect(t, r, 0, "spawn scratch")
	pane := field(paneRe, r.out)
	tty := strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", pane, "#{pane_tty}"), "/dev/")
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
	sid := "00000000-0000-4000-8000-000000000961"
	transcript := filepath.Join(envValue(s.env, "HOME"), ".claude", "projects", "fixture", sid+".jsonl")
	must(t, os.MkdirAll(filepath.Dir(transcript), 0o755))
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	old := time.Now().Add(-48 * time.Hour)
	must(t, os.Chtimes(transcript, old, old))
	s.env = append(s.env, `SUNSTACK_TEST_PROCESS_TTYS={"`+strconv.Itoa(pid)+`":"`+tty+`"}`)
	claudeList(t, s.env, map[string]any{"pid": pid, "sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})

	r = sh(t, p, s.env, "clean")
	expect(t, r, 2, "clean lists the idle free session")
	requireContains(t, r.stderr, "scratch (free)")

	// The free session takes the reviewer agent.
	cenv := append(append([]string{}, s.env...), "TMUX_PANE="+pane, "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	r = sh(t, p, cenv, "as", "reviewer", "--task", "held")
	expect(t, r, 0, "as reviewer")
	r = sh(t, p, s.env, "clean")
	if strings.Contains(r.stderr+r.out, "scratch (free)") {
		t.Fatalf("a free session holding a claim is closed as a free session:\n%s%s", r.out, r.stderr)
	}

	// Its claim is two days old too: now it is the agent's idle session.
	pr, err := core.FindProject(p)
	must(t, err)
	claims, err := pr.Claims("reviewer")
	must(t, err)
	if len(claims) != 1 {
		t.Fatalf("reviewer claims: %d", len(claims))
	}
	path := filepath.Join(pr.Dir, "_local", "live", "reviewer", claims[0].Token+".json")
	b, err := os.ReadFile(path)
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(b, &m))
	m["claimed"] = old.UTC().Format("2006-01-02T15:04:05Z")
	m["last_contact"] = m["claimed"]
	b, _ = json.Marshal(m)
	must(t, os.WriteFile(path, b, 0o600))
	r = sh(t, p, s.env, "clean")
	expect(t, r, 2, "clean lists the agent session")
	requireContains(t, r.stderr, "reviewer_held (pane "+pane)

	// A taken message holds it back.
	r = sh(t, p, s.env, "send", "reviewer_held", "hold", "--type", "fyi", "--no-nudge")
	expect(t, r, 0, "send")
	id := field(msgRe, r.out)
	expect(t, sh(t, p, cenv, "take", "reviewer", id, "--token", claims[0].Token), 0, "take")
	m["last_contact"] = m["claimed"]
	b, _ = json.Marshal(m)
	must(t, os.WriteFile(path, b, 0o600))
	r = sh(t, p, s.env, "clean")
	if strings.Contains(r.stderr+r.out, "reviewer_held") || strings.Contains(r.stderr+r.out, "scratch (free)") {
		t.Errorf("a session holding a taken message is listed:\n%s%s", r.out, r.stderr)
	}
}

// A plan code names its team: the same list in another team is another plan.
func TestPlanCodeNamesTeam(t *testing.T) {
	if idlePlanCode("/a", nil) == idlePlanCode("/b", nil) {
		t.Errorf("two teams share a plan code")
	}
}
