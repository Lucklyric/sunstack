package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/charmbracelet/x/ansi"
)

// treeOrg is a host with one team (an agent with a busy session, a free
// session) and two sessions outside teams.
func treeOrg() *core.Org {
	return &core.Org{
		Host:      core.HostInfo{Name: "h"},
		Attention: []string{"alpha: todo is waiting for you"},
		Teams: []*core.OrgTeam{{
			ID: "00000000000000a1", Name: "alpha", Root: "/r/alpha",
			Objectives: []core.OrgObjective{{Key: "O1", Text: "Ship it"}},
			Agents: []*core.OrgAgent{
				{ID: "pm.lead", Duty: "Plans the work", Now: []string{"KR1 Plan"}, Sessions: []*core.HostSession{
					{Tool: "claude", Status: "busy", Agent: "pm.lead", Label: "pm.lead_cloud@h", Doing: "demo round", Where: "w:0.0", Pane: "%9", Reach: "nudge", TeamRoot: "/r/alpha"},
				}},
				{ID: "marketing.social", Duty: "Posts"},
			},
			Free: []*core.HostSession{{Tool: "codex", Status: "idle", Name: "scratch", Reach: "nudge", Pane: "%10", Where: "w:0.1"}},
		}},
		Free: []*core.FreeGroup{{Group: "git@github.com:me/vault.git", Sessions: []*core.HostSession{
			{Tool: "claude", Status: "waiting", Name: "todo", SessionID: "00000000-0000-4000-8000-000000000001", Reach: "next prompt",
				Activity: core.Activity{Title: "Daily todos", LastPrompt: "reply the email", LastReply: "Draft saved."}},
			{Tool: "claude", Status: "idle", Name: "tnnls", SessionID: "00000000-0000-4000-8000-000000000002", Reach: "next prompt"},
		}}},
	}
}

func treeModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	m := newModel(nil, true)
	m.w, m.h = 120, 32
	m.org, m.orgBusy = treeOrg(), false
	m.peek = func(*core.HostSession) string { return "" }
	m.isFree = func(s *core.HostSession) bool { return s.Name == "codex-1" }
	return m
}

// selectRow moves the selection to the first row containing s.
func selectRow(t *testing.T, m *model, s string) {
	t.Helper()
	for i, n := range m.treeNodes() {
		if strings.Contains(n.label, s) {
			*m.treeSel() = i
			return
		}
	}
	t.Fatalf("no row %q in %v", s, m.treeNodes())
}

func TestTreeShowsHostTeamAgentSession(t *testing.T) {
	m := treeModel(t)
	m.showAll = true // the full roster, closed sessions included
	v := m.View()
	fits(t, v, m.w, "tree")
	requireAll(t, v, "4 sessions", "1 team", "1 for you", "▾ alpha", "pm.lead", "pm.lead_cloud", "● busy", "◐ waiting", "▾ vault")
	if n := strings.Count(v, "\n") + 1; n > m.h {
		t.Errorf("%d lines on a %d-line screen", n, m.h)
	}
	// One kind per level (§28): no needs row; by project the group sits
	// beside the team, by team under "no team"; one host has no host order.
	if strings.Contains(v, "Needs you (") || strings.Contains(v, "outside teams") {
		t.Errorf("project order:\n%s", v)
	}
	requireAll(t, v, "by project ▾", "all sessions ▾", "show: all ▾", "filter ▾")
	// A group or "no team" row counts its sessions that need the user.
	for _, n := range m.treeNodes() {
		if n.kind == "group" && n.need != 1 {
			t.Errorf("vault counts %d for you, want 1 (todo waits)", n.need)
		}
	}
	m.key("b")
	requireAll(t, m.View(), "by team ▾", "▾ no team", "vault")
	for _, n := range m.treeNodes() {
		if n.kind == "outside" && n.need != 1 {
			t.Errorf("no team counts %d for you, want 1", n.need)
		}
	}
	m.key("b") // by folder (§40.6)
	m.key("b")
	if strings.Contains(m.View(), "no team") {
		t.Error("b on one host does not cycle back to project order")
	}
	// A session's details on the right.
	selectRow(t, m, "pm.lead_cloud")
	requireAll(t, m.View(), "doing", "demo round", "w:0.0 %9", "reach")
	selectRow(t, m, "todo")
	requireAll(t, m.View(), "Daily todos", "reply the email", "Draft saved.", "outside tmux")
	// An agent's details.
	selectRow(t, m, "marketing.social")
	requireAll(t, m.View(), "Posts", "no live session")
	// A team's details.
	selectRow(t, m, "alpha")
	requireAll(t, m.View(), "O1 Ship it")
}

func TestLongTreeNamesKeepStatus(t *testing.T) {
	m := treeModel(t)
	for _, status := range []string{"busy", "waiting", "blocked", "idle", "pane gone", "not seen"} {
		s := &core.HostSession{Status: status, Tool: "claude"}
		n := treeNode{kind: "session", depth: 2, label: strings.Repeat("很长的名字", 10), sess: s}
		row := ansi.Strip(m.row(n, 38))
		fits(t, row, 38, status)
		if !strings.Contains(row, "…") || !strings.HasSuffix(row, status) {
			t.Errorf("long name hid the status: %q", row)
		}
	}
}

func TestTreeFoldsAndFilters(t *testing.T) {
	m := treeModel(t)
	selectRow(t, m, "alpha")
	m.Update(key("left"))
	if strings.Contains(m.View(), "pm.lead_cloud") {
		t.Error("left folds the team")
	}
	requireAll(t, m.View(), "▸ alpha")
	m.Update(key("right"))
	requireAll(t, m.View(), "pm.lead_cloud")

	// / filters by name as you type; esc clears it.
	m.Update(key("/"))
	for _, r := range "tnn" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	v := m.View()
	requireAll(t, v, "tnnls", "filter: tnn")
	if strings.Contains(v, "pm.lead_cloud") || strings.Contains(v, "todo") {
		t.Errorf("filter shows only matches:\n%s", v)
	}
	m.Update(key("esc"))
	requireAll(t, m.View(), "pm.lead_cloud", "todo")

	// f cycles: needs you shows the waiting session only.
	m.Update(key("f"))
	v = m.View()
	requireAll(t, v, "todo", "show: needs you")
	if strings.Contains(v, "pm.lead_cloud") || strings.Contains(v, "tnnls") {
		t.Errorf("needs-you filter:\n%s", v)
	}
	for i := 0; i < 3; i++ {
		m.Update(key("f"))
	}
	if !strings.Contains(m.View(), "show: all ▾") {
		t.Error("f comes back to all")
	}
}

func TestTreeActions(t *testing.T) {
	m := treeModel(t)
	var sent, killed, reopened string
	m.sendTo = func(s *core.HostSession, text string) string { sent = s.Name + s.Label + ": " + text; return "sent" }
	m.kill = func(s *core.HostSession) string { killed = s.Label; return "closed" }
	m.reopen = func(s *core.HostSession) string { reopened = s.SessionID; return "reopened" }

	selectRow(t, m, "todo")
	m.Update(key("m"))
	requireAll(t, m.View(), "message to todo")
	for _, r := range "hi" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if sent != "todo: hi" {
		t.Errorf("m sends to the session: %q", sent)
	}

	// R and K ask first; only y acts.
	m.Update(key("R"))
	requireAll(t, m.View(), "Reopen todo in a tmux pane")
	m.Update(key("n"))
	if reopened != "" {
		t.Error("anything but y cancels")
	}
	m.Update(key("R"))
	m.Update(key("y"))
	if reopened != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("R reopens: %q", reopened)
	}
	selectRow(t, m, "pm.lead_cloud")
	m.Update(key("K"))
	requireAll(t, m.View(), "Close pm.lead_cloud")
	m.Update(key("y"))
	if killed != "pm.lead_cloud@h" {
		t.Errorf("K kills: %q", killed)
	}
	// K on a session that holds no agent is refused, without asking,
	// unless spawn --free started it.
	selectRow(t, m, "scratch")
	m.Update(key("K"))
	requireAll(t, m.View(), "only an agent's session or a free session")
	m.org.Teams[0].Free[0].Name = "codex-1"
	selectRow(t, m, "codex-1")
	m.Update(key("K"))
	requireAll(t, m.View(), "Close codex-1")
	m.note = ""
	m.Update(key("y"))
	if m.note != "closed" {
		t.Errorf("K on a free session: note %q", m.note)
	}
}

// teamTreeModel is a team (two agents, one claimed from a pane that is gone)
// on a host that also has another team and sessions outside teams.
func teamTreeModel(t *testing.T) (*model, *core.Project) {
	t.Helper()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	root := t.TempDir()
	p := newTeam(t, root, "builder.a")
	newTeam(t, root, "reviewer.r")
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(root, "sunstack", rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("_local/live/builder.a/aaaaaaaaaaaaaaaa.json", `{"tool":"claude","host":"h","token":"aaaaaaaaaaaaaaaa","claimed":"2026-10-01T00:00:00Z","last_contact":"2026-10-01T00:00:00Z","task":"x","tmux_pane":"%7","tmux_socket":"`+filepath.Join(root, "gone.sock")+`","tmux_server":"1"}`)
	write("builder.a/pillars.md", "- 2026-10-01 test every change\n")
	m := newModel(p, false)
	m.w, m.h = 120, 34
	m.peek = func(*core.HostSession) string { return "" }
	m.reload()
	return m, p
}

// The Team tab is the session tree of this team only.
func TestTeamTabIsTeamTree(t *testing.T) {
	m, p := teamTreeModel(t)
	// Before the scan: agents, and sessions from their claims. Without tmux
	// (Windows CI) a claim's pane cannot be checked, so it is not shown gone.
	v := m.View()
	fits(t, v, m.w, "team before scan")
	requireAll(t, v, "builder.a", "reviewer.r", "no session", "Events")
	if _, err := exec.LookPath("tmux"); err == nil {
		requireAll(t, v, "builder.a_x", "pane gone")
	}
	if n := strings.Count(v, "\n") + 1; n > m.h {
		t.Errorf("%d lines on a %d-line screen", n, m.h)
	}

	// After the scan: live state, and nothing from other teams.
	m.org = &core.Org{Host: core.HostInfo{Name: "h"}, Attention: []string{"t1: builder.a#KR1 is waiting on the user", "other: something"},
		Teams: []*core.OrgTeam{
			{Name: "t1", Root: p.Root, Agents: []*core.OrgAgent{
				{ID: "builder.a", Sessions: []*core.HostSession{{Tool: "claude", Status: "busy", Agent: "builder.a", Label: "builder.a_x@h", Doing: "writing tests", Pane: "%7", Where: "w:1.0", Reach: "nudge"}}},
				{ID: "reviewer.r"},
			}, Free: []*core.HostSession{{Tool: "codex", Status: "idle", Name: "scratch"}}},
			{Name: "other", Root: "/elsewhere", Agents: []*core.OrgAgent{{ID: "pm.lead", Sessions: []*core.HostSession{{Tool: "claude", Status: "busy", Label: "pm.lead_z@h"}}}}},
		},
		Free: []*core.FreeGroup{{Group: "repo", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "todo"}}}}}
	m.orgBusy = false
	v = m.View()
	fits(t, v, m.w, "team after scan")
	requireAll(t, v, "Needs you (1)", "builder.a_x", "● busy", "free sessions (1)", "scratch")
	for _, other := range []string{"pm.lead", "todo", "other: something"} {
		if strings.Contains(v, other) {
			t.Errorf("the team tab shows %q from outside the team:\n%s", other, v)
		}
	}

	// An agent shows its pillars and board; a session its details.
	selectRow(t, m, "builder.a")
	requireAll(t, m.View(), "Pillars", "test every change")
	if a := m.current(); a == nil || a.ID != "builder.a" {
		t.Errorf("the selected agent is builder.a, got %v", a)
	}
	selectRow(t, m, "builder.a_x")
	requireAll(t, m.View(), "writing tests", "w:1.0 %7")
	if a := m.current(); a == nil || a.ID != "builder.a" {
		t.Errorf("a session's agent is the current agent for l and i, got %v", a)
	}
	selectRow(t, m, "reviewer.r")
	m.Update(key("l"))
	if m.view != viewLog {
		t.Error("l opens the selected agent's log")
	}
	m.Update(key("l"))

	// Folding and filtering work here too.
	selectRow(t, m, "builder.a")
	m.Update(key("left"))
	requireAll(t, m.View(), "▸ builder.a")
	m.Update(key("right"))
	requireAll(t, m.View(), "▾ builder.a")
	m.Update(key("f"))
	if v := m.View(); strings.Contains(v, "builder.a_x") || !strings.Contains(v, "show: needs you") {
		t.Errorf("needs-you filter in the team tab:\n%s", v)
	}
}

// With a team open, Next still lists this host's items outside every team,
// which the team's own list does not hold (§28.1).
func TestTeamNextKeepsOutsideItems(t *testing.T) {
	m, p := teamTreeModel(t)
	m.org = &core.Org{Host: core.HostInfo{Name: "h"},
		Teams: []*core.OrgTeam{{ID: "00000000000000a1", Name: "alpha"}, {ID: "00000000000000b2", Name: "mine", Root: p.Root}},
		Attention: []string{"alpha: todo is waiting for you", "claude session todo waits for you (in /x)",
			"mine: builder.a_x is waiting for you (answer it in its pane)"}}
	var got []string
	for _, it := range m.hostNext() {
		got = append(got, it.Text)
	}
	g := strings.Join(got, "|")
	if !strings.Contains(g, "claude session todo waits for you") || strings.Contains(g, "alpha: todo is waiting") ||
		strings.Count(g, "builder.a_x is waiting for you") != 1 {
		t.Errorf("team next: %q", g)
	}
}

// In host order a folder and a remote group with the same text fold apart.
func TestHostOrderGroupKeys(t *testing.T) {
	m := treeModel(t)
	m.org.Free = append(m.org.Free,
		&core.FreeGroup{Group: "/srv/git/shared", Kind: "folder", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "f1"}}},
		&core.FreeGroup{Group: "/srv/git/shared", Kind: "remote", Sessions: []*core.HostSession{{Tool: "claude", Status: "idle", Name: "r1"}}})
	keys := map[string]bool{}
	for _, n := range m.outsideRows(m.org, "host:x/", 1, nil) {
		if n.kind == "group" && n.label == "shared" {
			if keys[n.key] {
				t.Fatalf("two groups share the fold key %q", n.key)
			}
			keys[n.key] = true
		}
	}
	if len(keys) != 2 {
		t.Errorf("groups: %v", keys)
	}
}

// The org tree shows running sessions only until a shows the rest; the Team
// tab keeps its whole roster.
func TestOrgTreeShowsRunningSessionsByDefault(t *testing.T) {
	m := treeModel(t)
	o := treeOrg()
	o.Teams[0].Agents = append(o.Teams[0].Agents, &core.OrgAgent{ID: "builder.gone", Sessions: []*core.HostSession{
		{Tool: "claude", Status: "not seen", Agent: "builder.gone", Label: "builder.gone_x@h", Pane: "%11", TeamRoot: "/r/alpha"},
	}})
	m.org = o
	labels := func() string {
		var out []string
		for _, n := range m.treeNodes() {
			out = append(out, n.label)
		}
		return strings.Join(out, "|")
	}
	got := labels()
	for _, gone := range []string{"marketing.social", "builder.gone"} {
		if strings.Contains(got, gone) {
			t.Errorf("by default %s is hidden: %s", gone, got)
		}
	}
	requireAll(t, got, "pm.lead_cloud", "scratch", "todo")
	requireAll(t, m.View(), "running only", "a all")
	m.key("a")
	requireAll(t, labels(), "marketing.social", "builder.gone_x")
	requireAll(t, m.View(), "a running only")
	m.key("a")
	if strings.Contains(labels(), "builder.gone") {
		t.Error("a again hides closed sessions")
	}
}

// One column: --compact or a narrow screen shows the list alone, and d
// swaps in the selected session's details, host included.
func TestCompactListAndDetails(t *testing.T) {
	m := treeModel(t)
	m.compact = true
	selectRow(t, m, "pm.lead_cloud")
	v := m.View()
	fits(t, v, m.w, "compact list")
	requireAll(t, v, "pm.lead_cloud", "demo round", "claude tmux", "busy", "d details")
	if strings.Contains(v, "reach") {
		t.Error("the compact list shows no details pane")
	}
	m.key("d")
	requireAll(t, m.View(), "host", "(this host)", "w:0.0 %9", "reach", "d or esc list")
	m.key("esc")
	requireAll(t, m.View(), "d details")
	m.compact, m.w = false, 80 // narrow without the flag
	requireAll(t, m.View(), "d details")
}

// The mouse: the wheel moves the selection and a click selects a row.
func TestMouseSelects(t *testing.T) {
	m := treeModel(t)
	m.View()
	press := func(b tea.MouseButton, x, y int) {
		m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
		m.View()
	}
	press(tea.MouseButtonWheelDown, 5, 5)
	if *m.treeSel() != 1 {
		t.Fatalf("wheel down selects row 1, got %d", *m.treeSel())
	}
	press(tea.MouseButtonWheelUp, 5, 5)
	if *m.treeSel() != 0 {
		t.Fatalf("wheel up selects row 0, got %d", *m.treeSel())
	}
	// No tab bar outside a team: the list's rows start at line 3.
	press(tea.MouseButtonLeft, 5, m.listTop()+2)
	if *m.treeSel() != 2 {
		t.Errorf("a click on the third row selects it, got %d", *m.treeSel())
	}
}

// By folder: home-relative folders, one-child chains as one row, teams
// named at their root, sessions under the folder they run in.
func TestFolderOrder(t *testing.T) {
	m := treeModel(t)
	o := treeOrg()
	o.Teams[0].Root = "/Users/me/codes/alpha"
	o.Teams[0].Agents[0].Sessions[0].Cwd = "/Users/me/codes/alpha/web"
	o.Free[0].Sessions[0].Cwd = "/Users/me/codes/vault"
	o.Free[0].Sessions[1].Cwd = "/home/me/codes/vault/notes"
	m.org = o
	m.order = orderFolder
	var rows []string
	for _, n := range m.treeNodes() {
		rows = append(rows, fmt.Sprintf("%d %s", n.depth, n.label))
	}
	got := strings.Join(rows, "|")
	want := "0 ~/codes|1 alpha  (alpha)|2 scratch|2 web|3 pm.lead_cloud|1 vault|2 todo|2 notes|3 tnnls"
	if got != want {
		t.Errorf("by folder:\n got %s\nwant %s", got, want)
	}
	requireAll(t, m.View(), "by folder ▾")
	m.key("b")
	if m.order != orderProject {
		t.Errorf("b after folder goes to project, got %d", m.order)
	}
}

// The actions menu: "." or a right click opens it with the row's actions,
// enter or a click runs one, and the title's options are clickable.
func TestActionsMenuAndTitle(t *testing.T) {
	m := treeModel(t)
	var renamed string
	m.rename = func(s *core.HostSession, name string) error {
		if s.Pane == "" {
			return fmt.Errorf("it has no tmux pane")
		}
		renamed = sessionName(s) + "=" + name
		return nil
	}
	selectRow(t, m, "pm.lead_cloud")
	m.View()
	m.key(".")
	if m.menu == nil {
		t.Fatal(". opens the menu")
	}
	v := m.View()
	requireAll(t, v, "Go to its pane", "Rename…", "Message…", "Order by", "All sessions")
	requireAll(t, v, "Close…") // an agent's session: Sunstack started it
	m.key("esc")
	selectRow(t, m, "scratch")
	m.key(".")
	if strings.Contains(m.View(), "Close…") {
		t.Error("close is only for sessions Sunstack started")
	}
	m.key("esc")
	selectRow(t, m, "pm.lead_cloud")
	m.key(".")
	fits(t, v, m.w, "menu")
	m.key("esc")
	if m.menu != nil {
		t.Fatal("esc closes the menu")
	}
	// Rename from the menu: down to Rename, enter, type, enter.
	m.key(".")
	m.key("down")
	m.key("enter")
	for _, r := range "web" {
		m.key(string(r))
	}
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			t.Fatal("the rename runs as a command")
		}
		m.Update(cmd())
	}
	run(m.key("enter"))
	if renamed != "pm.lead_cloud=web" {
		t.Errorf("rename: %q, note %q", renamed, m.note)
	}
	selectRow(t, m, "todo") // no pane: it cannot be typed or gone to
	m.key("N")
	m.key("x")
	run(m.key("enter"))
	if !strings.Contains(m.note, "could not rename") {
		t.Errorf("rename without a pane: %q", m.note)
	}
	// A right click on a row selects it and opens the menu there.
	m.View()
	top := m.listTop()
	m.Update(tea.MouseMsg{X: 6, Y: top, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if m.menu == nil || *m.treeSel() != 0 {
		t.Fatalf("right click: menu %v, sel %d", m.menu != nil, *m.treeSel())
	}
	m.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.menu != nil {
		t.Fatal("a click outside closes the menu")
	}
	// The chips under the title: a click opens the choices, a click on one
	// sets it.
	m.View()
	var at titleSpan
	for _, sp := range m.titleX {
		if sp.chip == 1 {
			at = sp
		}
	}
	m.Update(tea.MouseMsg{X: at.x0 + 3, Y: m.chipY + at.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.menu == nil {
		t.Fatal("a click on a chip opens its choices")
	}
	requireAll(t, m.View(), "● running only", "all sessions")
	mu := m.menu
	m.Update(tea.MouseMsg{X: mu.x + 2, Y: mu.y + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !m.showAll || m.menu != nil {
		t.Errorf("choosing all sessions: showAll %t, menu open %t", m.showAll, m.menu != nil)
	}
	requireAll(t, m.View(), "all sessions ▾")
}

// Review fixes: absolute folders keep their /, a collapsed chain keeps the
// team rooted along it, and a click selects the row under it directly.
func TestFolderEdgesAndClickSelect(t *testing.T) {
	m := treeModel(t)
	o := treeOrg()
	o.Teams[0].Root = "/opt/proj"
	o.Teams[0].Agents[0].Sessions[0].Cwd = "/opt/proj/sub"
	o.Teams[0].Free = nil
	o.Free = nil
	m.org, m.order = o, orderFolder
	var rows []string
	for _, n := range m.treeNodes() {
		rows = append(rows, n.label)
	}
	if got := strings.Join(rows, "|"); got != "/opt/proj/sub  (alpha)|pm.lead_cloud" {
		t.Errorf("rows %q", got)
	}
	// A wide screen after d in one column: clicks still select.
	m.order, m.org = orderProject, treeOrg()
	m.compact, m.detail = false, true
	m.View()
	m.Update(tea.MouseMsg{X: 5, Y: m.listTop() + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if *m.treeSel() != 2 {
		t.Errorf("click with detail left set: sel %d", *m.treeSel())
	}
}
