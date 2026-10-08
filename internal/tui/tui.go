// Package tui is the read-only team dashboard behind `sunstack tui`
// (design §6 TUI). Everything shown comes from sunstack/ and sunstack/_local/.
package tui

import (
	"encoding/base64"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

const refresh = 2 * time.Second

type view int

const (
	viewTeam view = iota
	viewLog
	viewInbox
	viewOrg
	viewNext
	viewPicker
	viewHosts
)

type tick time.Time

// orgMsg carries a finished org scan; the scan runs off the UI loop since it
// lists processes and reads transcripts.
type orgMsg struct{ o *core.Org }

const orgRefresh = 10 * time.Second

type model struct {
	p        *core.Project
	agents   []core.AgentStatus
	events   []string
	warnings []core.Check
	view     view
	w, h     int
	note     string
	updated  time.Time

	org       *core.Org
	orgAt     time.Time
	orgBusy   bool
	orgScroll int

	cwd        string // the folder sunstack was started in
	teams      []pickTeam
	pickSel    int
	canInit    bool   // cwd is a git project with no team: offer to set one up
	confirm    string // a question waiting for y
	help       bool
	nextScroll int
	nextSel    int
	nextItems  []core.Issue
	orgSel     int
	saved      savedState // what tui.json holds, so it is written only on a change
	teamSel    int        // the selected row of the Team tab's tree

	folded      map[string]bool // tree nodes the user folded
	filter      string
	filterMode  int    // index into filterModes
	byHost      bool   // the Org tab lists hosts, then their teams (b)
	inputPrompt string // a line being typed: a filter or a message
	inputText   string
	inputDone   func(string)
	confirmDo   func()
	sendTo      func(*core.HostSession, string) string
	kill        func(*core.HostSession) string
	reopen      func(*core.HostSession) string
	peek        func(*core.HostSession) string

	hosts      *hub.OrgView // the org this host belongs to, from the local files; nil outside an org
	hostSel    int
	loadView   func(time.Time) *hub.OrgView
	remoteSend func(to, text string) string
	refreshHub tea.Cmd
}

var (
	cTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cOn     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	cWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cSel    = lipgloss.NewStyle().Reverse(true)
	cBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	cHeader = lipgloss.NewStyle().Bold(true)
)

// Run starts the dashboard for project p, on the host (org) view when org
// is set. With no project (outside any team) it shows the host view only.
func Run(p *core.Project, org bool) error {
	m := startModel(p, org)
	m.cwd, _ = os.Getwd()
	m.reload()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// newModel opens on the team, or with org on the host view. Without a team
// it opens on the team picker, or with org on the host view.
func newModel(p *core.Project, org bool) *model {
	m := &model{p: p, folded: map[string]bool{}, sendTo: realSend, kill: realKill, reopen: realReopen, peek: realPeek,
		loadView: hub.LoadView, remoteSend: realRemoteSend, refreshHub: realRefresh}
	switch {
	case org:
		m.view, m.orgBusy = viewOrg, true
	case p == nil:
		m.view = viewPicker
	}
	return m
}

func (m *model) reload() {
	m.updated = time.Now()
	m.loadHosts()
	if m.view == viewPicker {
		m.loadTeams()
	}
	if m.p == nil {
		return
	}
	if m.view == viewNext {
		m.nextItems = m.p.Next("", time.Now())
	}
	m.agents = m.p.Status()
	m.events = m.p.Events("")
	m.warnings = nil
	for _, c := range m.p.Health() {
		if c.Level != "ok" && c.Area != "project" {
			m.warnings = append(m.warnings, c)
		}
	}
	m.updated = time.Now()
}

func tickCmd() tea.Cmd { return tea.Tick(refresh, func(t time.Time) tea.Msg { return tick(t) }) }

// scans says whether the view in front shows live sessions.
func (m *model) scans() bool { return m.view == viewOrg || (m.view == viewTeam && m.p != nil) }

func (m *model) Init() tea.Cmd {
	if m.scans() {
		m.orgBusy = true
		return tea.Batch(tickCmd(), orgCmd())
	}
	return tickCmd()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tick:
		m.reload()
		if m.scans() && !m.orgBusy && time.Since(m.orgAt) > orgRefresh {
			m.orgBusy = true
			return m, tea.Batch(tickCmd(), orgCmd())
		}
		return m, tickCmd()
	case orgMsg:
		m.org, m.orgAt, m.orgBusy = msg.o, time.Now(), false
	case hubMsg:
		m.note = msg.note
		m.loadHosts()
	case tea.KeyMsg:
		cmd := m.key(msg.String())
		m.saveState()
		return m, cmd
	}
	return m, nil
}

// key handles one key press.
func (m *model) key(k string) tea.Cmd {
	m.note = ""
	if m.inputPrompt != "" {
		// Typing a line: every key goes to it.
		switch k {
		case "enter":
			done, text := m.inputDone, m.inputText
			m.inputPrompt, m.inputText, m.inputDone = "", "", nil
			if done != nil {
				done(text)
			}
		case "esc":
			m.inputPrompt, m.inputText, m.inputDone = "", "", nil
		case "backspace":
			if r := []rune(m.inputText); len(r) > 0 {
				m.inputText = string(r[:len(r)-1])
			}
		case "space":
			m.inputText += " "
		default:
			if len([]rune(k)) == 1 {
				m.inputText += k
			}
		}
		return nil
	}
	if m.help {
		m.help = false // any key closes the help
		return nil
	}
	if k == "q" || k == "ctrl+c" {
		return tea.Quit
	}
	if m.confirm != "" {
		do := m.confirmDo
		m.confirm, m.confirmDo = "", nil
		if k == "y" && do != nil {
			do()
		}
		return nil
	}
	if k == "?" {
		m.help = true
		return nil
	}
	if m.view == viewPicker {
		return m.pickerKey(k)
	}
	if (m.view == viewOrg || (m.view == viewTeam && m.p != nil)) && m.treeKey(k) {
		return nil
	}
	if m.view == viewHosts {
		if ok, cmd := m.hostsKey(k); ok {
			return cmd
		}
	}
	if k == "h" && m.hosts != nil {
		return m.show(toggle(m.view, viewHosts))
	}
	if k == "t" {
		m.openPicker()
		return nil
	}
	if m.p == nil {
		// Host only: no team views to switch to.
		switch k {
		case "esc":
			m.openPicker()
			return nil
		case "o":
			return m.show(viewOrg)
		case "l", "i", "g", "c", "n", "tab", "shift+tab":
			return nil
		}
	}
	switch k {
	case "esc":
		m.view = viewTeam
	case "tab", "shift+tab":
		ts := m.tabs()
		step := 1
		if k == "shift+tab" {
			step = len(ts) - 1
		}
		return m.show(ts[(m.tabIndex(m.view)+step)%len(ts)])
	case "up", "k":
		// The trees take their own keys first; this is the Next list.
		if m.view == viewNext {
			m.nextSel, m.nextScroll = max(0, m.nextSel-1), 0
		}
	case "down", "j":
		if m.view == viewNext {
			m.nextSel++
			m.nextScroll = 0
		}
	case "o":
		return m.show(toggle(m.view, viewOrg))
	case "n":
		return m.show(toggle(m.view, viewNext))
	case "pgdown", "pgup":
		// Scroll the detail pane of the view in front.
		step := 10
		if k == "pgup" {
			step = -10
		}
		if m.view == viewNext {
			m.nextScroll = max(0, m.nextScroll+step)
		} else {
			m.orgScroll = max(0, m.orgScroll+step)
		}
	case "l":
		m.view = toggle(m.view, viewLog)
	case "i":
		m.view = toggle(m.view, viewInbox)
	case "r":
		m.reload()
		if m.scans() && !m.orgBusy {
			m.orgBusy = true
			return orgCmd()
		}
	case "g":
		m.note = m.goToPane()
	case "c":
		m.note = m.copyResume()
	}
	return nil
}

// tabs are the views a team shows in its tab bar; Hosts only in an org.
func (m *model) tabs() []view {
	if m.hosts != nil {
		return []view{viewTeam, viewNext, viewOrg, viewHosts}
	}
	return []view{viewTeam, viewNext, viewOrg}
}

func (m *model) tabIndex(v view) int {
	for i, t := range m.tabs() {
		if t == v {
			return i
		}
	}
	return 0 // the log and inbox belong to the team tab
}

// show switches to a view, starting what it needs.
func (m *model) show(v view) tea.Cmd {
	m.view = v
	switch v {
	case viewNext:
		m.nextScroll = 0
		m.reload()
	}
	if m.scans() && m.org == nil && !m.orgBusy {
		m.orgBusy = true
		return orgCmd()
	}
	return nil
}

func toggle(cur, v view) view {
	if cur == v {
		return viewTeam
	}
	return v
}

// current is the agent selected on the Team tab: an agent row, or the agent
// of a session row.
func (m *model) current() *core.AgentStatus {
	if len(m.agents) == 0 || m.p == nil {
		return nil
	}
	id := ""
	if nodes := m.teamNodes(); m.teamSel < len(nodes) {
		switch n := nodes[m.teamSel]; {
		case n.st != nil:
			id = n.st.ID
		case n.sess != nil:
			id = n.sess.Agent
		}
	}
	for i := range m.agents {
		if m.agents[i].ID == id {
			return &m.agents[i]
		}
	}
	return &m.agents[0]
}

// latest is the most recent session working as a, or -1.
func latest(a *core.AgentStatus) int { return len(a.Claims) - 1 }

// goToPane switches the tmux client to the pane of the selected agent's most
// recent session.
func (m *model) goToPane() string {
	a := m.current()
	if a == nil || latest(a) < 0 || a.Claims[latest(a)].TmuxPane == "" {
		return "no tmux pane recorded for this agent"
	}
	c, where := a.Claims[latest(a)], a.Where[latest(a)]
	if where == "pane closed" {
		return "that pane is gone"
	}
	for _, args := range [][]string{{"select-window", "-t", c.TmuxPane}, {"select-pane", "-t", c.TmuxPane}, {"switch-client", "-t", c.TmuxPane}} {
		if err := exec.Command("tmux", core.TmuxArgs(c.TmuxSocket, args...)...).Run(); err != nil {
			return "tmux: " + err.Error()
		}
	}
	return "switched to " + where
}

func resumeCmd(l *core.Live) string { return core.ResumeCommand(l) }

// copyResume puts the resume command on the clipboard through OSC 52, which
// works over SSH and inside tmux (with set-clipboard on).
func (m *model) copyResume() string {
	a := m.current()
	if a == nil {
		return ""
	}
	if latest(a) < 0 {
		return "no session recorded for this agent"
	}
	cmd := resumeCmd(a.Claims[latest(a)])
	if cmd == "" {
		return "no session recorded for this agent"
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(cmd)) + "\x07"
	if os.Getenv("TMUX") != "" {
		seq = "\x1bPtmux;\x1b" + seq + "\x1b\\"
	}
	fmt.Fprint(os.Stderr, seq)
	return "copied: " + cmd
}

func (m *model) View() string {
	if m.w == 0 {
		return "loading…"
	}
	where := ""
	if m.p != nil {
		where = m.p.Root
	} else if m.org != nil {
		where = "host " + m.org.Host.Name
	} else {
		where = "host " + core.ThisHost().Name
	}
	head := cTitle.Render("sunstack") + cDim.Render(fmt.Sprintf("  %s  ·  %s", where, m.updated.Format("15:04:05")))
	if m.view == viewOrg && m.org != nil {
		head += cDim.Render("  ·  ") + m.orgCounts()
	}
	var body string
	switch {
	case m.help:
		body = m.helpView()
	case m.view == viewPicker:
		body = m.pickerView()
	case m.view == viewNext:
		body = m.nextView()
	case m.view == viewLog:
		body = m.logView()
	case m.view == viewInbox:
		body = m.inboxView()
	case m.view == viewOrg:
		body = m.orgPane()
	case m.view == viewHosts:
		body = m.hostsView()
	default:
		body = m.teamView()
	}
	help := cDim.Render("↑↓ select · o org · l log · i inbox · g go to pane · c copy resume · r refresh · esc back · q quit")
	switch {
	case m.view == viewPicker:
		help = cDim.Render("↑↓ select · enter open · o host view · s find teams · n set up · ? help · q quit")
	case m.view == viewNext:
		help = cDim.Render("↑↓ select · r refresh · tab switch · t teams · esc back · ? help · q quit")
	case m.view == viewOrg:
		help = cDim.Render("↑↓ move · ←→ fold · enter go to pane · m message · / filter · f show · R reopen · K close · ? keys · q quit")
		if m.hosts != nil && !m.teamScope() {
			help = cDim.Render("↑↓ move · ←→ fold · enter go to pane · m message · / filter · f show · b by team/host · R reopen · K close · ? keys · q quit")
		}
	case m.view == viewHosts:
		help = cDim.Render("←→↑↓ choose a host · enter its sessions · r refresh · h back · ? keys · q quit")
	}
	if m.note != "" {
		help = cWarn.Render(m.note) + "  " + help
	}
	parts := []string{fit(head, m.w)}
	if m.showTabs() {
		parts = append(parts, m.tabBar())
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(parts, body, fit(help, m.w))...)
}

func (m *model) teamView() string {
	if len(m.agents) == 0 {
		return cBox.Width(m.w - 2).Render("No agents yet. Run sunstack hire <title> [name], or ask an agent to recruit one.")
	}
	// Bottom boxes first, so the two top boxes get exactly the rest.
	events := m.events
	if len(events) > 5 {
		events = events[len(events)-5:]
	}
	bottom := []string{cBox.Width(m.w - 4).Render(cHeader.Render("Events") + "\n" + strings.Join(fitAll(orNone(events), m.w-6), "\n"))}
	var warn []string
	for _, c := range m.warnings {
		warn = append(warn, cWarn.Render("⚠ ")+c.Msg)
	}
	if len(warn) > 3 {
		warn = append(warn[:3], cDim.Render(fmt.Sprintf("… %d more (sunstack health)", len(m.warnings)-3)))
	}
	if len(warn) > 0 {
		bottom = append(bottom, cBox.Width(m.w-4).Render(strings.Join(fitAll(warn, m.w-6), "\n")))
	}
	used := m.chrome() // title, tab bar and help lines
	for _, b := range bottom {
		used += lipgloss.Height(b)
	}
	bodyH := max(4, m.h-used-2) // minus the top boxes' borders
	top := m.treeBox(bodyH)
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{top}, bottom...)...)
}

// agentDetails is the right pane for an agent on the Team tab: its sessions,
// pillars, proposals and board.
func (m *model) agentDetails(a *core.AgentStatus, width int) []string {
	out := []string{cHeader.Render(a.ID), wrap(a.Duty, width-2), ""}
	for i, c := range a.Claims {
		out = append(out, fmt.Sprintf("session %s: %s on %s, last contact %s", c.Label(a.ID), c.Tool, c.Host, c.LastContact))
		if a.Where[i] != "" {
			out = append(out, "  tmux: "+a.Where[i])
		}
		if r := resumeCmd(c); r != "" {
			out = append(out, "  resume: "+r)
		}
	}
	if len(a.Claims) == 0 {
		out = append(out, cDim.Render("free"))
	}
	out = append(out, "", cHeader.Render("Pillars"))
	if pl, err := m.p.Pillars(a.ID); err == nil {
		out = append(out, strings.Split(strings.TrimRight(pl, "\n"), "\n")...)
	}
	if len(a.Proposals) > 0 {
		out = append(out, "", cHeader.Render("Proposals (waiting for your approval)"))
		out = append(out, a.Proposals...)
	}
	board, _ := os.ReadFile(filepath.Join(m.p.AgentDir(a.ID), "board.md"))
	if now := sectionLines(board, "Now"); len(now) > 0 {
		out = append(out, "", cHeader.Render("Now"))
		out = append(out, now...)
	}
	if nx := sectionLines(board, "Next"); len(nx) > 0 {
		out = append(out, "", cHeader.Render("Next"))
		out = append(out, nx...)
	}
	if len(a.Threads) > 0 {
		out = append(out, "", cHeader.Render("Threads"), strings.Join(a.Threads, ", "))
	}
	if a.ContextLines > 200 {
		out = append(out, "", cWarn.Render(fmt.Sprintf("context.md has %d lines; compact it", a.ContextLines)))
	}
	return out
}

func (m *model) logView() string {
	a := m.current()
	id, label := "", "all agents"
	if a != nil {
		id, label = a.ID, a.ID
	}
	lines := m.p.Events(id)
	h := m.inner()
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return cBox.Width(m.w - 4).Height(h).Render(cHeader.Render("Events · "+label) + "\n" + strings.Join(fitAll(orNone(lines), m.w-6), "\n"))
}

func (m *model) inboxView() string {
	a := m.current()
	if a == nil {
		return ""
	}
	var lines []string
	for _, e := range m.p.Inbox(a.ID) {
		lines = append(lines, fmt.Sprintf("%s  %-8s from %-16s %s", e.At, e.Type, e.From, e.File))
	}
	return cBox.Width(m.w - 4).Height(m.inner()).Render(cHeader.Render("Inbox · "+a.ID) + "\n" + strings.Join(fitAll(orNone(lines), m.w-6), "\n"))
}

func sectionLines(doc []byte, name string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(string(doc), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			in = t == "## "+name
			continue
		}
		if in && t != "" {
			out = append(out, t)
		}
	}
	return out
}

func clip(lines []string, n int) []string {
	if len(lines) > n {
		return append(lines[:n-1], "…")
	}
	return lines
}

func orNone(lines []string) []string {
	if len(lines) == 0 {
		return []string{cDim.Render("(none yet)")}
	}
	return lines
}

func wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

func orgCmd() tea.Cmd { return func() tea.Msg { return orgMsg{core.BuildOrg()} } }

// fit cuts a line to w display columns, so a box never wraps it: wide
// characters count twice, and color codes not at all.
func fit(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func fitAll(lines []string, w int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fit(l, w)
	}
	return out
}
