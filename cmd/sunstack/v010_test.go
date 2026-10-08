package main

import (
	"os"
	"path/filepath"
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

// Remote peek (§20.5): granted on the host that has the session, asked by
// the user, answered sealed; pane text never sits in a file in the clear.
func TestRemotePeek(t *testing.T) {
	// The hub-host's sessions run on a private tmux server, which its
	// connector knows as the default one.
	events := t.TempDir() // the stand-in CLI records what it was given here
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "claude", "idle")
	sid := "00000000-0000-4000-8000-000000000031"
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	expect(t, sh(t, p, claim, "as", "builder.alice"), 0, "claim in the pane")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "PEEK-MARKER-7")
	soon(t, "both hosts pinned", func() bool {
		return strings.Contains(laptop.run(t, "org", "keys").out, "hub-host") && strings.Contains(hubHost.run(t, "org", "keys").out, "laptop")
	})

	// Not granted yet: the hub-host answers with a sealed refusal.
	r := laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "5")
	expect(t, r, 1, "peek without a grant")
	requireContains(t, r.stderr, "not allowed", "org allow laptop peek")

	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "allow peek")
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "5")
	expect(t, r, 0, "peek")
	requireContains(t, r.out, "builder.alice on hub-host", "PEEK-MARKER-7")

	// Refused: pane IDs across hosts, too many lines, an agent session.
	expect(t, laptop.run(t, "peek", "hub-host:%1"), 1, "pane ID across hosts")
	expect(t, laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "80"), 2, "more than 50 lines")
	agent := &orgHost{name: laptop.name, home: laptop.home, env: append(append([]string{}, laptop.env...), "CODEX_THREAD_ID="+sid)}
	expect(t, agent.run(t, "peek", "hub-host:shop/builder.alice"), 2, "peek from an agent session")

	// No file on either host, or at the hub, holds the pane text.
	for _, h := range []*orgHost{hubHost, laptop} {
		filepath.WalkDir(filepath.Join(h.home, ".sunstack"), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(path); strings.Contains(string(b), "PEEK-MARKER-7") {
					t.Errorf("%s holds the pane text", path)
				}
			}
			return nil
		})
	}
}
