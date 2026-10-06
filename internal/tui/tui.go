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
	sel      int
	offset   int // first visible row of the team list
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
	m := &model{p: p}
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
	if m.sel >= len(m.agents) {
		m.sel = max(0, len(m.agents)-1)
	}
	m.updated = time.Now()
}

func tickCmd() tea.Cmd { return tea.Tick(refresh, func(t time.Time) tea.Msg { return tick(t) }) }

func (m *model) Init() tea.Cmd {
	if m.view == viewOrg {
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
		if m.view == viewOrg && !m.orgBusy && time.Since(m.orgAt) > orgRefresh {
			m.orgBusy = true
			return m, tea.Batch(tickCmd(), orgCmd())
		}
		return m, tickCmd()
	case orgMsg:
		m.org, m.orgAt, m.orgBusy = msg.o, time.Now(), false
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
	if m.help {
		m.help = false // any key closes the help
		return nil
	}
	if k == "q" || k == "ctrl+c" {
		return tea.Quit
	}
	if m.confirm != "" {
		m.confirm = ""
		if k == "y" {
			m.setUp()
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
		case "o", "l", "i", "g", "c", "n", "tab", "shift+tab":
			return nil
		}
	}
	switch k {
	case "esc":
		m.view = viewTeam
	case "tab", "shift+tab":
		step := 1
		if k == "shift+tab" {
			step = len(tabs) - 1
		}
		return m.show(tabs[(tabIndex(m.view)+step)%len(tabs)])
	case "up", "k":
		switch m.view {
		case viewOrg:
			m.orgSel, m.orgScroll = max(0, m.orgSel-1), 0
		case viewNext:
			m.nextSel, m.nextScroll = max(0, m.nextSel-1), 0
		default:
			if m.sel > 0 {
				m.sel--
			}
		}
	case "down", "j":
		switch m.view {
		case viewOrg:
			m.orgSel++
			m.orgScroll = 0
		case viewNext:
			m.nextSel++
			m.nextScroll = 0
		default:
			if m.sel < len(m.agents)-1 {
				m.sel++
			}
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
		if m.view == viewOrg && !m.orgBusy {
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

// tabs are the views a team shows in its tab bar.
var tabs = []view{viewTeam, viewNext, viewOrg}

func tabIndex(v view) int {
	for i, t := range tabs {
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
	case viewOrg:
		if m.org == nil && !m.orgBusy {
			m.orgBusy = true
			return orgCmd()
		}
	}
	return nil
}

func toggle(cur, v view) view {
	if cur == v {
		return viewTeam
	}
	return v
}

func (m *model) current() *core.AgentStatus {
	if len(m.agents) == 0 {
		return nil
	}
	return &m.agents[m.sel]
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
	} else {
		where = "host " + core.ThisHost().Name
	}
	head := cTitle.Render("sunstack") + cDim.Render(fmt.Sprintf("  %s  ·  %s", where, m.updated.Format("15:04:05")))
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
	default:
		body = m.teamView()
	}
	help := cDim.Render("↑↓ select · o org · l log · i inbox · g go to pane · c copy resume · r refresh · esc back · q quit")
	switch {
	case m.view == viewPicker:
		help = cDim.Render("↑↓ select · enter open · o host view · s find teams · n set up · ? help · q quit")
	case m.view == viewNext:
		help = cDim.Render("↑↓ select · r refresh · tab switch · t teams · esc back · ? help · q quit")
	case m.view == viewOrg && m.p == nil:
		help = cDim.Render("↑↓ select · pgup/pgdn scroll · r refresh · esc teams · ? help · q quit")
	case m.view == viewOrg:
		help = cDim.Render("↑↓ select · pgup/pgdn scroll · r refresh · tab switch · t teams · esc back · ? help · q quit")
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
	leftW := min(46, m.w/2)
	rightW := m.w - leftW - 4

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

	var rows []string
	selRow := 0
	title := ""
	for i, a := range m.agents {
		if a.Title != title {
			title = a.Title
			rows = append(rows, cHeader.Render(title))
		}
		dot, state := cDim.Render("○"), cDim.Render("free")
		if a.ClaimErr != nil {
			dot, state = cWarn.Render("?"), cWarn.Render("claim file damaged")
		} else if n := len(a.Claims); n > 0 {
			dot = cOn.Render("●")
			state = "1 session"
			if n > 1 {
				state = fmt.Sprintf("%d sessions", n)
			}
			for _, w := range a.Where {
				if w == "pane closed" {
					state += ", " + cWarn.Render("pane gone")
					break
				}
			}
		}
		line := fit(fmt.Sprintf("%s %-20s %s", dot, a.ID, state), leftW-2)
		if i == m.sel {
			line = cSel.Render(line)
			selRow = len(rows)
		}
		rows = append(rows, line)
		rows = append(rows, fit(cDim.Render(fmt.Sprintf("    inbox %d · proposals %d · threads %d", a.Inbox, len(a.Proposals), len(a.Threads))), leftW-2))
	}
	// Scroll so the selected agent (its line and the summary under it) stays visible.
	if selRow < m.offset {
		m.offset = selRow
	} else if selRow+2 > m.offset+bodyH {
		m.offset = selRow + 2 - bodyH
	}
	m.offset = max(0, min(m.offset, max(0, len(rows)-bodyH)))
	left := cBox.Width(leftW).Height(bodyH).Render(strings.Join(clip(rows[m.offset:], bodyH), "\n"))
	details := fitAll(strings.Split(strings.Join(m.details(rightW), "\n"), "\n"), rightW-2)
	right := cBox.Width(rightW).Height(bodyH).Render(strings.Join(clip(details, bodyH), "\n"))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{top}, bottom...)...)
}

func (m *model) details(width int) []string {
	a := m.current()
	if a == nil {
		return nil
	}
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
