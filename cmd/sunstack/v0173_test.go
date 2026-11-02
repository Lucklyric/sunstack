package main

// v0.17.3 (§36): the prompt hook names the session.

import (
	"strings"
	"testing"
)

func TestPromptHookNamesSession(t *testing.T) {
	p, env := team086(t)
	sid := "00000000-0000-4000-8000-000000000361"
	cenv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	team := strings.TrimSpace(readText(t, p+"/sunstack/TEAM"))
	_ = team
	ctx := func(dir string) string {
		out := runHook(t, env, "UserPromptSubmit", "", sid, dir)
		if strings.TrimSpace(out) == "" {
			return ""
		}
		return hookContextText(t, out, "UserPromptSubmit")
	}
	// No claim yet, in a team folder.
	requireContains(t, ctx(p), "[sunstack] this session holds no agent in ")
	// Outside any team: nothing.
	if got := ctx(t.TempDir()); got != "" {
		t.Errorf("outside a team the hook said: %q", got)
	}
	// With a claim: its name; waiting messages still follow.
	expect(t, sh(t, p, cenv, "as", "builder.alice", "--task", "web"), 0, "claim")
	expect(t, sh(t, p, env, "send", "builder.alice", "hello", "--type", "question"), 0, "message")
	got := ctx(p)
	requireContains(t, got, "[sunstack] this session: builder.alice_web in ", "new message(s) waiting")
	if strings.Index(got, "this session") > strings.Index(got, "waiting") {
		t.Errorf("the name should come first: %q", got)
	}
}
