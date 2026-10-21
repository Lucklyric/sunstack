package hub

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// bigOrg is a snapshot whose board text alone passes MaxFrame: done entries
// of doneKB and objective key results of krKB, in 1 KB lines.
func bigOrg(doneKB, krKB int) *core.Org {
	line := strings.Repeat("x", 1024)
	a := &core.OrgAgent{ID: "builder.a", Now: []string{"KR1 work"}, Sessions: []*core.HostSession{{Tool: "claude", SessionID: "s1"}}}
	for i := 0; i < doneKB; i++ {
		a.Done = append(a.Done, line)
	}
	ob := core.OrgObjective{Key: "O1", Text: "ship"}
	for i := 0; i < krKB; i++ {
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
	b, err := snapshotOf(bigOrg(300, 10))
	if err != nil {
		t.Fatal(err)
	}
	o := decodeSnap(t, b)
	a := o.Teams[0].Agents[0]
	if len(a.Done) != 0 || len(a.Sessions) != 1 || len(o.Teams[0].Objectives[0].KRs) != 10 {
		t.Fatalf("want done cleared, sessions and key results kept: %+v", a)
	}
	if !strings.Contains(o.Trimmed, "300 done entries") || strings.Contains(o.Trimmed, "key result") {
		t.Fatalf("trimmed note: %q", o.Trimmed)
	}
}

func TestSnapshotTrimsKeyResultsWhenDoneIsNotEnough(t *testing.T) {
	b, err := snapshotOf(bigOrg(100, 300))
	if err != nil {
		t.Fatal(err)
	}
	o := decodeSnap(t, b)
	if len(o.Teams[0].Objectives[0].KRs) != 0 || len(o.Teams[0].Agents[0].Now) != 1 {
		t.Fatalf("want key results cleared, now kept: %+v", o.Teams[0])
	}
	if !strings.Contains(o.Trimmed, "100 done entries and 300 key results") {
		t.Fatalf("trimmed note: %q", o.Trimmed)
	}
}

func TestSnapshotUnderLimitUnchanged(t *testing.T) {
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
	b, _ := snapshotOf(bigOrg(300, 10))
	o := decodeSnap(t, b)
	v := &OrgView{OrgName: "personal", HubName: "server", Hosts: []*HostView{{ID: "x1", Name: "server", State: "live", Org: o, HasSnap: true}}}
	if text := v.Text("", nil); !strings.Contains(text, "snapshot cut to fit 256 KB: 300 done entries not shown") {
		t.Errorf("org text:\n%s", text)
	}
}
