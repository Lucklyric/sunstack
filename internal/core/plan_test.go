package core

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func boardsOf(team string, agents map[string]string) *Boards {
	b := &Boards{Agents: map[string][]Item{}}
	b.Team, _ = parseBoard("user", []byte(team))
	for id, doc := range agents {
		b.Agents[id], _ = parseBoard(id, []byte(doc))
	}
	return b
}

func krOf(t *testing.T, objs []*ObjectiveView, ref string) *KRView {
	t.Helper()
	for _, o := range objs {
		for _, k := range o.KRs {
			if k.Ref() == ref {
				return k
			}
		}
	}
	t.Fatalf("no %s", ref)
	return nil
}

// Each status, the order between them, needs of every kind, and objective
// summaries (§21.1).
func TestPlan(t *testing.T) {
	today, _ := time.Parse("2006-01-02", "2026-10-09")
	team := `## Objectives
- 2026-09-01 O1 Ship the export(due: 2026-12-01)
- 2026-09-01 O2 Nothing planned yet
- 2026-09-01 O3 Finished
## User
- 2026-10-01 KR1 [O1] Review the draft
`
	agents := map[string]string{
		"alice": `## Now
- 2026-10-08 KR1 [O1] Fresh work
- 2026-09-20 KR2 [O1] Old work (needs: KR1)
- 2026-10-08 KR3 [O1] Late and blocked (due: 2026-10-01) (needs: bob#KR1)
- 2026-10-08 KR4 [O1] Waits on an ask (needs: user#Q1)
- 2026-10-08 KR5 [O1] Waits on text (needs: user: the data dump, carol#KR9)
- 2026-10-08 KR6 [O9] Unknown objective
- 2026-10-08 KR7 Untagged (needs: KR8)
## Next
- 2026-10-08 KR8 [O1] Planned (needs: KR7)
## Done
- 2026-10-02 KR9 [O3] Shipped (verified: test)
## Asks
- 2026-10-08 Q1 Which format? (options: csv | json)
`,
		"bob": `## Now
- 2026-10-08 KR1 [O1] Same ID as alice's KR1 (done)
`,
	}
	objs := boardsOf(team, agents).Plan(today)

	for ref, want := range map[string]string{
		"user#KR1":  "open",
		"alice#KR1": "active",
		"alice#KR2": "blocked", // needs KR1, still active
		"alice#KR3": "overdue", // bob#KR1 is done, so not blocked
		"alice#KR4": "blocked",
		"alice#KR5": "blocked",
		"alice#KR7": "blocked", // KR7 and KR8 need each other: no loop
		"alice#KR8": "blocked",
		"alice#KR9": "done",
		"bob#KR1":   "done",
	} {
		if got := krOf(t, objs, ref).Status; got != want {
			t.Errorf("%s: %s, want %s", ref, got, want)
		}
	}
	if c := krOf(t, objs, "alice#KR2").Conditions; strings.Join(c, ",") != "blocked,stale,active" {
		t.Errorf("alice#KR2 conditions: %v", c)
	}
	n := krOf(t, objs, "alice#KR5").Needs
	if len(n) != 2 || n[0].Status != "waiting" || n[0].Ref != "" || n[1].Ref != "" {
		t.Errorf("free text and a missing target: %+v", n)
	}
	if n := krOf(t, objs, "alice#KR4").Needs; n[0].Ref != "alice#Q1" || n[0].Status != "waiting on the user" {
		t.Errorf("an ask need: %+v", n)
	}
	if n := krOf(t, objs, "alice#KR2").Needs; n[0].Ref != "alice#KR1" || n[0].Status != "active" {
		t.Errorf("a bare need is the owner's own: %+v", n)
	}

	got := map[string]*ObjectiveView{}
	for _, o := range objs {
		got[o.Key] = o
	}
	if o := got["O1"]; o.KRs[0].Owner != "user" || o.Done != 1 || o.Status != "blocked" {
		t.Errorf("O1: first %s, %d done, %s", o.KRs[0].Ref(), o.Done, o.Status)
	}
	if got["O2"].Status != "no key results" || got["O3"].Status != "all done" {
		t.Errorf("O2 %q, O3 %q", got["O2"].Status, got["O3"].Status)
	}
	none := got[""]
	if none == nil || len(none.KRs) != 2 {
		t.Fatalf("no objective group: %+v", none)
	}
	for _, o := range objs {
		for _, k := range o.KRs {
			if strings.HasPrefix(k.Key, "Q") {
				t.Errorf("an ask counted: %s", k.Ref())
			}
		}
	}
}

// Task details carry bodies and the lifecycle, and reading them moves no
// message (§21.2).
func TestTaskDetails(t *testing.T) {
	root := t.TempDir()
	p := &Project{Root: root, Dir: filepath.Join(root, "sunstack")}
	for _, id := range []string{"lead", "alice", "bob"} {
		os.MkdirAll(p.AgentDir(id), 0o755)
		os.WriteFile(filepath.Join(p.AgentDir(id), "AGENT.md"), []byte("# "+id+"\n"), 0o644)
	}
	msg := func(dir, id, from, to, typ, extra, body string) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, id+".md"), []byte("---\nid: "+id+"\nfrom: "+from+"\nto: "+to+"\nat: "+id[:4]+"-"+id[4:6]+"-"+id[6:8]+"T"+id[9:11]+":"+id[11:13]+":"+id[13:15]+"Z\ntype: "+typ+"\n"+extra+"---\n"+body), 0o644)
	}
	const (
		pendingNoSession = "20261008T100000Z-lead-aaaaa1"
		takenGone        = "20261008T100100Z-lead-aaaaa2"
		takenLive        = "20261008T100200Z-lead-aaaaa3"
		replied          = "20261001T100300Z-lead-aaaaa4"
		reply            = "20261008T110000Z-alice-bbbbb1"
	)
	msg(p.inboxDir("bob"), pendingNoSession, "lead", "bob", "task", "", "Goal: write the export\nbrief text\n")
	msg(p.takenDir("alice", "0123456789abcdef"), takenGone, "lead", "alice", "task", "", "Goal: gone\n")
	msg(p.takenDir("alice", "fedcba9876543210"), takenLive, "lead", "alice", "task", "", "Goal: live\n")
	msg(p.messageArchive(), replied, "lead", "alice", "task", "", "Goal: old\n")
	msg(p.messageArchive(), reply, "alice", "lead", "done", "reply_to: "+replied+"\n", "did it\nverified: go test\n")
	os.MkdirAll(p.claimDir("alice"), 0o755)
	os.WriteFile(filepath.Join(p.claimDir("alice"), "c.json"), []byte(`{"tool":"claude","host":"h","token":"fedcba9876543210","claimed":"2026-10-08T10:00:00Z","task":"exp"}`), 0o644)

	before := treeSum(t, p.Dir)
	today, _ := time.Parse("2006-01-02", "2026-10-09")
	got := map[string]*TaskDetail{}
	for _, d := range p.TaskDetails(today) {
		got[d.ID] = d
	}
	for id, want := range map[string]string{
		pendingNoSession: "pending, no live session",
		takenGone:        "taken, session gone",
		takenLive:        "taken by alice_exp",
		replied:          "done reply",
	} {
		if d := got[id]; d == nil || d.Lifecycle != want {
			t.Errorf("%s: %+v, want %s", id, d, want)
		}
	}
	if d := got[pendingNoSession]; !strings.Contains(d.Body, "brief text") {
		t.Errorf("task body: %q", d.Body)
	}
	if d := got[replied]; len(d.Replies) != 1 || d.Replies[0].From != "alice" || !strings.Contains(d.Replies[0].Body, "verified: go test") || !d.Replies[0].Verified {
		t.Errorf("reply: %+v", d.Replies)
	}
	if after := treeSum(t, p.Dir); after != before {
		t.Error("reading task details changed a file")
	}

	late, _ := time.Parse("2006-01-02", "2026-10-20")
	for _, d := range p.TaskDetails(late) {
		if d.ID == pendingNoSession && strings.Join(d.Findings, ",") != "overdue" {
			t.Errorf("overdue: %v", d.Findings)
		}
	}
}

// treeSum hashes every file's path and content under dir.
func treeSum(t *testing.T, dir string) [32]byte {
	t.Helper()
	h := sha256.New()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(path)
			h.Write([]byte(path))
			h.Write(b)
		}
		return nil
	})
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
