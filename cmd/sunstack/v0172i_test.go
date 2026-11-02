package main

import (
	"github.com/Lucklyric/sunstack/internal/core"
	"strings"
	"testing"
)

func TestReview172eDuplicateClaimPeek(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	expect(t, sh(t, p, s.env, "init"), 0, "init")
	sid := entries[0]["sessionId"].(string)
	expect(t, sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude"), 0, "claim")
	for _, ev := range s.env {
		k, v, ok := strings.Cut(ev, "=")
		if ok {
			t.Setenv(k, v)
		}
	}
	pp, e := core.FindProject(p)
	must(t, e)
	team, ok := pp.Team()
	if !ok {
		t.Fatal("no team")
	}
	s.tmux(t, "send-keys", "-t", panes[0], "-l", "B5-DUPLICATE-PEEK-MARKER")
	all, notes := core.ScanSessions()
	t.Logf("duplicates=%v notes=%v", core.DuplicateSessions(all), notes)
	if len(core.DuplicateSessions(all)[sid]) != 2 {
		t.Fatal("duplicate fixture not detected")
	}
	label, body, e := core.PeekFor(team.ID+"/reviewer", 10)
	t.Logf("PeekFor label=%s err=%v text=%q", label, e, body)
	if e == nil && strings.Contains(body, "B5-DUPLICATE-PEEK-MARKER") {
		t.Error("agent-addressed remote capture ignored duplicate session ID")
	}
}
