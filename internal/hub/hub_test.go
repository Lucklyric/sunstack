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
	must(saveConfig(&Config{OrgID: "0123456789abcdef", OrgName: "o", Hub: "hubbox", HubURL: "http://hubbox:7731", Token: "t"}))
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

func TestSealOpen(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	alice, _ := LoadKeys()
	bob := &Keys{}
	{
		home := t.TempDir()
		t.Setenv("SUNSTACK_HOME", home)
		bob, _ = LoadKeys()
	}
	m := &Mail{ID: "20261007T000000Z-user-aaaaaa", FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb"}
	l := &Letter{From: "user", To: "shop/builder.alice", Type: "fyi", At: "x", Body: "SECRET"}
	if err := seal(alice, bob.Public(), m, l); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Sealed, "SECRET") {
		t.Fatal("the body is in the clear")
	}
	got, err := open(bob, alice.Public(), m)
	if err != nil || got.Body != "SECRET" || got.To != l.To {
		t.Fatalf("open: %v %+v", err, got)
	}
	// Another sender's key, another receiver, a changed field: all refused.
	if _, err := open(bob, bob.Public(), m); err == nil {
		t.Error("opened with the wrong sender key")
	}
	if _, err := open(alice, alice.Public(), m); err == nil {
		t.Error("opened by a host it was not sealed to")
	}
	moved := *m
	moved.ToHost = "cccccccccccccccc"
	if _, err := open(bob, alice.Public(), &moved); err == nil {
		t.Error("opened after the hub changed the receiving host")
	}
	if alice.Public().Fingerprint() == bob.Public().Fingerprint() || len(alice.Public().Fingerprint()) != 19 {
		t.Errorf("fingerprints: %s %s", alice.Public().Fingerprint(), bob.Public().Fingerprint())
	}
}

func TestPinsAndTrust(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k1, _ := LoadKeys()
	other := &Keys{}
	{
		t.Setenv("SUNSTACK_HOME", t.TempDir())
		other, _ = LoadKeys()
	}
	r := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop", Keys: k1.Public()}}}
	if err := pinRoster(r); err != nil {
		t.Fatal(err)
	}
	if p := loadPins()["aaaaaaaaaaaaaaaa"]; p == nil || p.Keys != k1.Public() || p.Changed != nil {
		t.Fatalf("first sight not pinned: %+v", p)
	}
	r.Hosts[0].Keys = other.Public()
	_ = pinRoster(r)
	if p := loadPins()["aaaaaaaaaaaaaaaa"]; p.Keys != k1.Public() || p.Changed == nil {
		t.Fatal("a changed key replaced the pin, or was not flagged")
	}
	if _, err := Trust("LAPTOP"); err != nil {
		t.Fatal(err)
	}
	if p := loadPins()["aaaaaaaaaaaaaaaa"]; p.Keys != other.Public() || p.Changed != nil {
		t.Fatal("trust did not take the new key")
	}
	if _, err := Trust("laptop"); err == nil {
		t.Error("trust with no change succeeded")
	}
}

// Teams whose files sync between hosts (git) are one team: another host's
// copy is split off, and a warning both hosts report is shown once.
func TestSplitSharedTeams(t *testing.T) {
	local := &core.Org{
		Attention: []string{"alpha: KR5 needs KR1", "alpha: todo waits for you"},
		Teams:     []*core.OrgTeam{{ID: "00000000000000a1", Name: "alpha", Agents: []*core.OrgAgent{{ID: "pm.lead"}}}},
	}
	remote := &core.Org{
		Host:      core.HostInfo{Name: "server"},
		Attention: []string{"alpha: KR5 needs KR1", "alpha: deploy waits for you", "shop: Q1 waits for you"},
		Teams: []*core.OrgTeam{
			{ID: "00000000000000a1", Name: "alpha", Agents: []*core.OrgAgent{
				{ID: "pm.lead"},
				{ID: "ops.night", Sessions: []*core.HostSession{{Tool: "codex", Status: "busy", Agent: "ops.night", Label: "ops.night@server"}}},
			}},
			{ID: "00000000000000aa", Name: "shop"},
			{ID: "/srv/beta", Name: "beta"}, // no TEAM file: its ID is its root, never shared
		},
		Free: []*core.FreeGroup{{Group: "g", Sessions: []*core.HostSession{{Tool: "claude"}}}},
	}
	own, shared := Split(remote, local)
	if len(shared) != 1 || shared[0].Name != "alpha" {
		t.Fatalf("shared %+v", shared)
	}
	var names []string
	for _, tm := range own.Teams {
		names = append(names, tm.Name)
	}
	if strings.Join(names, ",") != "shop,beta" || len(own.Free) != 1 || own.Host.Name != "server" {
		t.Errorf("own teams %v, free %d", names, len(own.Free))
	}
	if strings.Join(own.Attention, "|") != "alpha: deploy waits for you|shop: Q1 waits for you" {
		t.Errorf("own attention %q", own.Attention)
	}
	if len(remote.Teams) != 3 || len(remote.Attention) != 3 {
		t.Error("Split changed the snapshot")
	}
	if own, shared := Split(remote, nil); len(shared) != 0 || len(own.Teams) != 3 {
		t.Error("without a local scan nothing is shared")
	}

	// sunstack org: a shared team appears under the other host only with
	// its sessions there, and the warning both report is not repeated.
	v := &OrgView{OrgName: "personal", HubName: "server", Hosts: []*HostView{
		{ID: "x1", Name: "server", Hub: true, State: "live", Org: remote, HasSnap: true},
		{ID: "x2", Name: "laptop", You: true, State: "live"},
	}}
	text := v.Text("", local)
	for _, want := range []string{"ops.night", "shop", "also on this host: alpha", "deploy waits for you"} {
		if !strings.Contains(text, want) {
			t.Errorf("org text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "KR5 needs KR1") || strings.Contains(text, "pm.lead") {
		t.Errorf("org text repeats this host's team:\n%s", text)
	}
	// A host whose teams are all here says only that.
	remote.Teams, remote.Free, remote.Attention = remote.Teams[:1], nil, nil
	remote.Teams[0].Agents = remote.Teams[0].Agents[:1]
	text = v.Text("", local)
	if !strings.Contains(text, "no sessions; its teams are also on this host: alpha") || strings.Contains(text, "No teams") {
		t.Errorf("org text:\n%s", text)
	}
}

// A grant names a host by ID and the key it had when granted: a changed
// key, or another host given the same name, never inherits it (§20.7).
func TestGrants(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k1, _ := LoadKeys()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k2, _ := LoadKeys()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	r := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop", Keys: k1.Public()}, {ID: "bbbbbbbbbbbbbbbb", Name: "server", Keys: k2.Public()}}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pinRoster(r))
	g, err := Allow("Laptop", []string{"spawn", "peek"})
	must(err)
	if g.HostID != "aaaaaaaaaaaaaaaa" || g.Fingerprint != k1.Public().Fingerprint() {
		t.Fatalf("grant %+v", g)
	}
	if !Allowed("aaaaaaaaaaaaaaaa", "spawn") || !Allowed("aaaaaaaaaaaaaaaa", "peek") || Allowed("aaaaaaaaaaaaaaaa", "update") || Allowed("bbbbbbbbbbbbbbbb", "spawn") {
		t.Error("grants do not match what was allowed")
	}
	if _, err := Allow("laptop", []string{"shell"}); err == nil {
		t.Error("an unknown kind was granted")
	}
	if _, err := Allow("nobody", []string{"peek"}); err == nil {
		t.Error("an unknown host was granted")
	}
	// Another host renamed to laptop does not inherit the grant.
	r.Hosts[0].Name, r.Hosts[1].Name = "old-laptop", "laptop"
	must(pinRoster(r))
	if Allowed("bbbbbbbbbbbbbbbb", "spawn") || !Allowed("aaaaaaaaaaaaaaaa", "spawn") {
		t.Error("a grant followed the name")
	}
	// A changed key voids it, and trusting the new key does not bring it back.
	r.Hosts[0].Keys = k2.Public()
	must(pinRoster(r))
	if Allowed("aaaaaaaaaaaaaaaa", "spawn") {
		t.Error("a host whose key changed is still allowed")
	}
	if _, err := Trust("old-laptop"); err != nil {
		t.Fatal(err)
	}
	if Allowed("aaaaaaaaaaaaaaaa", "spawn") {
		t.Error("trusting the new key restored the grant")
	}
	if err := Deny("laptop", []string{"peek"}); err == nil {
		t.Error("deny by the name another host now has removed old-laptop's grant")
	}
	must(Deny("old-laptop", []string{"peek"}))
	if txt := GrantsText(); !strings.Contains(txt, "old-laptop: spawn") || !strings.Contains(txt, "void") {
		t.Errorf("grants text: %s", txt)
	}
	must(Deny("old-laptop", []string{"spawn"}))
	if !strings.Contains(GrantsText(), "No host may") {
		t.Errorf("an empty grant was kept: %s", GrantsText())
	}
}

// Control requests (§20.3): an expired request and a reply nobody asked for
// are refused, and a request seen again is never run twice.
func TestControlRequests(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, _ := LoadKeys()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pinRoster(&Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop", Keys: k.Public()}}}))
	pin := loadPins()["aaaaaaaaaaaaaaaa"]
	body := func(exp time.Time) string {
		b, _ := json.Marshal(Control{Expires: stamp(exp), Args: json.RawMessage(`{"target":"shop/builder.alice","lines":5}`)})
		return string(b)
	}
	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa"}
	l := &Letter{From: "user", To: "host", Type: "peek", Body: body(time.Now().Add(-time.Hour))}
	if st, reason, _ := takeRequest(m, pin, l); st != "refused" || !strings.Contains(reason, "expired") {
		t.Errorf("expired request: %s %s", st, reason)
	}
	r := &Letter{From: "user", To: "host", Type: "peek-reply", ReplyTo: core.NewMessageID("user"), Body: "{}"}
	if st, _, _ := takeReply(m, r); st != "refused" {
		t.Error("a reply to no open request was taken")
	}

	ran := make(chan string, 4)
	handlers["peek"] = func(_ string, _ json.RawMessage) (any, error) { ran <- "peek"; return nil, nil }
	defer func() { handlers["peek"] = peekHandler }()
	_, err := Allow("laptop", []string{"peek"})
	must(err)
	l.Body = body(time.Now().Add(time.Minute))
	for i := 0; i < 2; i++ {
		if st, _, _ := takeRequest(m, pin, l); st != "delivered" {
			t.Fatalf("request %d: %s", i, st)
		}
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never ran")
	}
	select {
	case <-ran:
		t.Error("a request seen twice ran twice")
	case <-time.After(500 * time.Millisecond):
	}
}
