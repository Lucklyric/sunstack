package main

// §31: returned tasks are marked, their senders told, and a sender can name
// the task a new one replaces.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inboxTexts(t *testing.T, p, id string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", id, "*.md"))
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		must(t, err)
		out = append(out, string(b))
	}
	return out
}

func countContaining(texts []string, want string) int {
	n := 0
	for _, s := range texts {
		if strings.Contains(s, want) {
			n++
		}
	}
	return n
}

func TestReturnedTaskMarked(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	r := sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--from", "reviewer", "--token", tr)
	expect(t, r, 0, "task from reviewer")
	task := field(msgRe, r.out)
	fromUser := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief).out)
	expect(t, sh(t, p, nil, "take", "builder.alice", task, "--token", ta), 0, "take")
	expect(t, sh(t, p, nil, "take", "builder.alice", fromUser, "--token", ta), 0, "take the user's")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", ta), 0, "release with tasks taken")

	// Back in the inbox, marked, and logged.
	inbox := inboxTexts(t, p, "builder.alice")
	if countContaining(inbox, "\nreturned: builder.alice ") != 2 {
		t.Errorf("returned header missing:\n%s", strings.Join(inbox, "\n----\n"))
	}
	requireContains(t, eventsLog(t, p), "requeue  builder.alice  "+task)
	// The agent sender is told once; the user's task sends nothing.
	notes := inboxTexts(t, p, "reviewer")
	if countContaining(notes, task+" returned unfinished by builder.alice") != 1 || len(notes) != 1 {
		t.Errorf("sender notes:\n%s", strings.Join(notes, "\n----\n"))
	}

	// check, tasks and as show it.
	ta = field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	r = sh(t, p, nil, "as", "builder.alice", "--token", ta)
	requireContains(t, r.out, "returned, no owner")
	requireContains(t, sh(t, p, nil, "check", "builder.alice", "--token", ta).out, "returned: by builder.alice", "unfinished")
	requireContains(t, sh(t, p, nil, "tasks").out, "returned by builder.alice")
	requireContains(t, sh(t, p, nil, "next", "--all").out, "returned unfinished")

	// Returned again: one header, one more note; check sends none.
	expect(t, sh(t, p, nil, "take", "builder.alice", task, "--token", ta), 0, "take again")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", ta), 0, "release again")
	for _, s := range inboxTexts(t, p, "builder.alice") {
		if n := strings.Count(s, "\nreturned: "); n > 1 {
			t.Errorf("%d returned headers:\n%s", n, s)
		}
	}
	ta = field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	sh(t, p, nil, "check", "builder.alice", "--token", ta)
	if n := countContaining(inboxTexts(t, p, "reviewer"), "returned unfinished"); n != 2 {
		t.Errorf("want 2 notes after two releases, got %d", n)
	}
}

func TestSendReplaces(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	tb := field(tokenRe, sh(t, p, nil, "as", "builder.bob").out)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	asReviewer := []string{"--from", "reviewer", "--token", tr}
	t1 := field(msgRe, sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief}, asReviewer...)...).out)
	note := field(msgRe, sh(t, p, nil, append([]string{"send", "builder.alice", "an fyi"}, asReviewer...)...).out)

	r := sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1}, asReviewer...)...)
	expect(t, r, 0, "replaces own task")
	t2 := field(msgRe, r.out)
	out := sh(t, p, nil, "check", "builder.alice", "--token", ta).out
	requireContains(t, out, "replaces: "+t1, "replaced by "+t2)

	expect(t, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1, "--from", "builder.bob", "--token", tb), 2, "another sender's task")
	expect(t, sh(t, p, nil, append([]string{"send", "builder.bob", "--type", "task", "--file", brief, "--replaces", t1}, asReviewer...)...), 2, "another recipient's task")
	expect(t, sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief, "--replaces", note}, asReviewer...)...), 2, "not a task")
	expect(t, sh(t, p, nil, append([]string{"send", "builder.alice", "x", "--replaces", t1}, asReviewer...)...), 2, "the new message is not a task")
	expect(t, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--replaces", "20260101T000000Z-user-abcdef"), 2, "unknown task")
	// The user may name any task to that recipient.
	expect(t, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1), 0, "the user replaces")

	// Taken by this session, the old one still shows as replaced.
	expect(t, sh(t, p, nil, "take", "builder.alice", t1, "--token", ta), 0, "take the old one")
	// Replaced twice (by t2, then by the user's task): the latest is named.
	requireContains(t, sh(t, p, nil, "check", "builder.alice", "--token", ta).out, t1+" (taken (by this session))", "replaced by ")
}
