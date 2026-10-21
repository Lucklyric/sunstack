package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
				{Tool: "codex", Status: "busy", Agent: "builder.alice", Label: "builder.alice_api@server", Team: "00000000000000aa", TeamName: "shop", Pane: "%3", Where: "w:1.0", Reach: "nudge", SessionID: "00000000-0000-4000-8000-0000000000aa"},
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
	// The user runs the TUI: not from an agent session (core.UserOnly).
	for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "TMUX_PANE"} {
		t.Setenv(k, "")
	}
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
	requireAll(t, plain, "Org personal", "◆ hubbox  (hub)", "laptop  (you)", "this host  outbox 1", "◐ stale 45s  2 waiting", "○ not synced yet", "2 teams  3 sessions  2 for you")
	// Wide: a star, the links joined on one bus.
	if !strings.Contains(plain, "┬") || !strings.Contains(plain, "┴") || strings.Contains(plain, "├─ ") {
		t.Errorf("wide layout is not a star:\n%s", plain)
	}
	// The selected host's details on the right.
	m.key("right")
	m.key("right")
	requireAll(t, ansi.Strip(m.View()), "server", "contact", "45s ago", "snapshot", "1m old", "2 message(s) on the hub", "shop  1 agent  1 busy", "alpha  3 agents  1 busy, 1 waiting  also on this host", "Q1 waits for you", "deploy waits for you", "ABCD-EFGH-JKLM-NPQR", "Its key changed")

	// Narrow: one line per host under the hub.
	n := hostsModel(t, 70)
	n.key("h")
	nv := n.View()
	fits(t, nv, n.w, "hosts narrow")
	requireAll(t, ansi.Strip(nv), "├─ ", "└─ ", "server", "◐ stale")

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

func TestHostsEmptyAndLongRoster(t *testing.T) {
	m := hostsModel(t, 80)
	m.view, m.h = viewHosts, 24
	m.hosts.Hosts = nil
	requireAll(t, m.View(), "No hosts available.", "sunstack org join <hub>")
	for _, k := range []string{"up", "down", "enter", "u"} {
		m.key(k)
	}
	for i := 0; i < 30; i++ {
		m.hosts.Hosts = append(m.hosts.Hosts, &hub.HostView{
			ID: fmt.Sprint(i), Name: fmt.Sprintf("server-%02d", i), State: "live", Hub: i == 0,
		})
	}
	m.hostSel = 29
	screen := m.View()
	requireAll(t, screen, "Org personal", "server-29", "▸ ")
	fits(t, screen, m.w, "long host roster")
	if strings.Contains(screen, "server-00") || strings.Contains(screen, "server-01") {
		t.Error("host list did not scroll to selection")
	}
	if n := strings.Count(screen, "\n") + 1; n > m.h {
		t.Errorf("host roster uses %d lines on a %d-line screen", n, m.h)
	}
}

func TestHostsDetailsScroll(t *testing.T) {
	m := hostsModel(t, 80)
	m.view, m.h, m.hostSel = viewHosts, 24, 2
	requireAll(t, m.View(), "pgup/pgdn")
	before := m.View()
	m.key("pgdown")
	if after := m.View(); after == before {
		t.Error("page down did not scroll host details")
	} else {
		fits(t, after, m.w, "host details after page down")
	}
	m.key("down")
	if m.orgScroll != 0 {
		t.Error("selecting another host did not reset detail scroll")
	}
}

func TestRemoteSessionsInTheTree(t *testing.T) {
	m := hostsModel(t, 150)
	v := ansi.Strip(m.View())
	requireAll(t, v, "shop", "builder.alice_api @server")
	if strings.Contains(v, "newbox") || strings.Contains(v, "Needs you (") {
		t.Errorf("project order lists hosts or a needs row:\n%s", v)
	}
	selectRow(t, m, "builder.alice_api")
	requireAll(t, ansi.Strip(m.View()), "on server, as of its last snapshot (1m old)", "Other actions only on server")
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
	// By host (b twice: project, team, host), folding a host hides its rows.
	m.key("b")
	m.key("b")
	requireAll(t, ansi.Strip(m.View()), "server (stale, 45s)", "newbox (not synced yet)")
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
	// The header counts what needs the user on every host, each item once:
	// this host's todo, and the server's own Q1 and deploy.
	requireAll(t, v, "pm.lead_gpu @server", "ops.night @server", "shop", "3 for you")
	rowsOf := func() string {
		var rows []string
		for _, n := range m.treeNodes() {
			rows = append(rows, n.label)
		}
		return strings.Join(rows, "|")
	}
	// By project (§28.2): teams, then the groups outside teams, each session
	// with its host; no needs row and no host rows.
	got := rowsOf()
	for _, want := range []string{
		"alpha|pm.lead|pm.lead_cloud @laptop|pm.lead_gpu @server|marketing.social|ops.night|ops.night @server|free sessions (1)|scratch @laptop",
		"shop  (only on server)|builder.alice|builder.alice_api @server|vault|todo @laptop|tnnls @laptop",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("by project: rows %q lack %q", got, want)
		}
	}
	if strings.Count(got, "alpha") != 1 || strings.Contains(got, "Needs you") || strings.Contains(got, "(this host)") {
		t.Errorf("by project: rows %q", got)
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

	// By team (b): the same teams, then one "no team" row with the groups.
	m.key("b")
	if got := rowsOf(); !strings.Contains(got, "builder.alice_api @server|no team|vault|todo @laptop|tnnls @laptop") {
		t.Errorf("by team: rows %q", got)
	}

	// By host (b): each host with all its own teams, alpha under both; a host
	// row keeps its count.
	m.key("b")
	hv := ansi.Strip(m.View())
	if !strings.Contains(hv, "laptop (this host)") || !strings.Contains(hv, "3 for you") {
		t.Errorf("host order:\n%s", hv)
	}
	for _, n := range m.treeNodes() {
		if n.kind == "host" && n.host.Name == "server" {
			if row := ansi.Strip(m.row(n, 60)); !strings.Contains(row, "3 for you") {
				t.Errorf("server row %q lacks its count", row)
			}
		}
	}
	got = rowsOf()
	for _, want := range []string{
		"laptop (this host)|alpha|pm.lead|pm.lead_cloud|marketing.social|free sessions (1)|scratch|vault|todo|tnnls",
		"server (stale, 45s)|shop|builder.alice|builder.alice_api|alpha|pm.lead|pm.lead_gpu|marketing.social|ops.night|ops.night",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("by host: rows %q lack %q", got, want)
		}
	}
	m.key("b")
	if got := rowsOf(); strings.Count(got, "alpha") != 1 || strings.Contains(got, "no team") {
		t.Errorf("b does not cycle back to by project: %q", got)
	}
}

// The Hosts tab shows each host's version, marks one behind the newest
// release, and u / U ask it to update (§20.6).
func TestHostsVersionsAndUpdate(t *testing.T) {
	m := hostsModel(t, 200)
	m.latest = "0.10.0"
	m.key("h")
	v := ansi.Strip(m.View())
	requireAll(t, v, "0.10.0", "0.9.4  update to 0.10.0")
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
	// U asks every host behind; newbox never synced, so it is skipped.
	asked = nil
	m.key("U")
	requireAll(t, ansi.Strip(m.View()), "on server (skipping newbox (not synced yet))")
	if cmd := m.key("y"); cmd != nil {
		m.Update(cmd())
	}
	if strings.Join(asked, ",") != "server@0.10.0" {
		t.Errorf("U asked %v", asked)
	}
	// The Org tab marks the host behind, in host order.
	m.view, m.order = viewOrg, orderHost
	marked := false
	for _, n := range m.treeNodes() {
		marked = marked || n.label == "server (stale, 45s)  update 0.9.4"
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

// p peeks another host's session through the hub and shows the lines in its
// details; s starts a session from a row: on another host's rows there.
func TestRemotePeekAndSpawnKeys(t *testing.T) {
	m := hostsModel(t, 150)
	var peeked, spawned []string
	m.peekRemote = func(host, target string) (string, error) {
		peeked = append(peeked, host+":"+target)
		return "REMOTE-PANE-LINE\n", nil
	}
	m.spawnRemote = func(host string, a hub.SpawnArgs) string {
		spawned = append(spawned, host+" "+a.Team+"/"+a.Agent+" free="+fmt.Sprint(a.Free)+" beside="+a.Beside+" tool="+a.Tool+" note="+a.Note)
		return "started"
	}
	m.spawnLocal = func(n treeNode, tool, note string) string {
		spawned = append(spawned, "local "+n.kind+" "+n.label+" tool="+tool+" note="+note)
		return "started"
	}
	run := func(cmd tea.Cmd) {
		if cmd != nil {
			m.Update(cmd())
		}
	}

	selectRow(t, m, "builder.alice_api")
	run(m.key("p"))
	if len(peeked) != 1 || peeked[0] != "server:00000000000000aa/builder.alice_api" {
		t.Fatalf("p peeked %v", peeked)
	}
	requireAll(t, ansi.Strip(m.View()), "pane on server", "REMOTE-PANE-LINE")

	// s on the server's agent starts it there, after the line and a y.
	selectRow(t, m, "builder.alice")
	m.key("s")
	for _, r := range "codex fix the build" {
		m.key(string(r))
	}
	m.key("enter")
	requireAll(t, ansi.Strip(m.View()), "Start builder.alice on server")
	run(m.key("y"))
	// s on a session starts a free session beside it, on its host.
	selectRow(t, m, "builder.alice_api")
	m.key("s")
	m.key("enter")
	run(m.key("y"))
	// s on this host's team starts a free session here.
	selectRow(t, m, "alpha")
	m.key("s")
	for _, r := range "claude" {
		m.key(string(r))
	}
	m.key("enter")
	run(m.key("y"))
	want := []string{
		"server shop/builder.alice free=false beside= tool=codex note=fix the build",
		"local team alpha tool=claude note=",
	}
	got := strings.Join(spawned, "|")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("spawned %q, want %q", got, w)
		}
	}
	if !strings.Contains(got, "server / free=true beside=00000000-0000-4000-8000-0000000000aa") {
		t.Errorf("no free session beside the remote one: %q", got)
	}
}

// The Hosts tab shows SSH both ways; c checks and S opens the shared
// connection for a mapped host, and both refuse this host and unmapped
// ones (§23.3).
func TestHostsSSHKeys(t *testing.T) {
	m := hostsModel(t, 200)
	state, _ := json.Marshal(core.SSHState{
		Aliases: map[string]*core.SSHAlias{"srv": {ControlMaster: "auto", ControlPath: filepath.Join(t.TempDir(), "no-socket")}},
		Hosts:   map[string]string{"cccccccccccccccc": "srv"},
	})
	if err := os.WriteFile(filepath.Join(os.Getenv("SUNSTACK_HOME"), "ssh.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	m.hosts.Hosts[2].SSH = "ssh srv: no open connection"
	m.hosts.Hosts[2].SSHBack = "ssh from server: last check ok 3m ago, no open connection"
	var checked, started []string
	m.sshCheck = func(alias string) string { checked = append(checked, alias); return "ok" }
	m.sshStart = func(alias string) tea.Cmd {
		started = append(started, alias)
		return func() tea.Msg { return hubMsg{"connection to " + alias + " open"} }
	}
	m.key("h")

	// This host and an unmapped one are refused.
	for !m.hosts.Hosts[m.hostSel].You {
		m.key("right")
	}
	m.key("c")
	requireAll(t, ansi.Strip(m.View()), "another host's SSH")
	for m.hosts.Hosts[m.hostSel].Name != "hubbox" {
		m.key("right")
	}
	requireAll(t, ansi.Strip(m.View()), "not set up (sunstack ssh setup hubbox)")
	m.key("S")
	requireAll(t, ansi.Strip(m.View()), "hubbox has no SSH alias")

	for m.hosts.Hosts[m.hostSel].Name != "server" {
		m.key("right")
	}
	requireAll(t, ansi.Strip(m.View()), "srv: no open connection", "last check ok 3m ago")
	if cmd := m.key("c"); cmd != nil {
		m.Update(cmd())
	}
	if strings.Join(checked, ",") != "srv" {
		t.Errorf("c checked %v", checked)
	}
	requireAll(t, ansi.Strip(m.View()), "ssh srv: ok")

	m.key("S")
	requireAll(t, ansi.Strip(m.View()), "Open a shared connection to srv?")
	if cmd := m.key("y"); cmd != nil {
		m.Update(cmd())
	}
	if strings.Join(started, ",") != "srv" {
		t.Errorf("S started %v", started)
	}
	m.key("S")
	m.key("n")
	if len(started) != 1 {
		t.Error("S started without a yes")
	}

	// A session on the mapped host shows how to attach over SSH.
	m.view = viewOrg
	selectRow(t, m, "builder.alice_api")
	requireAll(t, ansi.Strip(m.View()), `ssh -t srv 'tmux attach -t '\''=w'\'''`)
}

// Groups outside teams across hosts (§28.2): one keyed by a git remote is one
// row; one keyed by a folder is one row per host. The other hosts' items
// reach the Next tab, and a new model opens on project order.
func TestOrgOrdersAcrossHosts(t *testing.T) {
	m := hostsModel(t, 150)
	if newModel(nil, true).order != orderProject {
		t.Error("a new model does not open on project order")
	}
	m.org.Free = append(m.org.Free, &core.FreeGroup{Group: "/home/me/pr", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "local-pr"}}},
		&core.FreeGroup{Group: "/srv/git/shared", Kind: "remote", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "path-remote-l"}}},
		&core.FreeGroup{Group: "/work/x.git", Kind: "folder", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "dotgit-l"}}},
		// The same text as the server's remote below, but a folder here.
		&core.FreeGroup{Group: "/srv/git/mixed", Kind: "folder", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "mixed-folder"}}})
	// A team without a TEAM file here, at the same root as one elsewhere.
	m.org.Teams = append(m.org.Teams, &core.OrgTeam{ID: "/work/project", Name: "legacy-l", Root: "/work/project", Agents: []*core.OrgAgent{{ID: "builder.l"}}})
	for _, h := range m.hosts.Hosts {
		if h.Name == "server" {
			h.Org.Free = []*core.FreeGroup{
				{Group: "git@github.com:me/vault.git", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "notes"}}},
				{Group: "/home/me/pr", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "server-pr"}}},
				{Group: `C:\work\scratch`, Sessions: []*core.HostSession{{Tool: "codex", Status: "idle", Name: "win-a"}}},
			}
		}
		if h.Name == "hubbox" { // a Windows folder on another host: its own row
			h.Org = &core.Org{Host: core.HostInfo{Name: "hubbox"}, Free: []*core.FreeGroup{
				{Group: `C:\work\scratch`, Sessions: []*core.HostSession{{Tool: "codex", Status: "idle", Name: "win-b"}}},
			}, Teams: []*core.OrgTeam{ // shop, which this host lacks, on a second host: still one row
				{ID: "00000000000000aa", Name: "shop", Root: "/hub/shop", Agents: []*core.OrgAgent{{ID: "builder.alice", Sessions: []*core.HostSession{
					{Tool: "claude", Status: "idle", Agent: "builder.alice", Label: "builder.alice_hub@hubbox", Pane: "%7", Reach: "nudge"},
				}}}},
			}}
		}
	}
	// A third copy of shop, with an agent the first copy lacks, on two hosts.
	extra := &hub.HostView{ID: "eeeeeeeeeeeeeeee", Name: "zbox", State: "live", HasSnap: true, Org: &core.Org{Host: core.HostInfo{Name: "zbox"},
		Attention: []string{"shop: Z1 waits for you"},
		Teams: []*core.OrgTeam{{ID: "00000000000000aa", Name: "shop", Root: "/z/shop", Agents: []*core.OrgAgent{{ID: "ops.night", Sessions: []*core.HostSession{
			{Tool: "codex", Status: "idle", Agent: "ops.night", Label: "ops.night_z@zbox", Pane: "%8", Reach: "nudge"},
		}}, {ID: "hire.new"}}}}}} // hire.new: hired on zbox only, no session yet
	m.hosts.Hosts = append(m.hosts.Hosts, extra)
	// Two unrelated teams without a TEAM file at the same root, and sessions
	// whose folder is unknown, on two hosts: never merged.
	extra.Org.Teams = append(extra.Org.Teams, &core.OrgTeam{ID: "/work/project", Name: "legacy-z", Root: "/work/project", Agents: []*core.OrgAgent{{ID: "builder.z"}}})
	extra.Org.Free = []*core.FreeGroup{{Group: "(unknown folder)", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "lost-z"}}}}
	for _, h := range m.hosts.Hosts {
		if h.Name == "server" {
			h.Org.Teams[0].Agents = append(h.Org.Teams[0].Agents, &core.OrgAgent{ID: "ops.night", Sessions: []*core.HostSession{
				{Tool: "codex", Status: "idle", Agent: "ops.night", Label: "ops.night_s@server", Pane: "%9", Reach: "nudge"},
			}})
			h.Org.Teams = append(h.Org.Teams, &core.OrgTeam{ID: "/work/project", Name: "legacy-s", Root: "/work/project", Agents: []*core.OrgAgent{{ID: "builder.s"}}})
			h.Org.Free = append(h.Org.Free, &core.FreeGroup{Group: "(unknown folder)", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "lost-s"}}},
				&core.FreeGroup{Group: "/srv/git/shared", Kind: "remote", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "path-remote-s"}}},
				&core.FreeGroup{Group: "/work/x.git", Kind: "folder", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "dotgit-s"}}},
				&core.FreeGroup{Group: "/srv/git/mixed", Kind: "remote", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "mixed-remote"}}})
		}
	}
	var rows []string
	for _, n := range m.treeNodes() {
		rows = append(rows, n.label)
	}
	got := strings.Join(rows, "|")
	for _, want := range []string{"vault|todo @laptop|tnnls @laptop|notes @server", "pr @laptop|local-pr @laptop", "pr @server|server-pr @server",
		"scratch @server|win-a @server", "scratch @hubbox|win-b @hubbox",
		"shop  (only on hubbox, server, zbox)|builder.alice|builder.alice_hub @hubbox|builder.alice_api @server|ops.night|ops.night_s @server|ops.night_z @zbox|hire.new"} {
		if !strings.Contains(got, want) {
			t.Errorf("rows %q lack %q", got, want)
		}
	}
	for _, want := range []string{"legacy-l|builder.l|shop", "legacy-s  (only on server)|builder.s", "shared|path-remote-l @laptop|path-remote-s @server", "x @laptop|dotgit-l @laptop", "x @server|dotgit-s @server", "mixed @laptop|mixed-folder @laptop", "mixed|mixed-remote @server", "legacy-z  (only on zbox)|builder.z", "(unknown folder) @server|lost-s @server", "(unknown folder) @zbox|lost-z @zbox"} {
		if !strings.Contains(got, want) {
			t.Errorf("rows %q lack %q", got, want)
		}
	}
	if strings.Count(got, "vault") != 1 || strings.Count(got, "shop") != 1 {
		t.Errorf("a shared group or team is not one row: %q", got)
	}
	// The merged team's warnings: each host's own, the later ones tagged.
	selectRow(t, m, "shop")
	requireAll(t, ansi.Strip(m.View()), "Q1 waits for you (on server)", "Z1 waits for you (on zbox)")
	for _, n := range m.treeNodes() {
		if n.kind == "team" && strings.HasPrefix(n.label, "shop") {
			if c := len(m.teamWarnings(n)); c != 2 {
				t.Errorf("shop's row counts %d for you, want 2", c)
			}
		}
	}
	// The host view's Next (n): this host's items across teams, then the
	// other hosts'. Before a scan, no other host's items: nothing to compare.
	m.key("n")
	if m.view != viewNext {
		t.Fatal("n does not open Next in the host view")
	}
	var all []string
	for _, it := range m.nextItems {
		all = append(all, it.Text)
	}
	if a := strings.Join(all, "|"); !strings.Contains(a, "alpha: todo is waiting for you") || !strings.Contains(a, "shop: Q1 waits for you") {
		t.Errorf("host-view next: %q", a)
	}
	requireAll(t, ansi.Strip(m.View()), "shop: Q1 waits for you")
	saved := m.org
	m.org = nil
	if len(m.otherHostsNext()) != 0 {
		t.Error("other hosts' items listed before this host's scan")
	}
	m.org = saved
	var next []string
	for _, it := range m.otherHostsNext() {
		next = append(next, it.Text+" / "+it.Do)
	}
	if n := strings.Join(next, "|"); !strings.Contains(n, "shop: Q1 waits for you / on server") || strings.Contains(n, "todo is waiting") {
		t.Errorf("other hosts' next items: %q", n)
	}
}
