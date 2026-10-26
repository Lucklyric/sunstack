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

// Review r33-r2: the commit is the last @ <sha> on the verified: line.
func TestVerifiedRevLast(t *testing.T) {
	for body, want := range map[string]string{
		"verified: git diff @ 8a9c8f4 @ 5cfc9ed":  "5cfc9ed",
		"(verified: go test ./... @ abcdef1)":      "abcdef1",
		"verified: read the diff":                  "",
		"> verified: quoted @ 1234567\nverified: x": "",
	} {
		if got := verifiedRev(body); got != want {
			t.Errorf("verifiedRev(%q) = %q, want %q", body, got, want)
		}
	}
}
