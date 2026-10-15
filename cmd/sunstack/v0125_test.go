package main

// v0.12.5: fixes for the issues left from the code review.

import (
	"strings"
	"testing"
)

// A free session's request to another host carries its session ID, so the
// reply can come back to it as <host>:<session ID>.
func TestRemoteFreeSessionReply(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	a.join(t, hub, addr)
	b.join(t, hub, addr)
	pb := b.team(t, "shop")
	expect(t, a.run(t, "org", "--refresh"), 0, "refresh roster")

	const sid = "0d5e1f2a-3b4c-4d5e-8f60-718293a4b5c6"
	r := sh(t, a.home, append(a.env, "CLAUDE_CODE_SESSION_ID="+sid), "send", "server:shop/builder.alice", "ready?", "--type", "question")
	expect(t, r, 0, "send from a free session")
	stop, _ := b.connector(t)
	defer stop()
	soon(t, "delivery", func() bool { return len(inboxFiles(t, pb, "builder.alice")) == 1 })
	got := readText(t, inboxFiles(t, pb, "builder.alice")[0])
	requireContains(t, got, "from: laptop:user", "from_session: laptop:"+sid)
	if strings.Count(got, "from_session:") != 1 {
		t.Errorf("message:\n%s", got)
	}
}
