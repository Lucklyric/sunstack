package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssueLogRotates(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	long := strings.Repeat("x", 1000)
	for i := 0; i < 1500; i++ {
		RecordIssue(IssueEntry{Source: "cli", Cmd: "send", Code: "usage", Exit: 2, Msg: long})
	}
	dir := filepath.Join(Home(), "log")
	cur, err := os.Stat(filepath.Join(dir, "issues.jsonl"))
	if err != nil || cur.Size() > issueMax+1024 {
		t.Fatalf("issues.jsonl: %v %v", cur, err)
	}
	old, err := os.Stat(filepath.Join(dir, "issues.1.jsonl"))
	if err != nil || old.Size() > issueMax+1024 {
		t.Fatalf("issues.1.jsonl: %v %v", old, err)
	}
	if es, _ := os.ReadDir(dir); len(es) != 2 {
		t.Fatalf("want 2 files, got %d", len(es))
	}
	got := ReadIssues(time.Time{})
	if len(got) == 0 || len(got[0].Msg) > issueMsgMax {
		t.Fatalf("read %d entries, msg %d", len(got), len(got[0].Msg))
	}
	if c := IssueCounts(time.Now().Add(-time.Hour)); c["usage"] != len(got) {
		t.Fatalf("counts %v, entries %d", c, len(got))
	}
}

// A repeating failure writes one entry per window, with the repeats in N.
func TestIssueRepeatLimit(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	var lim IssueLimiter
	at := time.Now()
	for i := 0; i < 5; i++ {
		lim.Record(IssueEntry{Source: "connector", Code: "snapshot", Msg: "too large"}, at.Add(time.Duration(i)*time.Minute))
	}
	lim.Record(IssueEntry{Source: "connector", Code: "snapshot", Msg: "too large"}, at.Add(11*time.Minute))
	got := ReadIssues(time.Time{})
	if len(got) != 2 || got[0].N != 0 || got[1].N != 5 {
		t.Fatalf("entries: %+v", got)
	}
}

// A rotation that fails never lets the file pass the bound.
func TestIssueLogRotationFailure(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	dir := filepath.Join(Home(), "log")
	if err := os.MkdirAll(filepath.Join(dir, "issues.1.jsonl", "x"), 0o700); err != nil { // a folder in the way
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte(strings.Repeat("x", issueMax)), 0o600); err != nil {
		t.Fatal(err)
	}
	RecordIssue(IssueEntry{Source: "note", Code: "note", Msg: "one more"})
	if fi, _ := os.Stat(filepath.Join(dir, "issues.jsonl")); fi.Size() > issueMax {
		t.Fatalf("issues.jsonl grew to %d", fi.Size())
	}
}

// The end of a burst is written once its window passes, without a new failure.
func TestIssueLimiterFlush(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	var lim IssueLimiter
	at := time.Now()
	for i := 0; i < 5; i++ {
		lim.Record(IssueEntry{Source: "connector", Code: "connect", Msg: "down"}, at.Add(time.Duration(i)*time.Minute))
	}
	lim.Flush(at.Add(5 * time.Minute)) // window not passed
	if n := len(ReadIssues(time.Time{})); n != 1 {
		t.Fatalf("%d entries before the window passed", n)
	}
	lim.Flush(at.Add(10 * time.Minute))
	got := ReadIssues(time.Time{})
	if len(got) != 2 || got[1].N != 4 || IssueCounts(time.Time{})["connect"] != 5 {
		t.Fatalf("entries: %+v", got)
	}
	lim.Flush(at.Add(30 * time.Minute)) // nothing held back
	if n := len(ReadIssues(time.Time{})); n != 2 {
		t.Fatalf("%d entries after an empty flush", n)
	}
}

// A host's own views show its issue counts.
func TestOwnIssueCountsShown(t *testing.T) {
	o := &Org{Host: HostInfo{Name: "laptop"}, Issues: map[string]int{"usage": 2}}
	if text := o.Text("host"); !strings.Contains(text, "issues (7 days): usage 2") {
		t.Errorf("host view:\n%s", text)
	}
}

// A long code is cut, so every later entry stays readable.
func TestIssueLongCode(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	RecordIssue(IssueEntry{Source: "note", Code: strings.Repeat("x", 70<<10), Msg: "first"})
	RecordIssue(IssueEntry{Source: "note", Code: "note", Msg: "later"})
	got := ReadIssues(time.Time{})
	if len(got) != 2 || len(got[0].Code) != issueCodeMax || got[1].Msg != "later" {
		t.Fatalf("entries: %d", len(got))
	}
}
