// Package tui is the read-only team dashboard behind `sunstack tui`
// (design §6 TUI). Everything shown comes from sunstack/ and sunstack/_local/.
package tui

import (
	"encoding/base64"
	"fmt"
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
)

type tick time.Time

type model struct {
	p        *core.Project
	agents   []core.AgentStatus
	events   []string
	warnings []core.Check
	sel      int
	view     view
	w, h     int
	note     string
	updated  time.Time
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

// Run starts the dashboard for project p.
func Run(p *core.Project) error {
	m := &model{p: p}
	m.reload()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) reload() {
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

func (m *model) Init() tea.Cmd { return tickCmd() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tick:
		m.reload()
		return m, tickCmd()
	case tea.KeyMsg:
		m.note = ""
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.view = viewTeam
		case "up", "k":
			if m.sel > 0 {
				m.sel--
			}
		case "down", "j":
			if m.sel < len(m.agents)-1 {
				m.sel++
			}
		case "l":
			m.view = toggle(m.view, viewLog)
		case "i":
			m.view = toggle(m.view, viewInbox)
		case "r":
			m.reload()
		case "g":
			m.note = m.goToPane()
		case "c":
			m.note = m.copyResume()
		}
	}
	return m, nil
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

// goToPane switches the tmux client to the selected agent's pane.
func (m *model) goToPane() string {
	a := m.current()
	if a == nil || a.Claim == nil || a.Claim.TmuxPane == "" {
		return "no tmux pane recorded for this agent"
	}
	if a.Where == "pane closed" {
		return "that pane is gone"
	}
	pane := a.Claim.TmuxPane
	for _, args := range [][]string{{"select-window", "-t", pane}, {"select-pane", "-t", pane}, {"switch-client", "-t", pane}} {
		if err := exec.Command("tmux", args...).Run(); err != nil {
			return "tmux: " + err.Error()
		}
	}
	return "switched to " + a.Where
}

func resumeCmd(l *core.Live) string {
	if l == nil || l.Session == "" {
		return ""
	}
	switch l.Tool {
	case "claude":
		return "claude --resume " + l.Session
	case "codex":
		return "codex resume " + l.Session
	}
	return ""
}

// copyResume puts the resume command on the clipboard through OSC 52, which
// works over SSH and inside tmux (with set-clipboard on).
func (m *model) copyResume() string {
	a := m.current()
	if a == nil {
		return ""
	}
	cmd := resumeCmd(a.Claim)
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
	head := cTitle.Render("sunstack") + cDim.Render(fmt.Sprintf("  %s  ·  %s", m.p.Root, m.updated.Format("15:04:05")))
	var body string
	switch m.view {
	case viewLog:
		body = m.logView()
	case viewInbox:
		body = m.inboxView()
	default:
		body = m.teamView()
	}
	help := cDim.Render("↑↓ select · l log · i inbox · g go to pane · c copy resume · r refresh · esc back · q quit")
	if m.note != "" {
		help = cWarn.Render(m.note) + "  " + help
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, body, help)
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
	bottom := []string{cBox.Width(m.w - 4).Render(cHeader.Render("Events") + "\n" + strings.Join(orNone(events), "\n"))}
	var warn []string
	for _, c := range m.warnings {
		warn = append(warn, cWarn.Render("⚠ ")+c.Msg)
	}
	if len(warn) > 3 {
		warn = append(warn[:3], cDim.Render(fmt.Sprintf("… %d more (sunstack health)", len(m.warnings)-3)))
	}
	if len(warn) > 0 {
		bottom = append(bottom, cBox.Width(m.w-4).Render(strings.Join(warn, "\n")))
	}
	used := 2 // title and help lines
	for _, b := range bottom {
		used += lipgloss.Height(b)
	}
	bodyH := max(4, m.h-used-2) // minus the top boxes' borders

	var rows []string
	title := ""
	for i, a := range m.agents {
		if a.Title != title {
			title = a.Title
			rows = append(rows, cHeader.Render(title))
		}
		dot, state := cDim.Render("○"), cDim.Render("free")
		if a.Claim != nil {
			dot = cOn.Render("●")
			state = a.Claim.Tool
			if a.Where == "pane closed" {
				state += " " + cWarn.Render("pane closed")
			} else if a.Where != "" {
				state += " " + a.Where
			}
		}
		line := fmt.Sprintf("%s %-20s %s", dot, a.ID, state)
		if i == m.sel {
			line = cSel.Render(line)
		}
		rows = append(rows, line)
		rows = append(rows, cDim.Render(fmt.Sprintf("    inbox %d · 提议 %d · threads %d", a.Inbox, len(a.Proposals), len(a.Threads))))
	}
	left := cBox.Width(leftW).Height(bodyH).Render(strings.Join(clip(rows, bodyH), "\n"))
	right := cBox.Width(rightW).Height(bodyH).Render(strings.Join(clip(m.details(rightW), bodyH), "\n"))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, append([]string{top}, bottom...)...)
}

func (m *model) details(width int) []string {
	a := m.current()
	if a == nil {
		return nil
	}
	out := []string{cHeader.Render(a.ID), wrap(a.Duty, width-2), ""}
	if c := a.Claim; c != nil {
		out = append(out, fmt.Sprintf("claimed by %s on %s, last contact %s", c.Tool, c.Host, c.LastContact))
		if a.Where != "" {
			out = append(out, "tmux: "+a.Where)
		}
		if r := resumeCmd(c); r != "" {
			out = append(out, "resume: "+r)
		}
	} else {
		out = append(out, cDim.Render("free"))
	}
	out = append(out, "", cHeader.Render("Pillars"))
	if pl, err := m.p.Pillars(a.ID); err == nil {
		out = append(out, strings.Split(strings.TrimRight(pl, "\n"), "\n")...)
	}
	if len(a.Proposals) > 0 {
		out = append(out, "", cHeader.Render("提议 (waiting for your approval)"))
		out = append(out, a.Proposals...)
	}
	ctx, _ := os.ReadFile(filepath.Join(m.p.AgentDir(a.ID), "context.md"))
	if st := sectionLines(ctx, "当前状态"); len(st) > 0 {
		out = append(out, "", cHeader.Render("当前状态"))
		out = append(out, st...)
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
	h := max(5, m.h-4)
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return cBox.Width(m.w - 4).Height(h).Render(cHeader.Render("Events · "+label) + "\n" + strings.Join(orNone(lines), "\n"))
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
	return cBox.Width(m.w - 4).Height(max(5, m.h-4)).Render(cHeader.Render("Inbox · "+a.ID) + "\n" + strings.Join(orNone(lines), "\n"))
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
