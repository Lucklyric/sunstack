package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Review of v0.18.4 (design §45): message delivery and rename.

func send185(t *testing.T, p *Project, o SendOptions) (*SendResult, error) {
	t.Helper()
	o.NoNudge = true
	if o.Type == "" {
		o.Type = "fyi"
	}
	return p.Send(o)
}

// A delivery killed before its rename leaves a temporary file; the retry of
// the same preset ID still arrives.
func TestPresetIDSurvivesLeftoverTmp(t *testing.T) {
	p := secTeam(t)
	id := "20260101T090000Z-hostb-user-aaaaa0"
	dir := p.inboxDir("builder.ann")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "."+id+".tmp"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := send185(t, p, SendOptions{To: "builder.ann", Body: "hello", ID: id, Op: "x1", FromLabel: "hostb:user", FromHost: "hostb"}); err != nil {
		t.Fatalf("retry after a leftover temp file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, id+".md")); err != nil {
		t.Errorf("message not delivered: %v", err)
	}

	// The session inbox, taken under its lock by --op, does the same.
	tmp := filepath.Join(t.TempDir(), ".x.tmp")
	os.WriteFile(tmp, nil, 0o600)
	dropStaleTmp(tmp, false)
	if _, err := os.Stat(tmp); err != nil {
		t.Error("a fresh temp file of an unlocked inbox was dropped")
	}
	old := time.Now().Add(-2 * time.Minute)
	os.Chtimes(tmp, old, old)
	dropStaleTmp(tmp, false)
	if _, err := os.Stat(tmp); err == nil {
		t.Error("an old temp file was kept")
	}
}

// A preset ID never replaces a message already waiting, taken or archived,
// with or without --op.
func TestPresetIDNeverOverwrites(t *testing.T) {
	p := secTeam(t)
	id := "20260101T090000Z-hostb-user-bbbbb0"
	first := SendOptions{To: "builder.ann", Body: "first", ID: id, FromLabel: "hostb:user", FromHost: "hostb"}
	if _, err := send185(t, p, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Body = "second"
	if _, err := send185(t, p, second); err == nil || !strings.Contains(err.Error(), "already here") {
		t.Errorf("second send with the same id: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(p.inboxDir("builder.ann"), id+".md"))
	if !strings.Contains(string(b), "first") {
		t.Errorf("the waiting message was replaced: %s", b)
	}
}

// setHeader changes one header line and nothing else, and leaves a file
// without a header alone.
func TestSetHeader(t *testing.T) {
	p := secTeam(t)
	f := filepath.Join(t.TempDir(), "m.md")
	body := "---\nid: m\nfrom: a.b\nto: builder.ann\nx-custom: keep # me\ntype: task\n---\n\nGoal: x\n---\nmore\n"
	os.WriteFile(f, []byte(body), 0o644)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	os.Chtimes(f, old, old)
	if err := p.setHeader(f, "to", "builder.bob"); err != nil {
		t.Fatal(err)
	}
	if err := p.setHeader(f, "returned", "s1 2026"); err != nil {
		t.Fatal(err)
	}
	if err := p.setHeader(f, "returned", "s2 2027"); err != nil {
		t.Fatal(err)
	}
	want := "---\nid: m\nfrom: a.b\nto: builder.bob\nx-custom: keep # me\ntype: task\nreturned: s2 2027\n---\n\nGoal: x\n---\nmore\n"
	if b, _ := os.ReadFile(f); string(b) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
	if st, _ := os.Stat(f); !st.ModTime().Equal(old) {
		t.Errorf("file time moved to %s", st.ModTime())
	}
	plain := filepath.Join(t.TempDir(), "p.md")
	os.WriteFile(plain, []byte("just a note\n"), 0o644)
	if err := p.setHeader(plain, "returned", "x"); err == nil {
		t.Error("a file without a header was rewritten")
	}
	if b, _ := os.ReadFile(plain); string(b) != "just a note\n" {
		t.Errorf("plain file changed: %q", b)
	}
}

// A taken file without a header goes back to the inbox with its text.
func TestRequeueKeepsPlainFile(t *testing.T) {
	p := secTeam(t)
	taken := p.takenDir("builder.ann", "deadtoken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(taken, "note.md"), []byte("hand-written, no header\n"), 0o644)
	os.WriteFile(filepath.Join(taken, "20260101T090000Z-user-cccccc.md"), []byte("---\nid: 20260101T090000Z-user-cccccc\nfrom: user\nto: builder.ann\nat: 2026-01-01T09:00:00Z\ntype: task\nextra: kept\n---\n\nGoal: x\n"), 0o644)
	p.requeue("builder.ann", nil, nil)
	if b, _ := os.ReadFile(filepath.Join(p.inboxDir("builder.ann"), "note.md")); string(b) != "hand-written, no header\n" {
		t.Errorf("plain file after requeue: %q", b)
	}
	b, _ := os.ReadFile(filepath.Join(p.inboxDir("builder.ann"), "20260101T090000Z-user-cccccc.md"))
	if !strings.Contains(string(b), "returned: ") || !strings.Contains(string(b), "extra: kept") || !strings.Contains(string(b), "Goal: x") {
		t.Errorf("returned task: %s", b)
	}
}

// A task sent before a rename closes on the renamed agent's reply.
func TestRenameKeepsTasksClosable(t *testing.T) {
	p := secTeam(t)
	lead := filepath.Join(p.Root, "sunstack", "pm.lead")
	os.MkdirAll(lead, 0o755)
	os.WriteFile(filepath.Join(lead, "AGENT.md"), []byte("---\ntitle: pm\nfrom: custom\n---\n## Role\nLeads.\n"), 0o644)
	brief := "Goal: g\nScope: s\nDone when: d\nVerify: v\nReport: r\n"
	lr, err := p.As(AsOptions{Arg: "pm.lead", Tool: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := send185(t, p, SendOptions{To: "builder.ann", Body: brief, Type: "task", From: "pm.lead", Token: lr.Token})
	if err != nil {
		t.Fatal(err)
	}
	held, err := send185(t, p, SendOptions{To: "builder.ann", Body: brief, Type: "task", From: "pm.lead", Token: lr.Token})
	if err != nil {
		t.Fatal(err)
	}
	// One is taken by a session that then ends, one still waits.
	ar, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Take("builder.ann", ar.Token, held.ID); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(p.local("claims", "builder.ann"), ar.Token+".json"))
	if err := p.Release("builder.ann", ar.Token); err != nil {
		t.Logf("release: %v", err)
	}
	if err := p.Rename("builder.ann", "builder.bob"); err != nil {
		t.Fatal(err)
	}
	br, err := p.As(AsOptions{Arg: "builder.bob", Tool: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{waiting.ID, held.ID} {
		if err := p.Take("builder.bob", br.Token, id); err != nil {
			t.Fatalf("take %s: %v", id, err)
		}
		if _, err := send185(t, p, SendOptions{To: "pm.lead", Body: "done\nverified: none, test", Type: "done", ReplyTo: id, From: "builder.bob", Token: br.Token}); err != nil {
			t.Fatal(err)
		}
		if err := p.Ack("builder.bob", br.Token, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, ti := range p.Tasks() {
		if (ti.ID == waiting.ID || ti.ID == held.ID) && ti.Open {
			t.Errorf("task %s still open after the renamed agent's reply: %+v", ti.ID, ti)
		}
	}
}
