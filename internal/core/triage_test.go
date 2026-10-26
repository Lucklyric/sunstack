package core

import (
	"testing"
	"time"
)

// Review r33-r1: an issue recorded in the same second as the triage mark is
// still new, since issue times have whole seconds.
func TestTriageSameSecond(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	if err := MarkTriaged(); err != nil {
		t.Fatal(err)
	}
	RecordIssue(IssueEntry{Source: "note", Code: "note", Msg: "right after the triage"})
	if got := ReadIssues(TriageSince(time.Now())); len(got) != 1 {
		t.Fatalf("issues since the triage: %d, want 1", len(got))
	}
	if _, ok := TriageCheck(time.Now().Add(8 * 24 * time.Hour)); !ok {
		t.Error("no reminder for an issue in the triage's second")
	}
}
