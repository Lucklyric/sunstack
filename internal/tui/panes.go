package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
)

// pickTeam is one team on this host, for the start picker.
type pickTeam struct {
	Name, Root      string
	Agents          []string
	Sessions, Needs int
	Objectives      []string
}

// pickActions follow the teams in the picker's list.
type pickAction struct{ key, label, about string }

func (m *model) actions() []pickAction {
	out := []pickAction{{"o", "Host view (every team and session)", "Every team on this host with its work, what needs you, and every Claude Code and Codex session, in or outside a team."}}
	if m.canInit {
		out = append(out, pickAction{"n", "Set up a team in this folder", "Creates sunstack/ in " + m.cwd + " (PROTOCOL.md, PILLARS.md, BOARD.md, TEAM) and adds it to this host's teams. Asks first."})
	}
	return append(out, pickAction{"s", "Find teams under this folder", "Looks for sunstack/ teams under " + m.cwd + " and adds them to the list."})
}

// loadTeams reads this host's teams for the picker.
func (m *model) loadTeams() {
	m.teams = nil
	now := time.Now()
	for _, p := range core.IndexedProjects() {
		tf, _ := p.Team()
		t := pickTeam{Name: tf.Name, Root: p.Root}
		for _, st := range p.Status() {
			t.Agents = append(t.Agents, st.ID)
			t.Sessions += len(st.Claims)
		}
		for _, it := range p.Next("", now) {
			if it.Kind == "broken" || it.Kind == "user" {
				t.Needs++
			}
		}
		for _, it := range p.LoadBoards().Team {
			if it.Section == "Objectives" && !it.Done {
				t.Objectives = append(t.Objectives, it.Key+" "+it.Text)
			}
		}
		m.teams = append(m.teams, t)
	}
	sort.Slice(m.teams, func(i, j int) bool { return strings.ToLower(m.teams[i].Name) < strings.ToLower(m.teams[j].Name) })
	m.canInit = false
	if m.cwd != "" {
		if _, err := core.FindProject(m.cwd); err != nil {
			m.canInit = exec.Command("git", "-C", m.cwd, "rev-parse", "--is-inside-work-tree").Run() == nil
		}
	}
	if n := len(m.teams) + len(m.actions()); m.pickSel >= n {
		m.pickSel = max(0, n-1)
	}
}

func (m *model) openPicker() {
	m.view = viewPicker
	m.reload()
}

func (m *model) pickerKey(k string) tea.Cmd {
	n := len(m.teams) + len(m.actions())
	switch k {
	case "up", "k":
		m.pickSel = max(0, m.pickSel-1)
	case "down", "j":
		m.pickSel = min(n-1, m.pickSel+1)
	case "esc":
		if m.p != nil {
			m.view = viewTeam
		}
	case "enter":
		if m.pickSel < len(m.teams) {
			p, err := core.FindProject(m.teams[m.pickSel].Root)
			if err != nil {
				m.note = err.Error()
				return nil
			}
			m.p, m.teamSel, m.org = p, 0, nil
			m.view = viewTeam
			m.reload()
			m.orgBusy = true
			return orgCmd()
		}
		return m.pickerKey(m.actions()[m.pickSel-len(m.teams)].key)
	case "o":
		return m.show(viewOrg)
	case "s":
		found, err := core.ScanTeams(m.cwd)
		if err != nil {
			m.note = err.Error()
		} else {
			m.note = fmt.Sprintf("found %d team(s) under %s", len(found), filepath.Base(m.cwd))
		}
		m.reload()
	case "n":
		if m.canInit {
			m.confirm = "Create sunstack/ in " + filepath.Base(m.cwd) + " (" + m.cwd + ")? y to create, any other key to cancel"
			m.confirmDo = m.setUp
		}
	}
	return nil
}

// setUp creates a team in the current folder and opens it.
func (m *model) setUp() {
	if _, err := core.Init(m.cwd, false); err != nil {
		m.note = err.Error()
		return
	}
	p, err := core.FindProject(m.cwd)
	if err != nil {
		m.note = err.Error()
		return
	}
	_ = p.Register()
	m.p, m.teamSel = p, 0
	m.view = viewTeam
	m.note = "team created; hire a first agent with sunstack hire, or the recruit skill"
	m.reload()
}

// chrome is how many lines sit outside the body: the title, the tab bar and
// the help line.
func (m *model) chrome() int {
	h := 1 + lipgloss.Height(m.footer())
	if m.showTabs() {
		h++
	}
	return h
}

// inner is the content height of a body box.
func (m *model) inner() int { return max(4, m.h-m.chrome()-2) }

func (m *model) showTabs() bool {
	return m.p != nil && (m.view == viewTeam || m.view == viewNext || m.view == viewOrg || m.view == viewHosts || m.view == viewLog || m.view == viewInbox)
}

func (m *model) tabBar() string {
	var parts []string
	names := map[view]string{viewTeam: "Team", viewNext: "Next", viewOrg: "Org", viewHosts: "Hosts"}
	for _, v := range m.tabs() {
		label := " " + names[v] + " "
		if m.tabIndex(m.view) == m.tabIndex(v) {
			label = cSel.Render(label)
		} else {
			label = cDim.Render(label)
		}
		parts = append(parts, label)
	}
	return fit(strings.Join(parts, " ")+cDim.Render("   tab switch  t teams"), m.w)
}

// panes draws a selectable list on the left and the selected item's details
// on the right, both kept inside the screen.
func (m *model) panes(title string, left []string, sel int, right []string, scroll int) string {
	return m.panesH(title, left, sel, right, scroll, m.inner())
}

// panesH is panes at content height h.
func (m *model) panesH(title string, left []string, sel int, right []string, scroll int, h int) string {
	leftW := min(46, m.w/2)
	rightW := m.w - leftW - 4
	start := max(0, sel-h+2)
	end := min(len(left), start+h-1)
	if len(left) > h-1 {
		title += fmt.Sprintf(" (%d–%d/%d)", start+1, end, len(left))
	}
	rows := []string{fit(cHeader.Render(title), leftW-2)}
	for i := start; i < end; i++ {
		l := fit(left[i], leftW-2)
		if i == sel {
			l = cSel.Render(l + strings.Repeat(" ", max(0, leftW-2-lipgloss.Width(l))))
		}
		rows = append(rows, l)
	}
	l := cBox.Width(leftW).Height(h).Render(strings.Join(rows, "\n"))
	r := cBox.Width(rightW).Height(h).Render(detailView(right, rightW-2, scroll, h))
	return lipgloss.JoinHorizontal(lipgloss.Top, l, r)
}

func detailView(right []string, w, scroll, h int) string {
	var detail []string
	for _, r := range right {
		detail = append(detail, strings.Split(r, "\n")...)
	}
	if len(detail) <= h {
		return strings.Join(fitAll(detail, w), "\n")
	}
	count := max(1, h-1)
	start := min(max(0, scroll), max(0, len(detail)-count))
	end := min(len(detail), start+count)
	lines := fitAll(detail[start:end], w)
	lines = append(lines, fit(cDim.Render(fmt.Sprintf("pgup/pgdn  %d–%d of %d", start+1, end, len(detail))), w))
	return strings.Join(lines, "\n")
}

func detailField(label, value string, labelW, w int) string {
	lines := strings.Split(wrap(value, max(1, w-labelW-1)), "\n")
	for i := range lines {
		prefix := strings.Repeat(" ", labelW+1)
		if i == 0 {
			prefix = cDim.Render(fmt.Sprintf("%-*s", labelW, label)) + " "
		}
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *model) pickerView() string {
	var left []string
	for _, t := range m.teams {
		needs := ""
		if t.Needs > 0 {
			needs = "  " + cWarn.Render(fmt.Sprintf("%d for you", t.Needs))
		}
		tail := fmt.Sprintf("%d live%s", t.Sessions, needs)
		left = append(left, rowText(t.Name, tail, min(46, m.w/2)-2))
	}
	if len(m.teams) == 0 {
		left = append(left, cDim.Render("no teams yet"))
	}
	left = append(left, cDim.Render(strings.Repeat("─", 20)))
	for _, a := range m.actions() {
		left = append(left, a.key+"  "+a.label)
	}
	// Keep the selection on the real rows: teams, then actions after the rule.
	sel := m.pickSel
	if sel >= len(m.teams) {
		sel++
		if len(m.teams) == 0 {
			sel++
		}
	}
	rightW := m.w - min(46, m.w/2) - 6
	var right []string
	switch {
	case m.pickSel < len(m.teams):
		t := m.teams[m.pickSel]
		right = append(right, wrap(cHeader.Render(t.Name), rightW), cDim.Render(wrap(t.Root, rightW)), "",
			fmt.Sprintf("sessions: %d", t.Sessions),
			fmt.Sprintf("needs you: %d", t.Needs),
			"agents: "+strings.Join(t.Agents, ", "), "", cHeader.Render("Objectives"))
		if len(t.Objectives) == 0 {
			right = append(right, cDim.Render("none yet"))
		}
		for _, o := range t.Objectives {
			right = append(right, wrap(o, rightW))
		}
		right = append(right, "", cDim.Render("enter opens this team"))
	default:
		a := m.actions()[m.pickSel-len(m.teams)]
		right = append(right, cHeader.Render(a.label), "", wrap(a.about, rightW), "", cDim.Render("enter or "+a.key))
	}
	if m.confirm != "" {
		right = append(right, "", cWarn.Render(wrap(m.confirm, rightW)))
	}
	return m.panes("Teams on this host", left, sel, right, 0)
}

func (m *model) nextView() string {
	if m.p == nil {
		return ""
	}
	items := m.nextItems // read at reload, not on every frame
	if len(items) == 0 {
		return m.panes("Next", []string{"Nothing needs doing"}, -1, []string{"No problems, nothing waiting on you, no open work on the boards."}, 0)
	}
	if m.nextSel >= len(items) {
		m.nextSel = len(items) - 1
	}
	var left []string
	for i, it := range items {
		left = append(left, fmt.Sprintf("%d. [%s] %s", i+1, it.Kind, it.Text))
	}
	it := items[m.nextSel]
	rightW := m.w - min(46, m.w/2) - 6
	right := []string{cHeader.Render(fmt.Sprintf("%d. %s", m.nextSel+1, tierName(it.Kind))), "", wrap(it.Text, rightW), "",
		cHeader.Render("do: ") + wrap(it.Do, rightW-4), "", cDim.Render("owner: " + it.Owner)}
	return m.panes("Next, most urgent first", left, m.nextSel, right, m.nextScroll)
}

func tierName(k string) string {
	switch k {
	case "broken":
		return "Broken: fix this first"
	case "user":
		return "Waiting on you"
	case "blocked":
		return "Blocked work"
	case "drift":
		return "Drift: the owner fixes it at its next save"
	case "yours":
		return "This session's own next step"
	}
	return k
}

// orgPane is the Org tab: the session tree (tree.go).
func (m *model) orgPane() string { return m.treeView() }

func (m *model) helpView() string {
	lines := []string{cHeader.Render("Navigation"),
		"t  teams: pick a team on this host",
		"tab / shift+tab  next / previous tab",
		"n  next: ranked work    o  host view    h  hosts in an org",
		"↑↓ or j/k  select    pgup/pgdn  scroll details",
		"r  refresh    esc  back    ?  help    q  quit", "",
		cHeader.Render("Team and Org"),
		"←→  fold / unfold    enter  open pane or toggle group",
		"/  filter by name    esc  clear filter",
		"f  show all, needs you, busy, or outside tmux",
		"b  group by team / host (in an org)",
		"m  message    s  start a session from the selected row",
		"p  peek at a remote pane (host must allow peek)",
		"R  reopen in tmux    K  close session (both ask)",
		"l  log    i  inbox    g  agent's pane    c  copy resume",
		cHeader.Render("Hosts"),
		"↑↓←→  select    enter  sessions    u  update host    U  update all behind",
		"c  check SSH    S  open or close SSH    r  refresh    h  back"}
	return cBox.Width(m.w - 2).Height(m.inner()).Render(strings.Join(clip(fitAll(lines, m.w-4), m.inner()), "\n"))
}
