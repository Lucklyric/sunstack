package main

// v0.12.6: plain next stays local. Outside a team it lists only what waits
// on this session; the org-wide list is sunstack org --attention.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextLocalToSession(t *testing.T) {
	p := t.TempDir()
	env := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home"), "SUNSTACK_NO_UPDATE_CHECK=1"}
	expect(t, sh(t, p, env, "init"), 0, "init")
	expect(t, sh(t, p, env, "hire", "builder", "alice"), 0, "hire")
	expect(t, sh(t, p, env, "halt", "rotating keys"), 0, "halt")
	free := t.TempDir()
	const sid = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"
	agents := filepath.Join(t.TempDir(), "agents.json")
	must(t, os.WriteFile(agents, []byte(`[{"pid":0,"sessionId":"`+sid+`","cwd":"`+filepath.ToSlash(free)+`","kind":"interactive","name":"helper","status":"idle"}]`), 0o644))
	env = append(env, "SUNSTACK_CLAUDE_AGENTS="+agents)
	me := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)

	// Nothing waits on this session yet: the other team's halt is only counted.
	r := sh(t, free, me, "next")
	expect(t, r, 0, "next outside a team")
	if strings.Contains(r.out, "rotating keys") || !strings.Contains(r.out, "1 more item(s)") ||
		!strings.Contains(r.out, "sunstack org --attention") {
		t.Errorf("next outside a team should list only this session's items:\n%s", r.out)
	}

	// A task this session sent, and a question for it, are its own items.
	r = sh(t, free, me, "send", "--root", p, "builder.alice", "Goal: export the report\nScope: the report only\nDone when: a CSV exists\nVerify: open it\nReport: the path", "--type", "task")
	expect(t, r, 0, "task from the free session")
	task := field(msgRe, r.out)
	tok := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	r = sh(t, p, env, "send", sid, "which format?", "--type", "question", "--from", "builder.alice", "--token", tok)
	expect(t, r, 0, "question to the session")
	q := field(msgRe, r.out)
	r = sh(t, free, me, "next")
	requireContains(t, r.out, task, "builder.alice", q, "sunstack check --session", "sunstack org --attention")
	if strings.Contains(r.out, "rotating keys") {
		t.Errorf("another team's item in the session's next:\n%s", r.out)
	}

	// Another session sees neither.
	other := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=1a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d")
	if r := sh(t, free, other, "next"); strings.Contains(r.out, task) || strings.Contains(r.out, q) {
		t.Errorf("another session's items leaked:\n%s", r.out)
	}

	// Once alice acks the task it is no longer waiting.
	expect(t, sh(t, p, env, "take", "builder.alice", task, "--token", tok), 0, "take")
	expect(t, sh(t, p, env, "ack", "builder.alice", task, "--token", tok), 0, "ack")
	if r := sh(t, free, me, "next"); strings.Contains(r.out, task) {
		t.Errorf("acked task still listed:\n%s", r.out)
	}

	// The org-wide list and a team's own list are unchanged.
	requireContains(t, sh(t, free, me, "org", "--attention").out, "rotating keys")
	requireContains(t, sh(t, p, env, "next").out, "rotating keys")
}
