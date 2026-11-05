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
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
	"github.com/Lucklyric/sunstack/internal/setup"
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
	viewBoard
	viewTasks
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
	nextShow   int // index into nextShows: the Next tab's chip
	nextItems  []core.Issue
	orgSel     int
	saved      savedState // what tui.json holds, so it is written only on a change
	teamSel    int        // the selected row of the Team tab's tree

	folded     map[string]bool // tree nodes the user folded
	filter     string
	filterMode int  // index into filterModes
	showAll    bool // the org tree shows closed sessions too (a)
	compact    bool // one column whatever the width (--compact)
	// The tmux pane the user is in, as "socket\tpane", so the side panel can
	// mark that session's row (§41.2); focus reads it on each tick.
	focused string
	focus   func() string
	popup   bool // in a tmux popup: going to a pane quits (--popup)
	jumped  bool // the last key or click went to a pane
	detail  bool // in one column: the details in place of the list (d)
	// Where the last frame drew its list and tabs, for mouse clicks.
	listStart, listN, listW, listSel int
	titleX                           []titleSpan // where the chips sit on the chip row
	chips, chipList                  []chip      // chips for the next list drawn, and those drawn
	listY0, chipY                    int         // screen lines of the list's first row and of the chips
	footX, headX                     []clickSpan // the footer's keys and the header's counts
	footY                            int
	menu                             *menu // the actions menu, while open (§40.7)
	rename                           func(*core.HostSession, string) error
	tabX                             [][2]int
	order                            int    // the Org tab's order: orderProject, orderTeam or orderHost (b cycles)
	inputPrompt                      string // a line being typed: a filter or a message
	inputText                        string
	inputDone                        func(string)
	confirmDo                        func()
	sendTo                           func(*core.HostSession, string) string
	kill                             func(*core.HostSession) string
	isFree                           func(*core.HostSession) bool // a free session spawn --free started
	reopen                           func(*core.HostSession) string
	peek                             func(*core.HostSession) string

	hosts      *hub.OrgView // the org this host belongs to, from the local files; nil outside an org
	hostSel    int
	loadView   func(time.Time) *hub.OrgView
	remoteSend func(to, text string) string
	latest     string                           // the newest release, from the daily check (§20.6)
	updateHost func(name, target string) string // asks another host to update
	updateHere func() string                    // updates this host and restarts its connector
	sshCheck   func(alias string) string        // a fresh SSH login, off the UI loop (§23.3)
	sshStop    func(alias string) string
	sshStart   func(alias string) tea.Cmd // hands the terminal to sunstack ssh start
	pending    tea.Cmd                    // what a confirmed action runs next
	// Another host's sessions (§20.4, §20.5), through the hub.
	peekRemote  func(host, target string) (string, error)
	spawnRemote func(host string, a hub.SpawnArgs) string
	spawnLocal  func(n treeNode, tool, note string) string
	remotePanes map[string]remotePane // by host ID and address
	refreshHub  tea.Cmd

	viewer *viewer // the file viewer, in front of everything while open (§21.3)
	board  boardState
	tasks  tasksState
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
func Run(p *core.Project, org, compact, popup bool) error {
	m := startModel(p, org)
	m.compact, m.popup = compact, popup
	if compact && os.Getenv("TMUX_PANE") != "" {
		m.focus = tmuxFocus
		m.focused = tmuxFocus()
	}
	m.cwd, _ = os.Getwd()
	m.reload()
	// The newest release: the last check now, a fresh one for next time
	// (at most daily, §20.6).
	m.latest = setup.CachedLatest()
	go setup.RefreshLatest()
	if !lipgloss.HasDarkBackground() {
		glamourStyle = "light"
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// newModel opens on the team, or with org on the host view. Without a team
// it opens on the team picker, or with org on the host view.
func newModel(p *core.Project, org bool) *model {
	m := &model{p: p, folded: map[string]bool{}, sendTo: realSend, rename: core.RenameSession, kill: realKill, isFree: realIsFree, reopen: realReopen, peek: realPeek,
		loadView: hub.LoadView, remoteSend: realRemoteSend, refreshHub: realRefresh,
		updateHost: realUpdateHost, updateHere: realUpdateHere,
		sshCheck: realSSHCheck, sshStop: realSSHStop, sshStart: realSSHStart,
		peekRemote: realPeekRemote, spawnRemote: realSpawnRemote, spawnLocal: realSpawnLocal, remotePanes: map[string]remotePane{}}
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
	if m.view == viewNext {
		m.nextItems = m.hostNext()
	}
	if m.p == nil {
		return
	}
	switch m.view {
	case viewBoard:
		m.loadBoard()
	case viewTasks:
		m.loadTasks()
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
// Next needs the scan too: it compares the other hosts' items with this
// host's (§28.1).
func (m *model) scans() bool {
	return m.view == viewOrg || m.view == viewNext || (m.view == viewTeam && m.p != nil)
}

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
		m.resizeViewer()
	case tick:
		if m.focus != nil {
			m.focused = m.focus()
		}
		m.reload()
		if m.scans() && !m.orgBusy && time.Since(m.orgAt) > orgRefresh {
			m.orgBusy = true
			return m, tea.Batch(tickCmd(), orgCmd())
		}
		return m, tickCmd()
	case orgMsg:
		m.org, m.orgAt, m.orgBusy = msg.o, time.Now(), false
		if m.view == viewNext {
			m.nextItems = m.hostNext() // now with this host's items to compare
		}
	case hubMsg:
		m.note = msg.note
		m.loadHosts()
	case peekMsg:
		if msg.err != nil {
			m.note = msg.err.Error()
		} else {
			m.remotePanes[msg.key] = remotePane{text: msg.text, at: time.Now()}
			m.note = "pane read through the hub"
		}
	case renameMsg:
		m.renamed(msg)
		return m, m.afterJump(nil)
	case tea.MouseMsg:
		cmd := m.mouse(msg)
		m.saveState()
		return m, m.afterJump(cmd)
	case tea.KeyMsg:
		if m.viewer != nil && msg.String() != "ctrl+c" {
			return m, m.viewerKey(msg)
		}
		if msg.Paste && m.inputPrompt != "" {
			m.inputText += pasteText(msg.Runes)
			return m, nil
		}
		cmd := m.key(msg.String())
		m.saveState()
		return m, m.afterJump(cmd)
	}
	return m, nil
}

// afterJump quits a popup dashboard once it went to a pane, so the popup
// closes over that pane.
func (m *model) afterJump(cmd tea.Cmd) tea.Cmd {
	if m.popup && m.jumped {
		return tea.Quit
	}
	m.jumped = false
	return cmd
}

// mouse handles the wheel and left clicks: the wheel moves the selection
// (or scrolls open details), a click on a tab opens it, a click on a list
// row selects it, and a click on the selected row opens it like enter.
func (m *model) mouse(e tea.MouseMsg) tea.Cmd {
	if m.viewer != nil || m.inputPrompt != "" || m.confirm != "" || e.Action != tea.MouseActionPress {
		return nil
	}
	if m.menu != nil {
		return m.menuMouse(e)
	}
	if e.Button == tea.MouseButtonLeft && (e.Y == m.footY || e.Y == 0) {
		spans := m.footX
		if e.Y == 0 {
			spans = m.headX
		}
		for _, s := range spans {
			if e.X >= s.x0 && e.X < s.x1 {
				return s.run(m)
			}
		}
		if e.Y == m.footY {
			return nil
		}
	}
	switch e.Button {
	case tea.MouseButtonWheelUp:
		if m.inDetail() {
			return m.key("pgup")
		}
		return m.key("up")
	case tea.MouseButtonWheelDown:
		if m.inDetail() {
			return m.key("pgdown")
		}
		return m.key("down")
	case tea.MouseButtonLeft, tea.MouseButtonRight:
	default:
		return nil
	}
	if m.help {
		m.help = false
		return nil
	}
	top := 1 // the title line
	if m.showTabs() {
		if e.Y == top && e.Button == tea.MouseButtonLeft {
			for i, r := range m.tabX {
				if e.X >= r[0] && e.X < r[1] && i < len(m.tabs()) {
					return m.show(m.tabs()[i])
				}
			}
			return nil
		}
		top++
	}
	y0 := m.listY0
	if m.chipY > 0 && e.Y >= m.chipY && e.Y < y0 {
		for _, s := range m.titleX {
			if e.Y == m.chipY+s.y && e.X >= s.x0+2 && e.X < s.x1+2 { // past the border and padding
				m.openChip(s.chip, s.x0+1, e.Y+1)
			}
		}
		return nil
	}
	if m.inDetail() || m.listN == 0 || e.Y < y0 || e.Y >= y0+m.listN || e.X >= m.listW+2 {
		return nil
	}
	target := m.listStart + e.Y - y0
	if m.view == viewPicker && target >= len(m.teams) {
		if target == len(m.teams) {
			return nil // the rule between teams and actions
		}
		target--
	}
	if target == m.selIndex() {
		if e.Button == tea.MouseButtonRight {
			if m.treeView2() {
				m.openMenu(e.X, e.Y)
			}
			return nil
		}
		return m.key("enter")
	}
	m.setSel(target)
	if e.Button == tea.MouseButtonRight && m.treeView2() {
		m.openMenu(e.X, e.Y)
	}
	return nil
}

// selIndex and setSel are the selection of the list in front.
func (m *model) selIndex() int {
	switch {
	case m.view == viewPicker:
		return m.pickSel
	case m.view == viewNext:
		return m.nextSel
	case m.view == viewBoard:
		return m.board.sel
	case m.view == viewTasks:
		return m.tasks.sel
	}
	return *m.treeSel()
}

func (m *model) setSel(i int) {
	switch {
	case m.view == viewPicker:
		m.pickSel = i
	case m.view == viewNext:
		m.nextSel, m.nextScroll = i, 0
	case m.view == viewBoard:
		m.board.sel, m.board.scroll = i, 0
	case m.view == viewTasks:
		m.tasks.sel, m.tasks.scroll = i, 0
	default:
		*m.treeSel(), m.orgScroll = i, 0
	}
}

// inDetail says whether one column shows details in place of the list.
func (m *model) inDetail() bool { return m.detail && m.compactMode() }

// treeView2 says whether the tab in front is a session tree (Team or Org),
// the one the actions menu serves.
func (m *model) treeView2() bool {
	return m.view == viewOrg || (m.view == viewTeam && m.p != nil)
}

// pasteText is pasted text for a one-line prompt: line breaks and tabs
// become spaces, other control characters are dropped.
func pasteText(rs []rune) string {
	var b strings.Builder
	for _, r := range rs {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// key handles one key press.
func (m *model) key(k string) tea.Cmd {
	m.note = ""
	if m.menu != nil {
		return m.menuKey(k)
	}
	if m.inputPrompt != "" {
		// Typing a line: every key goes to it.
		switch k {
		case "enter":
			done, text := m.inputDone, m.inputText
			m.inputPrompt, m.inputText, m.inputDone = "", "", nil
			if done != nil {
				done(text)
			}
			cmd := m.pending // what the line starts, such as a rename
			m.pending = nil
			return cmd
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
		cmd := m.pending
		m.pending = nil
		return cmd
	}
	if k == "?" {
		m.help = true
		return nil
	}
	if m.view == viewPicker {
		return m.pickerKey(k)
	}
	if m.compactMode() {
		switch {
		case k == "d":
			m.detail = !m.detail
			return nil
		case k == "esc" && m.inDetail():
			m.detail = false
			return nil
		}
	}
	if (m.view == viewOrg || (m.view == viewTeam && m.p != nil)) && m.treeKey(k) {
		cmd := m.pending
		m.pending = nil
		return cmd
	}
	if m.view == viewHosts {
		if ok, cmd := m.hostsKey(k); ok {
			return cmd
		}
	}
	if m.view == viewBoard && m.p != nil && m.boardKey(k) {
		return nil
	}
	if m.view == viewTasks && m.p != nil && m.tasksKey(k) {
		return nil
	}
	if k == "h" && m.hosts != nil {
		if m.view == viewHosts && m.p == nil {
			return m.show(viewOrg) // no team view outside a team (§34.15)
		}
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
		case "n":
			if m.view == viewNext {
				return m.show(viewOrg) // the host view has no Team tab to go back to
			}
			return m.show(viewNext) // this host's list, every team
		case "l", "i", "g", "c", "B", "T", "v", "tab", "shift+tab":
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
	case "B":
		return m.show(toggle(m.view, viewBoard))
	case "T":
		return m.show(toggle(m.view, viewTasks))
	case "v":
		if m.view == viewTeam {
			nodes := m.treeNodes()
			if sel := *m.treeSel(); sel < len(nodes) {
				m.treeFiles(&nodes[sel])
			} else {
				m.openViewer(m.p, m.p.TeamViewFiles(), "")
			}
		}
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
		return []view{viewTeam, viewNext, viewBoard, viewTasks, viewOrg, viewHosts}
	}
	return []view{viewTeam, viewNext, viewBoard, viewTasks, viewOrg}
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
	case viewBoard, viewTasks:
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
	m.jumped = true
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

// View is the screen. Text from files, boards and other hosts is in it, so
// only its colors pass as escape sequences.
func (m *model) View() string {
	screen := m.render()
	m.footY = lipgloss.Height(screen) - 1
	if m.menu != nil {
		screen = m.withMenu(screen)
	}
	return termSafe(screen)
}

// termSafe keeps SGR color sequences, newlines and tabs, and drops every
// other control character and sequence: OSC 52 would set the clipboard,
// OSC 8 plant a link, CSI move the cursor.
func termSafe(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				b.WriteString(s[i : j+1])
			}
			i = min(j+1, len(s))
			continue
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r == utf8.RuneError && n == 1):
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

func (m *model) render() string {
	if m.w == 0 {
		return "loading…"
	}
	m.listN, m.tabX, m.titleX, m.headX = 0, nil, nil, nil // set again by what this frame draws
	where := ""
	if m.p != nil {
		where = m.p.Root
	} else if m.org != nil {
		where = "host " + m.org.Host.Name
	} else {
		where = "host " + core.ThisHost().Name
	}
	head := cTitle.Render("sunstack") + "  " + cDim.Render(where)
	if m.view == viewOrg && m.org != nil {
		counts := m.orgCounts()
		head = fit(head, max(8, m.w-lipgloss.Width(counts)-2)) + "  " + counts
		m.headX = headSpans(head)
	}
	if m.viewer != nil {
		return m.viewerView()
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
	case m.view == viewBoard:
		body = m.boardView()
	case m.view == viewTasks:
		body = m.tasksView()
	default:
		body = m.teamView()
	}
	parts := []string{fit(head, m.w)}
	if m.showTabs() {
		parts = append(parts, m.tabBar())
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(parts, body, m.footer())...)
}

func (m *model) footer() string {
	switch {
	case m.help:
		return cDim.Render("Any key closes help")
	case m.inputPrompt != "":
		line := m.inputPrompt + ": " + m.inputText + "█"
		if n := lipgloss.Width(line) - m.w; n > 0 {
			line = ansi.TruncateLeft(line, n+1, "…")
		}
		return cWarn.Render(line) + "\n" + cDim.Render("enter apply  esc cancel")
	case m.confirm != "":
		return cWarn.Render(wrap(m.confirm, m.w))
	}
	hints := "↑↓ select  ←→ fold  enter open  . actions  / filter  f show"
	if m.compactMode() {
		hints = "↑↓ select  enter go  . actions  d details  / filter  f show"
		if m.inDetail() {
			hints = "d or esc list  pgup/pgdn scroll"
		}
	}
	if !m.teamScope() && !m.inDetail() {
		hints += "  a " + map[bool]string{true: "running only", false: "all"}[m.showAll]
	}
	switch m.view {
	case viewPicker:
		hints = "↑↓ select  enter open  o host view  s find teams  n set up"
	case viewNext:
		hints = "↑↓ select  pgup/pgdn details  r refresh  esc back"
	case viewLog, viewInbox:
		hints = "l log  i inbox  g pane  c copy resume  esc back"
	case viewHosts:
		hints = "↑↓ host  enter sessions  u update  c ssh  h back"
	case viewBoard:
		hints = "↑↓ select  enter open  f " + boardFilters[(m.board.filter+1)%len(boardFilters)] + "  / filter  v files"
	case viewTasks:
		hints = "↑↓ select  v message  a " + map[bool]string{true: "open only", false: "closed too"}[m.tasks.all]
	}
	keys := "? help  q quit"
	line := m.footLine(fit(hints, max(0, m.w-lipgloss.Width(keys)-2)) + "  " + keys)
	if m.note != "" {
		line = cWarn.Render(fit(m.note, m.w)) + "\n" + line
	}
	return line
}

func (m *model) teamView() string {
	if len(m.agents) == 0 {
		return cBox.Width(m.w - 2).Render(wrap("No agents yet. Run sunstack hire <title> [name], or ask an agent to recruit one.", m.w-4))
	}
	if m.compactMode() {
		return m.treeBox(m.inner()) // the list alone; health has its own command
	}
	// Bottom boxes first, so the two top boxes get exactly the rest.
	events := m.events
	if len(events) > 5 {
		events = events[len(events)-5:]
	}
	bottom := []string{cBox.Width(m.w - 2).Render(cHeader.Render("Events") + "\n" + strings.Join(fitAll(orEmpty(events, "No events yet."), m.w-4), "\n"))}
	var warn []string
	for _, c := range m.warnings {
		warn = append(warn, cWarn.Render("⚠ ")+c.Msg)
	}
	if len(warn) > 3 {
		warn = append(warn[:3], cDim.Render(fmt.Sprintf("… %d more (sunstack health)", len(m.warnings)-3)))
	}
	if len(warn) > 0 {
		bottom = append(bottom, cBox.Width(m.w-2).Render(strings.Join(fitAll(warn, m.w-4), "\n")))
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
	out := []string{wrap(cHeader.Render(a.ID), width-2), wrap(a.Duty, width-2), ""}
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
	if a.OverBudget != "" {
		out = append(out, "", cWarn.Render(a.OverBudget))
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
	if len(lines) > h-1 {
		lines = lines[len(lines)-h+1:]
	}
	return cBox.Width(m.w - 2).Height(h).Render(fit(cHeader.Render("Events: "+label), m.w-4) + "\n" + strings.Join(fitAll(orEmpty(lines, "No events yet."), m.w-4), "\n"))
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
	return cBox.Width(m.w - 2).Height(m.inner()).Render(fit(cHeader.Render("Inbox: "+a.ID), m.w-4) + "\n" + strings.Join(clip(fitAll(orEmpty(lines, "No messages waiting."), m.w-4), m.inner()-1), "\n"))
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

func orEmpty(lines []string, message string) []string {
	if len(lines) == 0 {
		return []string{cDim.Render(message)}
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
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
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

// tmuxFocus is the pane the user is in, beside this panel: its window's
// active pane, or the last active one while the panel itself has the focus.
func tmuxFocus() string {
	out, err := exec.Command("tmux", "list-panes", "-t", os.Getenv("TMUX_PANE"), "-F", "#{pane_active} #{pane_last} #{pane_id}").Output()
	if err != nil {
		return ""
	}
	self, active, last := os.Getenv("TMUX_PANE"), "", ""
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(l)
		if len(f) != 3 {
			continue
		}
		if f[0] == "1" {
			active = f[2]
		}
		if f[1] == "1" {
			last = f[2]
		}
	}
	if active == self {
		active = last
	}
	if active == "" {
		return ""
	}
	return strings.SplitN(os.Getenv("TMUX"), ",", 2)[0] + "\t" + active
}
