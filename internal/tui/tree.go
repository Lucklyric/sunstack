package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The Org tab is a tree: what needs the user, then each team with its
// agents and their sessions, then sessions outside teams. The right pane
// shows the selected node; session nodes take actions.

type treeNode struct {
	kind  string // needs, team, agent, free, outside, group, session
	key   string // stable across rescans, for folding
	depth int
	label string // the row text, without indentation or fold mark
	fold  bool   // has children that can be folded
	team  *core.OrgTeam
	agent *core.OrgAgent
	group *core.FreeGroup
	sess  *core.HostSession
}

var filterModes = []string{"all", "needs you", "busy", "outside tmux"}

var (
	cBusy    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	cWait    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cBlocked = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	cGone    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// glyph marks a session's state.
func glyph(s *core.HostSession) string {
	switch {
	case s.Status == "busy":
		return cBusy.Render("●")
	case s.Status == "waiting":
		return cWait.Render("◐")
	case s.Status == "blocked":
		return cBlocked.Render("⧗")
	case s.Status == "not seen":
		return cGone.Render("✕")
	case s.Reach == "background":
		return cDim.Render("◌")
	}
	return cDim.Render("○")
}

func sessionName(s *core.HostSession) string {
	switch {
	case s.Label != "":
		return strings.SplitN(s.Label, "@", 2)[0]
	case s.Name != "":
		return s.Name
	}
	return s.Tool + " session"
}

// keep says whether a session passes the text filter and the filter mode.
func (m *model) keep(s *core.HostSession, team, agent string) bool {
	switch filterModes[m.filterMode] {
	case "needs you":
		if s.Status != "waiting" && s.Status != "blocked" && s.Pending == 0 {
			return false
		}
	case "busy":
		if s.Status != "busy" {
			return false
		}
	case "outside tmux":
		if s.Pane != "" || s.SessionID == "" {
			return false
		}
	}
	if f := strings.ToLower(m.filter); f != "" {
		hay := strings.ToLower(strings.Join([]string{sessionName(s), s.Activity.Title, s.Tool, team, agent, s.Group}, " "))
		return strings.Contains(hay, f)
	}
	return true
}

func (m *model) filtering() bool { return m.filter != "" || m.filterMode != 0 }

// treeNodes lists the visible rows, top to bottom.
func (m *model) treeNodes() []treeNode {
	o := m.org
	if o == nil {
		return nil
	}
	var out []treeNode
	open := func(key string) bool { return !m.folded[key] }
	if !m.filtering() {
		out = append(out, treeNode{kind: "needs", key: "needs", label: fmt.Sprintf("Needs you (%d)", len(o.Attention))})
	}
	for _, t := range o.Teams {
		var rows []treeNode
		tk := "team:" + t.Root
		for _, a := range t.Agents {
			ak := tk + "/" + a.ID
			var ss []treeNode
			for _, s := range a.Sessions {
				if m.keep(s, t.Name, a.ID) {
					ss = append(ss, treeNode{kind: "session", key: ak + "/" + sessionName(s), depth: 2, label: sessionName(s), sess: s})
				}
			}
			if m.filtering() && len(ss) == 0 {
				continue
			}
			rows = append(rows, treeNode{kind: "agent", key: ak, depth: 1, label: a.ID, fold: len(ss) > 0, team: t, agent: a})
			if open(ak) {
				rows = append(rows, ss...)
			}
		}
		var free []treeNode
		for _, s := range t.Free {
			if m.keep(s, t.Name, "") {
				free = append(free, treeNode{kind: "session", key: tk + "/free/" + sessionName(s), depth: 2, label: sessionName(s), sess: s})
			}
		}
		if len(free) > 0 {
			fk := tk + "/free"
			rows = append(rows, treeNode{kind: "free", key: fk, depth: 1, label: fmt.Sprintf("free sessions (%d)", len(free)), fold: true, team: t})
			if open(fk) {
				rows = append(rows, free...)
			}
		}
		if m.filtering() && len(rows) == 0 {
			continue
		}
		out = append(out, treeNode{kind: "team", key: tk, label: t.Name, fold: len(rows) > 0, team: t})
		if open(tk) {
			out = append(out, rows...)
		}
	}
	var outside []treeNode
	for _, g := range o.Free {
		gk := "group:" + g.Group
		var ss []treeNode
		for _, s := range g.Sessions {
			if m.keep(s, "", "") {
				ss = append(ss, treeNode{kind: "session", key: gk + "/" + sessionName(s), depth: 2, label: sessionName(s), sess: s})
			}
		}
		if len(ss) == 0 {
			continue
		}
		outside = append(outside, treeNode{kind: "group", key: gk, depth: 1, label: path.Base(strings.TrimSuffix(g.Group, ".git")), fold: true, group: g})
		if open(gk) {
			outside = append(outside, ss...)
		}
	}
	if len(outside) > 0 {
		out = append(out, treeNode{kind: "outside", key: "outside", label: "outside teams", fold: true})
		if open("outside") {
			out = append(out, outside...)
		}
	}
	return out
}

// row draws one tree line: indentation, fold mark, label and state.
func (m *model) row(n treeNode, w int) string {
	indent := strings.Repeat("  ", n.depth)
	mark := "  "
	if n.fold {
		mark = "▾ "
		if m.folded[n.key] {
			mark = "▸ "
		}
	}
	line := indent + mark + n.label
	tail := ""
	switch n.kind {
	case "session":
		tail = glyph(n.sess) + " " + n.sess.Status
	case "agent":
		if len(n.agent.Sessions) == 0 {
			tail = cDim.Render("no session")
		}
	case "team":
		if c := m.teamAttention(n.team); c > 0 {
			tail = cWarn.Render(fmt.Sprintf("%d for you", c))
		}
	}
	if tail == "" {
		return line
	}
	pad := w - lipgloss.Width(line) - lipgloss.Width(tail)
	if pad < 1 {
		pad = 1
	}
	return line + strings.Repeat(" ", pad) + tail
}

func (m *model) teamAttention(t *core.OrgTeam) int {
	n := 0
	for _, a := range m.org.Attention {
		if strings.HasPrefix(a, t.Name+":") {
			n++
		}
	}
	return n
}

// treeView draws the tree on the left and the selected node on the right.
func (m *model) treeView() string {
	if m.org == nil {
		return m.panes("Sessions", []string{cDim.Render("scanning sessions…")}, -1, nil, 0)
	}
	nodes := m.treeNodes()
	if m.orgSel >= len(nodes) {
		m.orgSel = max(0, len(nodes)-1)
	}
	leftW := min(46, m.w/2)
	title := "Sessions"
	if m.filter != "" {
		title += " · filter: " + m.filter
	}
	if m.filterMode != 0 {
		title += " · show: " + filterModes[m.filterMode]
	}
	var left []string
	for _, n := range nodes {
		left = append(left, m.row(n, leftW-2))
	}
	if len(nodes) == 0 {
		left = append(left, cDim.Render("nothing matches (esc clears)"))
	}
	var right []string
	if len(nodes) > 0 {
		right = m.details2(nodes[m.orgSel], m.w-leftW-6)
	}
	if m.inputPrompt != "" {
		right = append(right, "", cWarn.Render(m.inputPrompt+": ")+m.inputText+"█", cDim.Render("enter sends · esc cancels"))
	}
	if m.confirm != "" {
		right = append(right, "", cWarn.Render(m.confirm))
	}
	return m.panes(title, left, m.orgSel, right, m.orgScroll)
}

func ago(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

func placeText(s *core.HostSession) string {
	switch {
	case s.Where == "pane closed":
		return "pane " + s.Pane + " closed"
	case s.Where == "left pane":
		return "pane " + s.Pane + " no longer runs " + s.Tool
	case s.Where != "":
		return "tmux " + s.Where + " " + s.Pane
	case s.Reach == "background":
		return "background"
	case s.SessionID != "":
		return "outside tmux (sees messages at its next prompt)"
	}
	return "no tmux pane"
}

// details2 is the right pane for a tree node.
func (m *model) details2(n treeNode, w int) []string {
	field := func(k, v string) string { return cDim.Render(fmt.Sprintf("%-7s", k)) + " " + wrap(v, w-8) }
	var out []string
	switch n.kind {
	case "needs":
		out = append(out, cHeader.Render("Needs you"), "")
		if len(m.org.Attention) == 0 {
			out = append(out, cDim.Render("nothing"))
		}
		for _, a := range m.org.Attention {
			out = append(out, wrap("• "+a, w))
		}
		for _, note := range m.org.Notes {
			out = append(out, "", cWarn.Render(wrap("note: "+note, w)))
		}
	case "team":
		t := n.team
		out = append(out, cHeader.Render(t.Name), cDim.Render(t.Root), "", cHeader.Render("Objectives"))
		if len(t.Objectives) == 0 {
			out = append(out, cDim.Render("none yet"))
		}
		for _, ob := range t.Objectives {
			out = append(out, wrap(ob.Key+" "+ob.Text, w))
			for _, kr := range ob.KRs {
				out = append(out, cDim.Render(wrap("  "+kr, w)))
			}
		}
		if len(t.Issues) > 0 {
			out = append(out, "", fmt.Sprintf("%d board item(s) need attention (Next tab, or sunstack board)", len(t.Issues)))
		}
		for _, a := range m.org.Attention {
			if strings.HasPrefix(a, t.Name+":") {
				out = append(out, cWarn.Render(wrap("• "+strings.TrimPrefix(a, t.Name+": "), w)))
			}
		}
	case "agent":
		a := n.agent
		out = append(out, cHeader.Render(a.ID), wrap(a.Duty, w), "")
		for _, x := range a.Now {
			out = append(out, field("now", x))
		}
		for _, x := range a.Done {
			out = append(out, cDim.Render(wrap("done "+x, w)))
		}
		if a.Pending > 0 {
			out = append(out, cWarn.Render(fmt.Sprintf("%d message(s) waiting", a.Pending)))
		}
		if len(a.Sessions) == 0 {
			out = append(out, "", cDim.Render("no live session (sunstack spawn "+a.ID+" starts one)"))
		}
	case "free", "outside", "group":
		out = append(out, cHeader.Render(n.label), "", cDim.Render("sessions that hold no agent; select one for its details"))
		if n.group != nil {
			out = append(out, "", cDim.Render(n.group.Group))
		}
	case "session":
		s := n.sess
		out = append(out, glyph(s)+" "+cHeader.Render(sessionName(s))+"  "+s.Status+" · "+s.Tool, placeText(s))
		where := []string{}
		if s.TeamName != "" {
			where = append(where, "team "+s.TeamName)
		}
		if s.Agent != "" {
			where = append(where, "agent "+s.Agent)
		}
		where = append(where, "reach "+s.Reach)
		out = append(out, cDim.Render(strings.Join(where, " · ")), "")
		if s.Doing != "" {
			out = append(out, field("doing", s.Doing))
		}
		if s.Activity.Title != "" {
			out = append(out, field("title", s.Activity.Title))
		}
		if s.Activity.LastPrompt != "" {
			out = append(out, field("prompt", s.Activity.LastPrompt))
		}
		if s.Activity.LastReply != "" {
			out = append(out, field("reply", s.Activity.LastReply))
		}
		if s.Activity.LastActive != "" {
			out = append(out, field("active", ago(s.Activity.LastActive)))
		}
		if s.SessionID != "" {
			out = append(out, field("id", s.SessionID))
		}
		if s.Pending > 0 {
			out = append(out, cWarn.Render(fmt.Sprintf("%d message(s) waiting", s.Pending)))
		}
		if tail := m.peek(s); tail != "" {
			out = append(out, "", cDim.Render("── pane ──"))
			out = append(out, strings.Split(strings.TrimRight(tail, "\n"), "\n")...)
		}
		out = append(out, "", cDim.Render("enter go to pane · m message · R reopen in tmux · K close"))
	}
	return out
}

// treeKey handles keys on the Org tab; false when the key is not its own.
func (m *model) treeKey(k string) bool {
	nodes := m.treeNodes()
	var n *treeNode
	if m.orgSel < len(nodes) {
		n = &nodes[m.orgSel]
	}
	switch k {
	case "left":
		if n != nil && n.fold && !m.folded[n.key] {
			m.folded[n.key] = true
		} else if n != nil {
			// Up to the parent.
			for i := m.orgSel - 1; i >= 0; i-- {
				if nodes[i].depth < n.depth {
					m.orgSel = i
					break
				}
			}
		}
	case "right":
		if n != nil && n.fold {
			delete(m.folded, n.key)
		}
	case "enter":
		switch {
		case n == nil:
		case n.kind == "session":
			m.note = m.goToSession(n.sess)
		case n.fold:
			m.folded[n.key] = !m.folded[n.key]
		}
	case "/":
		m.inputPrompt, m.inputText = "filter", m.filter
		m.inputDone = func(text string) { m.filter, m.orgSel = text, 0 }
	case "f":
		m.filterMode, m.orgSel = (m.filterMode+1)%len(filterModes), 0
	case "esc":
		if !m.filtering() {
			return false
		}
		m.filter, m.filterMode, m.orgSel = "", 0, 0
	case "m":
		if n == nil || n.kind != "session" {
			m.note = "select a session to message"
			return true
		}
		s := n.sess
		m.inputPrompt, m.inputText = "message to "+sessionName(s), ""
		m.inputDone = func(text string) {
			if strings.TrimSpace(text) != "" {
				m.note = m.sendTo(s, text)
			}
		}
	case "R":
		if n == nil || n.kind != "session" || n.sess.SessionID == "" {
			m.note = "select a session with a session ID to reopen"
			return true
		}
		s := n.sess
		m.confirm = "Reopen " + sessionName(s) + " in a tmux pane? Exit it where it runs first. y to reopen, any other key to cancel"
		m.confirmDo = func() { m.note = m.reopen(s) }
	case "K":
		if n == nil || n.kind != "session" {
			m.note = "select a session to close"
			return true
		}
		s := n.sess
		if s.Agent == "" {
			m.note = "only an agent's session can be closed here; close a free session in its own pane"
			return true
		}
		m.confirm = "Close " + sessionName(s) + "'s pane now? Unsaved work is lost. y to close, any other key to cancel"
		m.confirmDo = func() { m.note = m.kill(s) }
	default:
		return false
	}
	return true
}

func (m *model) goToSession(s *core.HostSession) string {
	if s.Pane == "" || s.Where == "pane closed" || s.Where == "left pane" {
		return sessionName(s) + " has no tmux pane to go to"
	}
	for _, args := range [][]string{{"select-window", "-t", s.Pane}, {"select-pane", "-t", s.Pane}, {"switch-client", "-t", s.Pane}} {
		if err := exec.Command("tmux", core.TmuxArgs(s.Socket(), args...)...).Run(); err != nil {
			return "tmux: " + err.Error()
		}
	}
	return "switched to " + s.Where
}

// The real actions; tests replace them.

func realSend(s *core.HostSession, text string) string {
	var r *core.SendResult
	var err error
	if s.Agent != "" && s.TeamRoot != "" {
		p, perr := core.FindProject(s.TeamRoot)
		if perr != nil {
			return perr.Error()
		}
		r, err = p.Send(core.SendOptions{To: sessionName(s), Body: text, Type: "fyi"})
	} else {
		r, err = core.SendToSession(s, core.SendOptions{Body: text, Type: "fyi"})
	}
	if err != nil {
		return err.Error()
	}
	if r.Nudged != "" {
		return "sent " + r.ID + ", nudged " + r.Nudged
	}
	return "sent " + r.ID + "; " + r.Note
}

func realKill(s *core.HostSession) string {
	p, err := core.FindProject(s.TeamRoot)
	if err != nil {
		return err.Error()
	}
	out, err := p.Kill(sessionName(s))
	if err != nil {
		return err.Error()
	}
	return out
}

func realReopen(s *core.HostSession) string {
	socket, server := "", ""
	if t := os.Getenv("TMUX"); t != "" {
		f := strings.Split(t, ",")
		socket = f[0]
		if len(f) > 1 {
			server = f[1]
		}
	}
	r, err := core.Reopen(core.ReopenOptions{SessionID: s.SessionID, Socket: socket, Server: server, Caller: os.Getenv("TMUX_PANE")})
	if err != nil {
		return err.Error()
	}
	return "reopened in pane " + r.Pane + " (" + r.Command + ")"
}

func realPeek(s *core.HostSession) string {
	if s.Pane == "" || s.Where == "pane closed" || s.Where == "left pane" || s.Where == "" {
		return ""
	}
	out, err := (*core.Project)(nil).Peek(s.Pane, s.Socket(), 8)
	if err != nil {
		return ""
	}
	return out
}

// orgCounts is the header summary of the host.
func (m *model) orgCounts() string {
	sessions := 0
	for _, t := range m.org.Teams {
		sessions += len(t.Free)
		for _, a := range t.Agents {
			sessions += len(a.Sessions)
		}
	}
	for _, g := range m.org.Free {
		sessions += len(g.Sessions)
	}
	teams := fmt.Sprintf("%d teams", len(m.org.Teams))
	if len(m.org.Teams) == 1 {
		teams = "1 team"
	}
	out := cDim.Render(fmt.Sprintf("%s · %d sessions · ", teams, sessions))
	if n := len(m.org.Attention); n > 0 {
		return out + cWarn.Render(fmt.Sprintf("%d for you", n))
	}
	return out + cDim.Render("0 for you")
}
