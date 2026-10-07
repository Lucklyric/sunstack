package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

func TestSnapshotCarriesNoPrivateText(t *testing.T) {
	act := core.Activity{Title: "SECRET-TITLE", LastPrompt: "SECRET-PROMPT", LastReply: "SECRET-REPLY", LastActive: "2026-10-07T00:00:00Z"}
	s := func() *core.HostSession { return &core.HostSession{Tool: "claude", Status: "idle", Activity: act} }
	o := &core.Org{Schema: 1,
		Teams: []*core.OrgTeam{{Name: "t", Agents: []*core.OrgAgent{{ID: "builder.alice", Sessions: []*core.HostSession{s()}}}, Free: []*core.HostSession{s()}}},
		Free:  []*core.FreeGroup{{Group: "g", Sessions: []*core.HostSession{s()}}},
	}
	stripActivity(o)
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "SECRET") {
		t.Fatalf("private text reaches the wire: %s", b)
	}
	if !strings.Contains(string(b), act.LastActive) {
		t.Error("the last active time was dropped")
	}
}

func TestViewAges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	t.Setenv("SUNSTACK_HOST_NAME", "laptop")
	me := core.ThisHost()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveConfig(&Config{OrgID: "0123456789abcdef", OrgName: "o", Hub: "hubbox"}))
	hubNow := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	local := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) // clocks disagree
	ago := func(d time.Duration) string { return stamp(hubNow.Add(-d)) }
	r := &Roster{OrgName: "o", Hosts: []*Host{
		{ID: "aaaaaaaaaaaaaaaa", Name: "hub-host", Hub: true, Contact: ago(2 * time.Second)},
		{ID: me.ID, Name: "laptop", Contact: ago(time.Hour)},
		{ID: "bbbbbbbbbbbbbbbb", Name: "quiet", Contact: ago(5 * time.Second)},
		{ID: "cccccccccccccccc", Name: "stale", Contact: ago(45 * time.Second)},
		{ID: "dddddddddddddddd", Name: "gone", Contact: ago(20 * time.Minute)},
		{ID: "eeeeeeeeeeeeeeee", Name: "new"},
	}}
	must(writeJSON(filepath.Join(remoteDir(), "roster.json"), &cachedRoster{Roster: r, HubTime: stamp(hubNow), LocalTime: stamp(local)}, 0o600))
	snap := &cachedSnap{Snap: Snap{Host: "bbbbbbbbbbbbbbbb", ReceivedAt: ago(time.Hour), Snapshot: json.RawMessage(`{"schema":1,"teams":[{"name":"t"}],"attention":["a"]}`)}, HubTime: stamp(hubNow), LocalTime: stamp(local)}
	must(writeJSON(filepath.Join(remoteDir(), "bbbbbbbbbbbbbbbb.json"), snap, 0o600))

	state := func(v *OrgView) map[string]string {
		m := map[string]string{}
		for _, h := range v.Hosts {
			m[h.Name] = h.State
		}
		return m
	}
	v := LoadView(local)
	got := state(v)
	want := map[string]string{"hub-host": "live", "laptop": "live", "quiet": "live", "stale": "stale", "gone": "offline", "new": "not synced yet"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s is %q, want %q", k, got[k], w)
		}
	}
	if v.Hosts[0].Name != "hub-host" || v.Hosts[1].Name != "laptop" {
		t.Errorf("order: hub first, then this host: %s, %s", v.Hosts[0].Name, v.Hosts[1].Name)
	}
	for _, h := range v.Hosts {
		if h.Name == "quiet" {
			if teams, _, needs := h.Counts(); teams != 1 || needs != 1 || h.SnapAge != time.Hour {
				t.Errorf("quiet: teams %d needs %d snapshot age %s", teams, needs, h.SnapAge)
			}
		}
	}
	// Cut off from the hub, ages keep counting on the local clock.
	later := state(LoadView(local.Add(40 * time.Second)))
	if later["quiet"] != "stale" || later["hub-host"] != "stale" {
		t.Errorf("after 40 s without news: %v", later)
	}
	if _, err := os.Stat(filepath.Join(home, "org.json")); err != nil {
		t.Fatal(err)
	}
}
