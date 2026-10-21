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
