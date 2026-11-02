package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestReview172eFailedScanSecondClaimNudge(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	expect(t, sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude", "--task", "one"), 0, "first claim")
	expect(t, sh(t, p, review172Identity(s, panes[1], sid), "as", "reviewer", "--tool", "claude", "--join", "--task", "two"), 0, "second claim")
	r := sh(t, p, s.env, "send", "reviewer", "--type", "question", "control")
	requireContains(t, r.out, "runs in panes")
	if len(submitted(t, s.env)) != 0 {
		t.Fatal("control nudged")
	}
	must(t, os.WriteFile(envValue(s.env, "SUNSTACK_CLAUDE_AGENTS"), []byte("invalid json"), 0600))
	r = sh(t, p, s.env, "send", "reviewer", "--type", "question", "failed scan")
	time.Sleep(150 * time.Millisecond)
	t.Logf("send=%d %q submitted=%q", r.code, r.out, submitted(t, s.env))
	if len(submitted(t, s.env)) > 0 {
		t.Error("second claim bypassed failed-scan guard")
	}
}

func TestReview172eStaleClaimNudgeAndKill(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	expect(t, sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude"), 0, "claim")
	s.tmux(t, "kill-pane", "-t", panes[1])
	s.tmux(t, "respawn-pane", "-k", "-t", panes[0], "/bin/sh")
	q := *s
	q.pane = panes[0]
	q.start(t, "claude", "idle")
	events := launches(t, s.env)
	entries[0]["pid"] = events[len(events)-1].PID
	entries[0]["sessionId"] = "00000000-0000-4000-8000-000000000099"
	claudeList(t, s.env, entries[0])
	r := sh(t, p, s.env, "send", "reviewer", "--type", "question", "to old session")
	time.Sleep(150 * time.Millisecond)
	t.Logf("nudge=%d %q submitted=%q", r.code, r.out, submitted(t, s.env))
	if len(submitted(t, s.env)) > 0 {
		t.Error("nudge reached replacement CLI")
	}
	r = sh(t, p, s.env, "kill", "reviewer", "--yes")
	t.Logf("kill=%d %q stderr=%q", r.code, r.out, r.stderr)
	if r.code == 0 && strings.Contains(r.out, "closed") {
		t.Error("kill closed replacement CLI")
	}
}
