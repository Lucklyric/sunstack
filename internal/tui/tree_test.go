package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
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
	v := m.View()
	fits(t, v, m.w, "tree")
	requireAll(t, v, "4 sessions", "1 team", "Needs you (1)", "▾ alpha", "pm.lead", "pm.lead_cloud", "● busy", "◐ waiting", "outside teams")
	if n := strings.Count(v, "\n") + 1; n > m.h {
		t.Errorf("%d lines on a %d-line screen", n, m.h)
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
	if strings.Contains(m.View(), "show:") {
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
