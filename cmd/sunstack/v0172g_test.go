package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestReview172dSpawnedPeekReplacement(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	soon(t, "both hosts pinned", func() bool {
		return strings.Contains(laptop.run(t, "org", "keys").out, "hub-host") && strings.Contains(hubHost.run(t, "org", "keys").out, "laptop")
	})
	expect(t, hubHost.run(t, "org", "allow", "laptop", "spawn", "peek"), 0, "allow")
	r := laptop.run(t, "spawn", "hub-host:shop/builder.alice", "--task", "remote", "--tool", "claude", "--yes")
	expect(t, r, 0, "remote spawn")
	pane := field(paneRe, r.out)
	if pane == "" {
		t.Fatalf("no pane: %s", r.out)
	}
	original := review172cEditClaim(t, p, func(v map[string]any) {})
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice_remote", "--lines", "10")
	expect(t, r, 0, "peek newly spawned original")
	t.Logf("spawned claim=%v original peek=%d", original, r.code)
	before := launches(t, []string{"SUNSTACK_TEST_EVENTS=" + events})
	s.tmux(t, "respawn-pane", "-k", "-t", pane, "/bin/sh")
	q := *s
	q.pane = pane
	q.start(t, "claude", "idle")
	q.tmux(t, "send-keys", "-t", pane, "-l", "B4-SPAWNED-REPLACEMENT-SECRET")
	after := launches(t, []string{"SUNSTACK_TEST_EVENTS=" + events})
	mark := s.tmux(t, "show-options", "-p", "-v", "-t", pane, "@sunstack")
	time.Sleep(2100 * time.Millisecond)
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice_remote", "--lines", "10")
	t.Logf("launches %v -> %v mark=%q replacement peek=%d out=%q stderr=%q", before, after, mark, r.code, r.out, r.stderr)
	if r.code == 0 && strings.Contains(r.out, "B4-SPAWNED-REPLACEMENT-SECRET") {
		t.Error("spawn pane mark authorized an unrelated replacement")
	}
}

func TestReview172dDuplicateNudgeFailedScan(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	r := sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "claim")
	r = sh(t, p, s.env, "send", "reviewer", "--type", "question", "control before scan failure")
	expect(t, r, 0, "duplicate blocked control")
	requireContains(t, r.out, "runs in panes")
	if len(submitted(t, s.env)) != 0 {
		t.Fatal("control already nudged")
	}
	must(t, os.WriteFile(envValue(s.env, "SUNSTACK_CLAUDE_AGENTS"), []byte("invalid json"), 0600))
	r = sh(t, p, s.env, "send", "reviewer", "--type", "question", "after scan failure")
	time.Sleep(150 * time.Millisecond)
	t.Logf("failed scan send=%d out=%q submitted=%q", r.code, r.out, submitted(t, s.env))
	if len(submitted(t, s.env)) != 0 {
		t.Error("failed scan allowed a nudge into an ambiguous conversation")
	}
}
