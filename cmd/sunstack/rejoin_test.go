package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A host keeps its keys across org leave, so it joins again; another key
// under its host ID would take over its name and mail, so that is refused.
func TestRejoinNeedsSameKeys(t *testing.T) {
	hubHost, addr, hosts := newOrgEnv(t, nil, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	expect(t, laptop.run(t, "org", "leave"), 0, "leave")
	laptop.join(t, hubHost, addr)

	expect(t, laptop.run(t, "org", "leave"), 0, "leave again")
	must(t, os.Remove(laptop.file("keys.json")))
	r := hubHost.run(t, "hub", "invite")
	expect(t, r, 0, "invite")
	r = laptop.run(t, "org", "join", addr, "--code", field(codeRe, r.out))
	expect(t, r, 1, "join with new keys")
	if !strings.Contains(r.stderr, "other keys") {
		t.Errorf("join with new keys: %s", r.stderr)
	}
}

// Org membership and the hub's roster are the user's: an agent session is
// refused, as for org allow.
func TestOrgAdminIsUserOnly(t *testing.T) {
	hubHost, _, _ := newOrgEnv(t, nil)
	agent := &orgHost{name: hubHost.name, home: hubHost.home, env: append(append([]string{}, hubHost.env...), "CLAUDECODE=1")}
	for _, args := range [][]string{
		{"hub", "init", "other"}, {"hub", "invite"}, {"hub", "revoke", "laptop"},
		{"org", "join", "127.0.0.1", "--code", "x"}, {"org", "leave"}, {"org", "trust", "laptop"},
	} {
		r := agent.run(t, args...)
		expect(t, r, 2, strings.Join(args, " ")+" from an agent session")
		if !strings.Contains(r.stderr, "for the user") {
			t.Errorf("%v: %s", args, r.stderr)
		}
	}
}

// Inside tmux, spawn and spawn --free open beside the caller's pane, or in
// a new window of the caller's session when that pane is too small to split;
// the team's home only with --place team.
func TestSpawnBesideCaller(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	win := s.tmux(t, "display-message", "-p", "-t", s.pane, "#{window_id}")
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "one", "--tool", "claude")
	expect(t, r, 0, "spawn")
	requireContains(t, r.out, "a pane beside this one")
	if got := s.tmux(t, "display-message", "-p", "-t", field(paneRe, r.out), "#{window_id}"); got != win {
		t.Errorf("spawned into window %s, the caller is in %s", got, win)
	}
	r = sh(t, p, s.env, "spawn", "--free", "--tool", "codex", "--name", "beside")
	expect(t, r, 0, "spawn --free")
	requireContains(t, r.out, "a pane beside this one")
	if got := s.tmux(t, "display-message", "-p", "-t", field(paneRe, r.out), "#{window_id}"); got != win {
		t.Errorf("free session in window %s, the caller is in %s", got, win)
	}
	if out := s.tmux(t, "list-sessions", "-F", "#{session_name}"); strings.Contains(out, "ss-") {
		t.Errorf("a home was made: %s", out)
	}

	// Too small to split: a new window after the caller's.
	for i := 0; i < 40; i++ {
		if exec.Command("tmux", "-S", s.socket, "split-window", "-d", "-v", "-t", s.pane).Run() != nil {
			break
		}
	}
	r = sh(t, p, s.env, "spawn", "reviewer", "--task", "two", "--tool", "claude")
	expect(t, r, 0, "spawn into a full window")
	requireContains(t, r.out, "a new tmux window")
	if got := s.tmux(t, "display-message", "-p", "-t", field(paneRe, r.out), "#{session_name}"); got != s.tmux(t, "display-message", "-p", "-t", s.pane, "#{session_name}") {
		t.Errorf("the new window is in session %s", got)
	}
}
