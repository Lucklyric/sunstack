package main

// §30: one nudge at a time, and the prompt hook's new and taken lines.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func eventsLog(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(p, "sunstack", "_local", "log", "events.log"))
	return string(b)
}

// A second message to a session with a nudge waiting is not typed; check,
// take and ack clear the wait, and a marker over 5 minutes old no longer holds.
func TestNudgeOncePerSession(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	tok := field(tokenRe, sh(t, p, s.env, "as", "reviewer", "--tool", "claude").out)
	nudges := 0
	sendNudged := func(text string) string {
		t.Helper()
		r := sh(t, p, s.env, "send", "reviewer", text)
		expect(t, r, 0, "send "+text)
		requireContains(t, r.out, "nudged session reviewer")
		nudges++
		eventually(t, "nudge submitted", func() bool { return len(submitted(t, s.env)) == nudges })
		return field(msgRe, r.out)
	}
	sendHeld := func(text string) string {
		t.Helper()
		r := sh(t, p, s.env, "send", "reviewer", text)
		expect(t, r, 0, "send "+text)
		requireContains(t, r.out, "not nudged (a nudge is already waiting)")
		return field(msgRe, r.out)
	}

	sendNudged("one")
	held := sendHeld("two")
	if !strings.Contains(eventsLog(t, p), "nudge-held") || !strings.Contains(eventsLog(t, p), held) {
		t.Errorf("no nudge-held event:\n%s", eventsLog(t, p))
	}
	time.Sleep(300 * time.Millisecond)
	if got := len(submitted(t, s.env)); got != 1 {
		t.Fatalf("held message was typed: %d prompts", got)
	}

	expect(t, sh(t, p, s.env, "check", "reviewer", "--token", tok), 0, "check")
	m := sendNudged("three")
	sendHeld("four")
	expect(t, sh(t, p, s.env, "take", "reviewer", m, "--token", tok), 0, "take")
	m = sendNudged("five")
	expect(t, sh(t, p, s.env, "ack", "reviewer", m, "--token", tok), 0, "ack")
	sendNudged("six")

	// An old marker no longer holds.
	markers, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "nudged", "reviewer", "*"))
	if len(markers) != 1 {
		t.Fatalf("markers: %v", markers)
	}
	must(t, os.WriteFile(markers[0], []byte(time.Now().Add(-6*time.Minute).UTC().Format(time.RFC3339)+"\n"), 0o644))
	sendNudged("seven")
}

// With two sessions of one agent, a fresh marker on either holds a message
// for the agent; a message for the other session is not held by it.
func TestNudgeHeldAcrossSessions(t *testing.T) {
	p := fixture(t)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--task", "api").out)
	field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--join", "--task", "docs").out)
	dir := filepath.Join(p, "sunstack", "_local", "nudged", "builder.alice")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ta), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644))

	r := sh(t, p, nil, "send", "builder.alice", "for the agent")
	expect(t, r, 0, "send to agent")
	requireContains(t, r.out, "not nudged (a nudge is already waiting)")
	requireContains(t, eventsLog(t, p), "nudge-held")

	r = sh(t, p, nil, "send", "builder.alice_docs", "for docs")
	expect(t, r, 0, "send to the other session")
	if strings.Contains(r.out, "already waiting") {
		t.Errorf("held by another session's marker: %s", r.out)
	}
	r = sh(t, p, nil, "send", "builder.alice_api", "for api")
	requireContains(t, r.out, "not nudged (a nudge is already waiting)")
}

// The host inbox marker: held while fresh, cleared by check --session.
func TestHostNudgeOnce(t *testing.T) {
	p := t.TempDir()
	sid := "00000000-0000-4000-8000-000000000001"
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	claudeList(t, s.env, privateSession(t, s, sid, p))
	r := sh(t, p, s.env, "send", sid, "first")
	requireContains(t, r.out, "nudged session claude "+s.pane)
	eventually(t, "host nudge", func() bool { return len(submitted(t, s.env)) == 1 })
	r = sh(t, p, s.env, "send", sid, "second")
	expect(t, r, 0, "second send")
	requireContains(t, r.out, "not nudged (a nudge is already waiting)")
	me := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	expect(t, sh(t, p, me, "check", "--session"), 0, "check --session")
	r = sh(t, p, s.env, "send", sid, "third")
	requireContains(t, r.out, "nudged session claude "+s.pane)
	eventually(t, "host nudge after check", func() bool { return len(submitted(t, s.env)) == 2 })
}

// The prompt hook names new and taken messages apart, with the age of an old
// taken one.
func TestHookNewAndTaken(t *testing.T) {
	p := fixture(t)
	env := []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=sess-a"}
	home := isolatedEnv(t)
	env = append(env, home...)
	tok := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	m1 := field(msgRe, sh(t, p, env, "send", "builder.alice", "first").out)
	m2 := field(msgRe, sh(t, p, env, "send", "builder.alice", "second").out)
	expect(t, sh(t, p, env, "take", "builder.alice", m1, "--token", tok), 0, "take")

	out := runHook(t, home, "UserPromptSubmit", "", "sess-a", p)
	requireContains(t, out, "builder.alice has 1 new message(s) waiting", "builder.alice has 1 taken message(s) not finished")
	if strings.Contains(out, "days ago") {
		t.Errorf("a message taken today shows an age: %s", out)
	}
	taken, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice", ".taken", tok, "*.md"))
	if len(taken) != 1 {
		t.Fatalf("taken: %v", taken)
	}
	old := time.Now().Add(-9*24*time.Hour - time.Hour)
	must(t, os.Chtimes(taken[0], old, old))
	out = runHook(t, home, "UserPromptSubmit", "", "sess-a", p)
	requireContains(t, out, "builder.alice has 1 taken message(s) not finished (oldest taken 9 days ago)")

	// Only taken ones: no "new" line.
	expect(t, sh(t, p, env, "take", "builder.alice", m2, "--token", tok), 0, "take the second")
	out = runHook(t, home, "UserPromptSubmit", "", "sess-a", p)
	requireContains(t, out, "builder.alice has 2 taken message(s) not finished")
	if strings.Contains(out, "new message") {
		t.Errorf("no new message, but: %s", out)
	}
}

// The host inbox lines, outside and inside a team.
func TestHookHostNewAndTaken(t *testing.T) {
	home := isolatedEnv(t)
	sid := "00000000-0000-4000-8000-000000000002"
	inbox := filepath.Join(envValue(home, "SUNSTACK_HOME"), "inbox", sid)
	must(t, os.MkdirAll(filepath.Join(inbox, ".taken"), 0o700))
	msg := func(id string) []byte {
		return []byte("---\nid: " + id + "\nfrom: user\nto: session " + sid + "\nat: 2026-10-13T15:12:03Z\ntype: task\n---\nGoal: x\n")
	}
	must(t, os.WriteFile(filepath.Join(inbox, ".taken", "20261013T151203Z-user-aaaaaa.md"), msg("20261013T151203Z-user-aaaaaa"), 0o600))
	old := time.Now().Add(-9*24*time.Hour - time.Hour)
	must(t, os.Chtimes(filepath.Join(inbox, ".taken", "20261013T151203Z-user-aaaaaa.md"), old, old))

	out := runHook(t, home, "UserPromptSubmit", "", sid, t.TempDir())
	requireContains(t, out, "1 message(s) taken for this session are not finished (oldest taken 9 days ago)")
	if strings.Contains(out, "new message") {
		t.Errorf("no new message, but: %s", out)
	}
	must(t, os.WriteFile(filepath.Join(inbox, "20261022T151203Z-user-bbbbbb.md"), msg("20261022T151203Z-user-bbbbbb"), 0o600))
	out = runHook(t, home, "UserPromptSubmit", "", sid, fixture(t))
	requireContains(t, out, "1 new message(s) are waiting for this session", "1 message(s) taken for this session are not finished")
}
