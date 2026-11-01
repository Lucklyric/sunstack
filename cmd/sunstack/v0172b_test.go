package main

// v0.17.2 (§34), batch 2: org leave, remote update, remote peek, issues
// output, peek guidance.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// §34.7: after org leave the connector stops: no new mail, no snapshots.
func TestLeaveStopsConnector(t *testing.T) {
	hubHost, addr, hosts := newOrg(t, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	project := laptop.team(t, "shop")
	laptop.connector(t)
	snap := filepath.Join(hubHost.file("hub"), orgOf(t, hubHost).OrgID, "snap", hostID(t, laptop)+".json")
	soon(t, "hub pins laptop", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	soon(t, "initial snapshot", func() bool { _, err := os.Stat(snap); return err == nil })
	expect(t, laptop.run(t, "org", "leave"), 0, "leave")
	expect(t, hubHost.run(t, "send", "laptop:shop/builder.alice", "AFTER-LEAVE-MARKER", "--type", "fyi"), 0, "send to the old member")
	laptop.team(t, "made-after-leave")
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		for _, path := range inboxFiles(t, project, "builder.alice") {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), "AFTER-LEAVE-MARKER") {
				t.Fatal("mail arrived after org leave")
			}
		}
		if b, _ := os.ReadFile(snap); strings.Contains(string(b), "made-after-leave") {
			t.Fatal("a snapshot went to the old hub after org leave")
		}
		if _, err := os.Stat(laptop.file("remote/roster.json")); err == nil {
			t.Fatal("the roster came back after org leave")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// §34.9: update under the service updates the host only.
func TestServiceUpdateNoMigrate(t *testing.T) {
	p := fixture(t)
	expect(t, sh(t, p, nil, "init"), 0, "init")
	team := filepath.Join(p, "sunstack", "TEAM")
	must(t, os.Remove(team))
	expect(t, sh(t, p, []string{"SUNSTACK_SERVICE=1", "SUNSTACK_TEST_MODE=exit"}, "update", "--skip-binary", "--claude"), 0, "service update")
	if _, err := os.Stat(team); err == nil {
		t.Error("update under the service migrated a team")
	}
}

// §34.10: remote peek refuses a pane whose CLI is not the claimed session.
func TestPeekReplacedSession(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "claude", "idle")
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000031")
	expect(t, sh(t, p, claim, "as", "builder.alice"), 0, "claim")
	before := len(launches(t, s.env))
	time.Sleep(1100 * time.Millisecond) // a later session, in a later second than the claim
	s.tmux(t, "respawn-pane", "-k", "-t", s.pane, "/bin/sh")
	s.start(t, "claude", "idle")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "UNRELATED-NEW-SESSION-SECRET")
	if len(launches(t, s.env)) <= before {
		t.Fatal("the replacement CLI did not start")
	}
	soon(t, "laptop pinned", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "grant peek")
	r := laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "10")
	if strings.Contains(r.out, "UNRELATED-NEW-SESSION-SECRET") {
		t.Errorf("remote peek showed a replacement session under the old claim: %s", r.out)
	}
}

// §34.13: issues shows stored control characters as visible text.
func TestIssuesControls(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	expect(t, sh(t, p, env, "issue", "diag \x1b]52;c;cmV2aWV3\x07\x1b[2J"), 0, "note")
	if r := sh(t, p, env, "issues", "--code", "note"); strings.ContainsAny(r.out, "\x1b\x07") {
		t.Error("issues printed raw control characters")
	}
}

// §34.14: the progress guidance keeps peek on this host.
func TestPeekGuidance(t *testing.T) {
	for _, path := range []string{"../../internal/assets/project/PROTOCOL.md", "../../plugins/sunstack/skills/org/SKILL.md"} {
		b, err := os.ReadFile(path)
		must(t, err)
		if s := strings.Join(strings.Fields(string(b)), " "); !strings.Contains(s, "`peek` on this host") {
			t.Errorf("%s does not limit progress peeks to this host", path)
		}
	}
}
