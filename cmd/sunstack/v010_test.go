package main

import (
	"strings"
	"testing"
)

// org allow and deny are the user's: refused from an agent session, and
// a grant names the host's key (§20.7).
func TestOrgAllow(t *testing.T) {
	hubHost, addr, hosts := newOrg(t, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	soon(t, "the hub pins laptop", func() bool {
		return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop")
	})
	agent := &orgHost{name: hubHost.name, home: hubHost.home, env: append(append([]string{}, hubHost.env...), "CLAUDECODE=1")}
	r := agent.run(t, "org", "allow", "laptop", "spawn")
	expect(t, r, 2, "allow from an agent session")
	requireContains(t, r.stderr, "user_only")

	// So is a pane sunstack recorded for a session, even without the
	// CLI's variables.
	s := privateTmux(t, t.TempDir(), hubHost.env)
	s.tmux(t, "set-option", "-p", "-t", s.pane, "@sunstack_launch", "0123456789abcdef")
	inPane := &orgHost{name: hubHost.name, home: hubHost.home, env: append(append([]string{}, hubHost.env...), "TMUX="+envValue(s.env, "TMUX"), "TMUX_PANE="+s.pane)}
	expect(t, inPane.run(t, "org", "allow", "laptop", "spawn"), 2, "allow from a session's pane")

	r = hubHost.run(t, "org", "allow", "laptop", "spawn", "peek")
	expect(t, r, 0, "allow")
	requireContains(t, r.out, "laptop", "may now peek, spawn", "secrets printed there", "starts Claude or Codex")
	requireContains(t, hubHost.run(t, "org", "allow").out, "laptop: peek, spawn")
	expect(t, hubHost.run(t, "org", "allow", "laptop", "shell"), 1, "unknown kind")
	expect(t, hubHost.run(t, "org", "allow", "nobody", "peek"), 1, "unknown host")
	expect(t, hubHost.run(t, "org", "deny", "laptop", "peek", "spawn"), 0, "deny")
	requireContains(t, hubHost.run(t, "org", "allow").out, "No host may")
}
