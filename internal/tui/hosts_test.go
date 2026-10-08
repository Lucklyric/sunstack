package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

// orgView is an org of four hosts: the hub, this host, a live server with a
// team, and a host that never synced.
func orgView() *hub.OrgView {
	server := &core.Org{
		Host:      core.HostInfo{Name: "server"},
		Attention: []string{"shop: Q1 waits for you", "alpha: todo is waiting for you", "alpha: deploy waits for you"},
		Teams: []*core.OrgTeam{
			{ID: "00000000000000aa", Name: "shop", Root: "/srv/shop", Agents: []*core.OrgAgent{{ID: "builder.alice", Sessions: []*core.HostSession{
				{Tool: "codex", Status: "busy", Agent: "builder.alice", Label: "builder.alice_api@server", Team: "00000000000000aa", TeamName: "shop", Pane: "%3", Where: "w:1.0", Reach: "nudge"},
			}}}},
			// alpha's files sync through git: the same team as this host's.
			{ID: "00000000000000a1", Name: "alpha", Root: "/srv/alpha", Agents: []*core.OrgAgent{
				{ID: "pm.lead", Sessions: []*core.HostSession{
					{Tool: "claude", Status: "waiting", Agent: "pm.lead", Label: "pm.lead_gpu@server", Team: "00000000000000a1", TeamName: "alpha", Pane: "%4", Where: "w:2.0", Reach: "nudge"},
				}},
				{ID: "marketing.social"},
				{ID: "ops.night", Sessions: []*core.HostSession{
					{Tool: "codex", Status: "busy", Agent: "ops.night", Label: "ops.night@server", Team: "00000000000000a1", TeamName: "alpha", Pane: "%5", Where: "w:3.0", Reach: "nudge"},
				}},
			}},
		},
	}
	return &hub.OrgView{OrgName: "personal", HubName: "hubbox", Outbox: 1, Hosts: []*hub.HostView{
		{ID: "aaaaaaaaaaaaaaaa", Name: "hubbox", Hub: true, State: "live", ContactAge: 2 * time.Second, CanSend: true, Version: "0.10.0"},
		{ID: "bbbbbbbbbbbbbbbb", Name: "laptop", You: true, State: "live", CanSend: true, Version: "0.10.0"},
		{ID: "cccccccccccccccc", Name: "server", State: "stale", ContactAge: 45 * time.Second, Waiting: 2, Org: server, HasSnap: true, SnapAge: time.Minute, Fingerprint: "ABCD-EFGH-JKLM-NPQR", KeyChanged: true, Version: "0.9.4"},
		{ID: "dddddddddddddddd", Name: "newbox", State: "not synced yet"},
	}}
}

func hostsModel(t *testing.T, w int) *model {
	t.Helper()
	m := treeModel(t)
	m.w = w
	v := orgView()
	m.loadView = func(time.Time) *hub.OrgView { return v }
	m.loadHosts()
	return m
}

func TestHostsGraph(t *testing.T) {
	m := hostsModel(t, 200)
	m.key("h")
	if m.view != viewHosts {
		t.Fatal("h does not open the Hosts tab")
	}
	v := m.View()
	fits(t, v, m.w, "hosts wide")
	plain := ansi.Strip(v)
	requireAll(t, plain, "Org personal", "◆ hubbox  (hub)", "laptop  (you)", "this host · outbox 1", "◐ stale 45s · 2 waiting", "○ not synced yet", "2 teams · 3 sessions · 2 for you")
	// Wide: a star, the links joined on one bus.
	if !strings.Contains(plain, "┬") || !strings.Contains(plain, "┴") || strings.Contains(plain, "├─ ") {
		t.Errorf("wide layout is not a star:\n%s", plain)
	}
	// The selected host's details on the right.
	m.key("right")
	m.key("right")
	requireAll(t, ansi.Strip(m.View()), "server", "contact", "45s ago", "snapshot", "1m old", "2 message(s) on the hub", "shop  1 agent · 1 busy", "alpha  3 agents · 1 busy, 1 waiting · also on this host", "Q1 waits for you", "deploy waits for you", "ABCD-EFGH-JKLM-NPQR", "Its key changed")

	// Narrow: one line per host under the hub.
	n := hostsModel(t, 70)
	n.key("h")
	nv := n.View()
	fits(t, nv, n.w, "hosts narrow")
	requireAll(t, ansi.Strip(nv), "├─ ", "└─ ", "server  ◐ stale 45s")

	// enter opens the Org tab on that host.
	m.key("enter")
	if m.view != viewOrg {
		t.Fatal("enter does not open the Org tab")
	}
	if nodes := m.treeNodes(); nodes[m.orgSel].kind != "host" || nodes[m.orgSel].host.Name != "server" {
		t.Errorf("enter selected %+v", nodes[m.orgSel])
	}
}

func TestHostsTabOnlyInAnOrg(t *testing.T) {
	m := treeModel(t)
	m.loadView = func(time.Time) *hub.OrgView { return nil }
	m.loadHosts()
	m.key("h")
	if m.view == viewHosts {
		t.Error("the Hosts tab opened outside an org")
	}
}

func TestRemoteSessionsInTheTree(t *testing.T) {
	m := hostsModel(t, 150)
	v := ansi.Strip(m.View())
	requireAll(t, v, "server (stale, 45s)", "shop", "builder.alice_api", "newbox (not synced yet)")
	selectRow(t, m, "builder.alice_api")
	requireAll(t, ansi.Strip(m.View()), "on server, as of its last snapshot (1m old)", "other actions only on server")
	peeked := false
	m.peek = func(*core.HostSession) string { peeked = true; return "" }
	m.View()
	if peeked {
		t.Error("a remote session's pane was peeked")
	}
	for _, k := range []string{"R", "K", "enter"} {
		m.key(k)
		if m.confirm != "" || !strings.Contains(m.note, "server") {
			t.Errorf("%s on a remote session: note %q confirm %q", k, m.note, m.confirm)
		}
		m.confirm = ""
	}
	var sentTo, sentText string
	m.remoteSend = func(to, text string) string { sentTo, sentText = to, text; return "sent" }
	m.key("m")
	for _, r := range "hi" {
		m.key(string(r))
	}
	m.key("enter")
	if sentTo != "server:00000000000000aa/builder.alice_api" || sentText != "hi" {
		t.Errorf("message went to %q with %q", sentTo, sentText)
	}
	// By host (b), folding a host hides its rows.
	m.key("b")
	selectRow(t, m, "server (stale")
	m.key("left")
	if strings.Contains(ansi.Strip(m.View()), "builder.alice_api") {
		t.Error("a folded host still shows its sessions")
	}
}

// A team on both hosts is one team: the other host's sessions sit under its
// agents tagged with the host, and the host's own rows hold only what this
// host lacks.
func TestSharedTeamsMerge(t *testing.T) {
	m := hostsModel(t, 150)
	v := ansi.Strip(m.View())
	if n := strings.Count(v, "▾ alpha"); n != 1 {
		t.Errorf("alpha shown %d times:\n%s", n, v)
	}
	requireAll(t, v, "Needs you (3)", "pm.lead_gpu @server", "ops.night @server", "server (stale, 45s)", "shop")
	var rows []string
	for _, n := range m.treeNodes() {
		rows = append(rows, n.label)
	}
	got := strings.Join(rows, "|")
	// Under alpha: pm.lead's local then remote session, then this host's
	// marketing.social, then the agent only the server runs.
	for _, want := range []string{
		"alpha|pm.lead|pm.lead_cloud|pm.lead_gpu @server|marketing.social|ops.night|ops.night @server",
		"shop  (only on server)|builder.alice|builder.alice_api",
		// Then every host, this one too, with its sessions outside teams.
		"laptop (this host)|vault|todo|tnnls|hubbox (live, 2s)|server (stale, 45s)|no sessions outside teams|newbox (not synced yet)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rows %q lack %q", got, want)
		}
	}
	if strings.Count(got, "alpha") != 1 {
		t.Errorf("rows %q", got)
	}
	// The merged session is the server's: only m, with the hub address.
	selectRow(t, m, "pm.lead_gpu")
	requireAll(t, ansi.Strip(m.View()), "on server, as of its last snapshot")
	var sentTo string
	m.remoteSend = func(to, text string) string { sentTo = to; return "sent" }
	m.key("m")
	m.key("x")
	m.key("enter")
	if sentTo != "server:00000000000000a1/pm.lead_gpu" {
		t.Errorf("message went to %q", sentTo)
	}
	// The team's warnings: this host's once, the server's own tagged.
	selectRow(t, m, "alpha")
	tv := ansi.Strip(m.View())
	if strings.Count(tv, "todo is waiting for you") != 1 || !strings.Contains(tv, "deploy waits for you (on server)") {
		t.Errorf("team details:\n%s", tv)
	}
	selectRow(t, m, "Needs you")
	requireAll(t, ansi.Strip(m.View()), "todo is waiting for you", "deploy waits for you (on server)", "Q1 waits for you (on server)")

	// By host (b): each host with all its own teams, alpha under both.
	m.key("b")
	rows = nil
	for _, n := range m.treeNodes() {
		rows = append(rows, n.label)
	}
	got = strings.Join(rows, "|")
	for _, want := range []string{
		"laptop (this host)|alpha|pm.lead|pm.lead_cloud|marketing.social|free sessions (1)|scratch|outside teams|vault",
		"server (stale, 45s)|shop|builder.alice|builder.alice_api|alpha|pm.lead|pm.lead_gpu|marketing.social|ops.night|ops.night",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("by host: rows %q lack %q", got, want)
		}
	}
	m.key("b")
	n := 0
	for _, r := range m.treeNodes() {
		if r.label == "alpha" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("b does not switch back to by team: alpha %d times", n)
	}
}

// The Hosts tab shows each host's version, marks one behind the newest
// release, and u / U ask it to update (§20.6).
func TestHostsVersionsAndUpdate(t *testing.T) {
	m := hostsModel(t, 200)
	m.latest = "0.10.0"
	m.key("h")
	v := ansi.Strip(m.View())
	requireAll(t, v, "0.10.0", "0.9.4 · update to 0.10.0")
	var asked []string
	m.updateHost = func(name, target string) string { asked = append(asked, name+"@"+target); return name + " updated" }
	m.updateHere = func() string { return "this host updated" }
	// u on the server asks first.
	for m.hosts.Hosts[m.hostSel].Name != "server" {
		m.key("right")
	}
	m.key("u")
	requireAll(t, ansi.Strip(m.View()), "Update sunstack on server to 0.10.0")
	if cmd := m.key("y"); cmd != nil {
		m.Update(cmd())
	}
	if len(asked) != 1 || asked[0] != "server@0.10.0" {
		t.Errorf("u asked %v", asked)
	}
	// U asks every host behind: only server (newbox reports no version).
	asked = nil
	m.key("U")
	requireAll(t, ansi.Strip(m.View()), "server, newbox")
	if cmd := m.key("y"); cmd != nil {
		m.Update(cmd())
	}
	if strings.Join(asked, ",") != "server@0.10.0,newbox@0.10.0" {
		t.Errorf("U asked %v", asked)
	}
	// The Org tab marks the host behind.
	m.view = viewOrg
	marked := false
	for _, n := range m.treeNodes() {
		marked = marked || n.label == "server (stale, 45s) · update 0.9.4"
	}
	if !marked {
		t.Error("the Org tab does not mark the host behind")
	}
	m.view = viewHosts
	// u on this host updates here.
	for !m.hosts.Hosts[m.hostSel].You {
		m.key("right")
	}
	m.key("u")
	if cmd := m.key("y"); cmd != nil {
		m.Update(cmd())
	}
	if !strings.Contains(m.note, "this host updated") {
		t.Errorf("u here: note %q", m.note)
	}
}
