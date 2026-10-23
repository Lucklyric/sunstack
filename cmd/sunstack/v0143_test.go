package main

// §31: returned tasks are marked, their senders told, and a sender can name
// the task a new one replaces.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
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

// Review findings (§31.6).

// "replaced by" stays while the newer task is taken by another session or archived.
func TestReplacedBySeenAcrossSessions(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--task", "api").out)
	tb := field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--join", "--task", "docs").out)
	asReviewer := []string{"--from", "reviewer", "--token", tr}
	t1 := field(msgRe, sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief}, asReviewer...)...).out)
	expect(t, sh(t, p, nil, "take", "builder.alice", t1, "--token", ta), 0, "A takes the old task")
	t2 := field(msgRe, sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1}, asReviewer...)...).out)
	expect(t, sh(t, p, nil, "take", "builder.alice", t2, "--token", tb), 0, "B takes the new task")
	requireContains(t, sh(t, p, nil, "check", "builder.alice", "--token", ta).out, "replaced by "+t2)
	expect(t, sh(t, p, nil, "ack", "builder.alice", t2, "--token", tb), 0, "B acks the new task")
	requireContains(t, sh(t, p, nil, "check", "builder.alice", "--token", ta).out, "replaced by "+t2)
}

// A busy sender lock does not lose the notice silently: release says so.
func TestReturnNoticeBusySender(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	task := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--from", "reviewer", "--token", tr).out)
	expect(t, sh(t, p, nil, "take", "builder.alice", task, "--token", ta), 0, "take")
	lock := filepath.Join(p, "sunstack", "_local", "locks", "reviewer")
	must(t, os.MkdirAll(lock, 0o755))
	owner := fmt.Sprintf("2026-10-23T00:00:00Z pid %d", os.Getpid())
	if st := core.ProcStart(os.Getpid()); st != "" {
		owner += " start " + st
	}
	must(t, os.WriteFile(filepath.Join(lock, "owner"), []byte(owner+"\n"), 0o644))
	r := sh(t, p, nil, "release", "builder.alice", "--token", ta)
	requireContains(t, r.out+r.stderr, "released", "could not tell reviewer", task)
	must(t, os.RemoveAll(lock))
	if n := len(inboxTexts(t, p, "builder.alice")); n != 1 {
		t.Errorf("task not back in the inbox: %d", n)
	}
}

// A repeated --op send of a replacement returns the first one even after
// the old task was closed.
func TestReplacesRetryWithOp(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	asReviewer := []string{"--from", "reviewer", "--token", tr}
	t1 := field(msgRe, sh(t, p, nil, append([]string{"send", "builder.alice", "--type", "task", "--file", brief}, asReviewer...)...).out)
	args := append([]string{"send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1, "--op", "repl-1"}, asReviewer...)
	t2 := field(msgRe, sh(t, p, nil, args...).out)
	expect(t, sh(t, p, nil, "send", "reviewer", "closed: replaced by "+t2+"\nverified: none, closed per reviewer", "--type", "done", "--reply-to", t1, "--from", "builder.alice", "--token", ta), 0, "close the old one")
	r := sh(t, p, nil, args...)
	expect(t, r, 0, "retry with the same --op")
	requireContains(t, r.out, t2, "already sent")
}

// A returned header that cannot be written still returns the task, logged as unmarked.
func TestReturnedMarkFailureKeepsTask(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	task := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief).out)
	expect(t, sh(t, p, nil, "take", "builder.alice", task, "--token", ta), 0, "take")
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice", ".taken", ta, task+".md.tmp", "x"), 0o755))
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", ta), 0, "release")
	if n := len(inboxTexts(t, p, "builder.alice")); n != 1 {
		t.Fatalf("task lost: %d in the inbox", n)
	}
	requireContains(t, eventsLog(t, p), "requeue  builder.alice  "+task+"  unmarked")
}

// Round-2 review findings.

// Taking and returning a done reply keeps its file time, which orders
// same-second replies in the ledger.
func TestReturnKeepsReplyTime(t *testing.T) {
	p := fixture(t)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	reply := field(msgRe, sh(t, p, nil, "send", "builder.alice", "done\nverified: x", "--type", "done", "--reply-to", "20260101T000000Z-user-abcdef").out)
	path := filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice", reply+".md")
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	must(t, os.Chtimes(path, old, old))
	expect(t, sh(t, p, nil, "take", "builder.alice", reply, "--token", ta), 0, "take the reply")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", ta), 0, "release")
	st, err := os.Stat(path)
	must(t, err)
	if !st.ModTime().Equal(old) {
		t.Errorf("file time changed: %v", st.ModTime())
	}
}

// --op matching includes --replaces.
func TestOpIncludesReplaces(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	t1 := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief).out)
	t2 := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--op", "round").out)
	r := sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--op", "round", "--replaces", t1)
	if strings.Contains(r.out, "already sent") || strings.Contains(r.out, t2) {
		t.Errorf("a changed --replaces was taken as a repeat: %s", r.out)
	}
}

// The newest replacement is named, wherever each one is stored.
func TestLatestReplacementNamed(t *testing.T) {
	p := fixture(t)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	t1 := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief).out)
	b := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1).out)
	expect(t, sh(t, p, nil, "ack", "builder.alice", b, "--token", ta), 0, "ack the first replacement")
	time.Sleep(1100 * time.Millisecond)
	c := field(msgRe, sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", brief, "--replaces", t1).out)
	requireContains(t, sh(t, p, nil, "check", "builder.alice", "--token", ta).out, "replaced by "+c)
}

// A returned handoff shows in next, like a returned task (round-3 review).
func TestReturnedHandoffInNext(t *testing.T) {
	p := fixture(t)
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	h := field(msgRe, sh(t, p, nil, "send", "builder.alice", "take over the export", "--type", "handoff").out)
	expect(t, sh(t, p, nil, "take", "builder.alice", h, "--token", ta), 0, "take")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", ta), 0, "release")
	requireContains(t, sh(t, p, nil, "next", "--all").out, "handoff "+h, "returned unfinished")
}
