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
		Attention: []string{"shop: Q1 waits for you"},
		Teams: []*core.OrgTeam{{Name: "shop", Root: "/srv/shop", Agents: []*core.OrgAgent{{ID: "builder.alice", Sessions: []*core.HostSession{
			{Tool: "codex", Status: "busy", Agent: "builder.alice", Label: "builder.alice_api@server", Team: "00000000000000aa", TeamName: "shop", Pane: "%3", Where: "w:1.0", Reach: "nudge"},
		}}}}},
	}
	return &hub.OrgView{OrgName: "personal", HubName: "hubbox", Outbox: 1, Hosts: []*hub.HostView{
		{ID: "aaaaaaaaaaaaaaaa", Name: "hubbox", Hub: true, State: "live", ContactAge: 2 * time.Second, CanSend: true},
		{ID: "bbbbbbbbbbbbbbbb", Name: "laptop", You: true, State: "live", CanSend: true},
		{ID: "cccccccccccccccc", Name: "server", State: "stale", ContactAge: 45 * time.Second, Waiting: 2, Org: server, HasSnap: true, SnapAge: time.Minute, Fingerprint: "ABCD-EFGH-JKLM-NPQR", KeyChanged: true},
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
	requireAll(t, plain, "Org personal", "◆ hubbox  (hub)", "laptop  (you)", "this host · outbox 1", "◐ stale 45s · 2 waiting", "○ not synced yet", "1 team · 1 session · 1 for you")
	// Wide: a star, the links joined on one bus.
	if !strings.Contains(plain, "┬") || !strings.Contains(plain, "┴") || strings.Contains(plain, "├─ ") {
		t.Errorf("wide layout is not a star:\n%s", plain)
	}
	// The selected host's details on the right.
	m.key("right")
	m.key("right")
	requireAll(t, ansi.Strip(m.View()), "server", "contact", "45s ago", "snapshot", "1m old", "2 message(s) on the hub", "shop  1 agent · 1 busy", "Q1 waits for you", "ABCD-EFGH-JKLM-NPQR", "Its key changed")

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
	// Folding a host hides its rows.
	selectRow(t, m, "server (stale")
	m.key("left")
	if strings.Contains(ansi.Strip(m.View()), "builder.alice_api") {
		t.Error("a folded host still shows its sessions")
	}
}
