package hub

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Lucklyric/sunstack/internal/core"
)

// bigOrg is a snapshot whose board text alone passes MaxFrame: done entries
// of doneKB and objective key results of krKB, in lines of 256 bytes (four
// per KB), short enough that no line is cut.
func bigOrg(doneKB, krKB int) *core.Org {
	line := strings.Repeat("x", 256)
	a := &core.OrgAgent{ID: "builder.a", Now: []string{"KR1 work"}, Sessions: []*core.HostSession{{Tool: "claude", SessionID: "s1"}}}
	for i := 0; i < 4*doneKB; i++ {
		a.Done = append(a.Done, line)
	}
	ob := core.OrgObjective{Key: "O1", Text: "ship"}
	for i := 0; i < 4*krKB; i++ {
		ob.KRs = append(ob.KRs, line)
	}
	return &core.Org{Schema: core.OrgSchema, Teams: []*core.OrgTeam{{Name: "t", Agents: []*core.OrgAgent{a}, Objectives: []core.OrgObjective{ob}}}}
}

func decodeSnap(t *testing.T, b []byte) *core.Org {
	t.Helper()
	if len(b) > MaxFrame {
		t.Fatalf("snapshot is %d bytes, over %d", len(b), MaxFrame)
	}
	var o core.Org
	if err := json.Unmarshal(b, &o); err != nil || o.Schema < 1 {
		t.Fatalf("not a snapshot: %v", err)
	}
	return &o
}

func TestSnapshotTrimsDoneToFit(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // a trim is recorded in the issue log
	b, err := snapshotOf(bigOrg(300, 10))
	if err != nil {
		t.Fatal(err)
	}
	o := decodeSnap(t, b)
	a := o.Teams[0].Agents[0]
	if len(a.Done) != 0 || len(a.Sessions) != 1 || len(o.Teams[0].Objectives[0].KRs) != 40 {
		t.Fatalf("want done cleared, sessions and key results kept: %+v", a)
	}
	if !strings.Contains(o.Trimmed, "1200 done entries") || strings.Contains(o.Trimmed, "key result") {
		t.Fatalf("trimmed note: %q", o.Trimmed)
	}
}

func TestSnapshotTrimsKeyResultsWhenDoneIsNotEnough(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // a trim is recorded in the issue log
	b, err := snapshotOf(bigOrg(100, 300))
	if err != nil {
		t.Fatal(err)
	}
	o := decodeSnap(t, b)
	if len(o.Teams[0].Objectives[0].KRs) != 0 || len(o.Teams[0].Agents[0].Now) != 1 {
		t.Fatalf("want key results cleared, now kept: %+v", o.Teams[0])
	}
	if !strings.Contains(o.Trimmed, "400 done entries and 1200 key results") {
		t.Fatalf("trimmed note: %q", o.Trimmed)
	}
}

func TestSnapshotUnderLimitUnchanged(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // a trim is recorded in the issue log
	o := bigOrg(5, 5)
	want, _ := json.Marshal(o)
	b, err := snapshotOf(bigOrg(5, 5))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, want) || strings.Contains(string(b), "trimmed") {
		t.Fatal("a snapshot under the limit must go as it is")
	}
}

// The other hosts' view says what a trimmed snapshot left out.
func TestTrimmedSnapshotShown(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // a trim is recorded in the issue log
	b, _ := snapshotOf(bigOrg(300, 10))
	o := decodeSnap(t, b)
	v := &OrgView{OrgName: "personal", HubName: "server", Hosts: []*HostView{{ID: "x1", Name: "server", State: "live", Org: o, HasSnap: true}}}
	if text := v.Text("", nil); !strings.Contains(text, "snapshot cut to fit 256 KB: 1200 done entries not shown") {
		t.Errorf("org text:\n%s", text)
	}
}

// A host's issue counts show in the other hosts' view, counts only (§26).
func TestIssueCountsShown(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // a trim is recorded in the issue log
	o := bigOrg(1, 1)
	o.Issues = map[string]int{"usage": 2, "taken": 3}
	b, _ := snapshotOf(o)
	if strings.Contains(string(b), `"msg"`) {
		t.Fatal("snapshot carries issue details")
	}
	v := &OrgView{OrgName: "personal", HubName: "server", Hosts: []*HostView{{ID: "x1", Name: "server", State: "live", Org: decodeSnap(t, b), HasSnap: true}}}
	if text := v.Text("", nil); !strings.Contains(text, "issues (7 days): taken 3, usage 2") {
		t.Errorf("org text:\n%s", text)
	}
}

// Long board lines are cut to 400 characters in the snapshot first, so a
// host over the limit only because of them keeps every entry (§29.2).
func TestSnapshotCutsLongLinesFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	long := strings.Repeat("y", 3000)
	a := &core.OrgAgent{ID: "builder.a", Now: []string{long}, Sessions: []*core.HostSession{{Tool: "claude", SessionID: "s1"}}}
	ob := core.OrgObjective{Key: "O1", Text: "ship"}
	for i := 0; i < 100; i++ {
		a.Done = append(a.Done, long)
		ob.KRs = append(ob.KRs, long)
	}
	o := &core.Org{Schema: core.OrgSchema, Teams: []*core.OrgTeam{{Name: "t", Agents: []*core.OrgAgent{a}, Objectives: []core.OrgObjective{ob}}}}
	b, err := snapshotOf(o)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeSnap(t, b)
	ga := got.Teams[0].Agents[0]
	if got.Trimmed != "" || len(ga.Done) != 100 || len(got.Teams[0].Objectives[0].KRs) != 100 {
		t.Fatalf("want every entry kept and no trim note: %q, %d done", got.Trimmed, len(ga.Done))
	}
	for _, x := range []string{ga.Now[0], ga.Done[0], got.Teams[0].Objectives[0].KRs[0]} {
		if n := len([]rune(x)); n != 400 || !strings.HasSuffix(x, "…") {
			t.Fatalf("line not cut to 400 characters: %d", n)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home, "log", "issues.jsonl")); len(b) != 0 {
		t.Fatalf("cutting lines is not an issue:\n%s", b)
	}
	// A short line is not touched, and multi-byte text is cut on a character.
	if cutLine("short") != "short" || !utf8.ValidString(cutLine(strings.Repeat("碘", 500))) {
		t.Fatal("cutLine")
	}
}
