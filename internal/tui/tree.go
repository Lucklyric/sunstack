package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
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
	st    *core.AgentStatus // the agent's files, on the Team tab
	org   *core.Org         // the snapshot the node comes from (this host's, or another host's)
	host  *hub.HostView     // set for nodes of another host of the org
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
	case s.Status == "not seen" || s.Status == "pane gone":
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

// teamScope says whether the tree shows only this team (the Team tab).
func (m *model) teamScope() bool {
	return m.p != nil && (m.view == viewTeam || m.view == viewLog || m.view == viewInbox)
}

// sel is the selection of the tree in front.
func (m *model) treeSel() *int {
	if m.teamScope() {
		return &m.teamSel
	}
	return &m.orgSel
}

// treeNodes lists the visible rows, top to bottom.
func (m *model) treeNodes() []treeNode {
	if m.teamScope() {
		return m.teamNodes()
	}
	o := m.org
	if o == nil {
		return nil
	}
	var out []treeNode
	splits := m.splits()
	if !m.filtering() {
		n := len(o.Attention)
		for _, sp := range splits {
			if sp.own != nil {
				n += len(sp.own.Attention)
			}
		}
		out = append(out, treeNode{kind: "needs", key: "needs", label: fmt.Sprintf("Needs you (%d)", n), org: o})
	}
	if m.hosts == nil {
		// In no org: this host's teams and sessions.
		return append(out, m.orgRows(o, "", 0, nil, nil)...)
	}
	if m.byHost {
		return append(out, m.byHostRows(splits)...)
	}
	// By team (§18.7): every team once, with the sessions of every host,
	// then one heading per host for its sessions outside teams.
	out = append(out, m.teamRows(o, "", 0, nil, splits, "")...)
	for _, sp := range splits {
		if sp.own != nil {
			out = append(out, m.teamRows(sp.own, "host:"+sp.h.ID+"/", 0, sp.h, nil, "  (only on "+sp.h.Name+")")...)
		}
	}
	for _, h := range m.hostOrder() {
		hk := "host:" + h.ID
		var rows []treeNode
		switch {
		case h.You:
			rows = m.outsideRows(o, hk+"/", 1, nil)
		case h.Org != nil:
			rows = m.outsideRows(h.Org, hk+"/", 1, h)
		}
		if len(rows) == 0 && (h.You || h.Org != nil) && !m.filtering() {
			rows = []treeNode{{kind: "info", key: hk + "/info", depth: 1, label: "no sessions outside teams", host: h, org: h.Org}}
		}
		out = m.hostHeading(out, h, rows)
	}
	return out
}

// byHostRows are every host, this one first, each with all its own teams
// and sessions; a team both hosts have shows under each, with that host's
// sessions.
func (m *model) byHostRows(splits []hostSplit) []treeNode {
	var out []treeNode
	if me := m.hostOrder()[0]; me.You {
		out = m.hostHeading(out, me, m.orgRows(m.org, "host:"+me.ID+"/", 1, nil, nil))
	}
	for _, sp := range splits {
		var rows []treeNode
		if sp.h.Org != nil {
			rows = m.orgRows(sp.h.Org, "host:"+sp.h.ID+"/", 1, sp.h, nil)
		}
		out = m.hostHeading(out, sp.h, rows)
	}
	return out
}

// hostOrder is the org's hosts with this one first, then as the view
// lists them (the hub, then by name).
func (m *model) hostOrder() []*hub.HostView {
	var me, rest []*hub.HostView
	for _, h := range m.hosts.Hosts {
		if h.You {
			me = append(me, h)
		} else {
			rest = append(rest, h)
		}
	}
	return append(me, rest...)
}

// hostHeading adds a host's heading and, unless folded, its rows. While
// filtering, a host with nothing that matches is left out.
func (m *model) hostHeading(out []treeNode, h *hub.HostView, rows []treeNode) []treeNode {
	if m.filtering() && len(rows) == 0 {
		return out
	}
	hk := "host:" + h.ID
	out = append(out, treeNode{kind: "host", key: hk, label: h.Label(), fold: len(rows) > 0, host: h, org: h.Org})
	if !m.folded[hk] {
		out = append(out, rows...)
	}
	return out
}

// hostSplit is another host's snapshot split against this host's scan:
// own is what only it has, shared the teams both have (hub.Split).
type hostSplit struct {
	h      *hub.HostView
	own    *core.Org // nil for a host never synced
	shared map[string]*core.OrgTeam
}

// splits are the other hosts of the org, in the order the view lists them.
func (m *model) splits() []hostSplit {
	if m.hosts == nil {
		return nil
	}
	var out []hostSplit
	for _, h := range m.hosts.Hosts {
		if h.You {
			continue
		}
		sp := hostSplit{h: h, shared: map[string]*core.OrgTeam{}}
		if h.Org != nil {
			var shared []*core.OrgTeam
			sp.own, shared = hub.Split(h.Org, m.org)
			for _, t := range shared {
				sp.shared[t.ID] = t
			}
		}
		out = append(out, sp)
	}
	return out
}

// remoteSession is a node for a session another host runs in a team this
// host also has: tagged with the host, acted on like the host's own rows.
func remoteSession(key string, d int, s *core.HostSession, sp hostSplit) treeNode {
	return treeNode{kind: "session", key: key + "/" + sessionName(s) + "@" + sp.h.ID, depth: d, label: sessionName(s) + " @" + sp.h.Name, sess: s, org: sp.h.Org, host: sp.h}
}

// orgRows are the rows of one host's snapshot: its teams, then sessions
// outside teams. Another host's rows carry it, and sit one level deeper.
// orgRows are the rows of one host's snapshot: its teams, then its sessions
// outside teams under one heading.
func (m *model) orgRows(o *core.Org, prefix string, d int, h *hub.HostView, splits []hostSplit) []treeNode {
	out := m.teamRows(o, prefix, d, h, splits, "")
	if outside := m.outsideRows(o, prefix, d+1, h); len(outside) > 0 {
		ok := prefix + "outside"
		out = append(out, treeNode{kind: "outside", key: ok, depth: d, label: "outside teams", fold: true, org: o, host: h})
		if !m.folded[ok] {
			out = append(out, outside...)
		}
	}
	return out
}

// teamRows are a snapshot's teams; splits, given for this host's teams,
// adds the other hosts' sessions in the teams both have. tag follows each
// team's name.
func (m *model) teamRows(o *core.Org, prefix string, d int, h *hub.HostView, splits []hostSplit, tag string) []treeNode {
	var out []treeNode
	open := func(key string) bool { return !m.folded[key] }
	for _, t := range o.Teams {
		var rows []treeNode
		tk := prefix + "team:" + t.Root
		agentRows := func(a *core.OrgAgent, ah *hub.HostView) {
			ak := tk + "/" + a.ID
			var ss []treeNode
			if ah == nil {
				for _, s := range a.Sessions {
					if m.keep(s, t.Name, a.ID) {
						ss = append(ss, treeNode{kind: "session", key: ak + "/" + sessionName(s), depth: d + 2, label: sessionName(s), sess: s, org: o, host: h})
					}
				}
			}
			for _, sp := range splits {
				if rt := sp.shared[t.ID]; rt != nil && (ah == nil || ah == sp.h) {
					for _, ra := range rt.Agents {
						if ra.ID != a.ID {
							continue
						}
						for _, s := range ra.Sessions {
							if m.keep(s, t.Name, a.ID) {
								ss = append(ss, remoteSession(ak, d+2, s, sp))
							}
						}
					}
				}
			}
			if (m.filtering() || ah != nil) && len(ss) == 0 {
				return
			}
			n := treeNode{kind: "agent", key: ak, depth: d + 1, label: a.ID, fold: len(ss) > 0, team: t, agent: a, org: o, host: h}
			if ah != nil {
				n.org, n.host = ah.Org, ah
			}
			rows = append(rows, n)
			if open(ak) {
				rows = append(rows, ss...)
			}
		}
		local := map[string]bool{}
		for _, a := range t.Agents {
			local[a.ID] = true
			agentRows(a, nil)
		}
		// Agents only another host has (new, not yet synced here).
		for _, sp := range splits {
			if rt := sp.shared[t.ID]; rt != nil {
				for _, ra := range rt.Agents {
					if !local[ra.ID] {
						local[ra.ID] = true
						agentRows(ra, sp.h)
					}
				}
			}
		}
		var free []treeNode
		for _, s := range t.Free {
			if m.keep(s, t.Name, "") {
				free = append(free, treeNode{kind: "session", key: tk + "/free/" + sessionName(s), depth: d + 2, label: sessionName(s), sess: s, org: o, host: h})
			}
		}
		for _, sp := range splits {
			if rt := sp.shared[t.ID]; rt != nil {
				for _, s := range rt.Free {
					if m.keep(s, t.Name, "") {
						free = append(free, remoteSession(tk+"/free", d+2, s, sp))
					}
				}
			}
		}
		if len(free) > 0 {
			fk := tk + "/free"
			rows = append(rows, treeNode{kind: "free", key: fk, depth: d + 1, label: fmt.Sprintf("free sessions (%d)", len(free)), fold: true, team: t, org: o, host: h})
			if open(fk) {
				rows = append(rows, free...)
			}
		}
		if m.filtering() && len(rows) == 0 {
			continue
		}
		out = append(out, treeNode{kind: "team", key: tk, depth: d, label: t.Name + tag, fold: len(rows) > 0, team: t, org: o, host: h})
		if open(tk) {
			out = append(out, rows...)
		}
	}
	return out
}

// outsideRows are a snapshot's sessions outside teams, by folder group, the
// groups at depth d.
func (m *model) outsideRows(o *core.Org, prefix string, d int, h *hub.HostView) []treeNode {
	var out []treeNode
	for _, g := range o.Free {
		gk := prefix + "group:" + g.Group
		var ss []treeNode
		for _, s := range g.Sessions {
			if m.keep(s, "", "") {
				ss = append(ss, treeNode{kind: "session", key: gk + "/" + sessionName(s), depth: d + 1, label: sessionName(s), sess: s, org: o, host: h})
			}
		}
		if len(ss) == 0 {
			continue
		}
		out = append(out, treeNode{kind: "group", key: gk, depth: d, label: path.Base(strings.TrimSuffix(g.Group, ".git")), fold: true, group: g, org: o, host: h})
		if !m.folded[gk] {
			out = append(out, ss...)
		}
	}
	return out
}

// orgTeam is this team in the last scan, if any.
func (m *model) orgTeam() *core.OrgTeam {
	if m.org == nil || m.p == nil {
		return nil
	}
	for _, t := range m.org.Teams {
		if t.Root == m.p.Root {
			return t
		}
	}
	return nil
}

// teamNodes is the tree of this team: its needs, each agent with its
// sessions, and its free sessions. Until a scan has run, sessions come from
// the claim files.
func (m *model) teamNodes() []treeNode {
	var out []treeNode
	team := m.orgTeam()
	name, root := filepath.Base(m.p.Root), m.p.Root
	if team != nil {
		name = team.Name
		if n := teamAttention(m.org, team); n > 0 && !m.filtering() {
			out = append(out, treeNode{kind: "needs", key: "needs:" + root, label: fmt.Sprintf("Needs you (%d)", n), team: team})
		}
	}
	for i := range m.agents {
		a := &m.agents[i]
		var sessions []*core.HostSession
		if team != nil {
			for _, oa := range team.Agents {
				if oa.ID == a.ID {
					sessions = oa.Sessions
				}
			}
		} else {
			for j, c := range a.Claims {
				st := "scanning…"
				if a.Where[j] == "pane closed" {
					st = "pane gone"
				}
				sessions = append(sessions, &core.HostSession{Tool: c.Tool, Status: st, Agent: a.ID, Label: c.Label(a.ID), Pane: c.TmuxPane, Where: a.Where[j], TeamRoot: root, SessionID: c.Session})
			}
		}
		ak := "agent:" + root + "/" + a.ID
		var ss []treeNode
		for _, s := range sessions {
			if m.keep(s, name, a.ID) {
				ss = append(ss, treeNode{kind: "session", key: ak + "/" + sessionName(s), depth: 1, label: sessionName(s), sess: s})
			}
		}
		if m.filtering() && len(ss) == 0 {
			continue
		}
		out = append(out, treeNode{kind: "agent", key: ak, label: a.ID, fold: len(ss) > 0, st: a})
		if !m.folded[ak] {
			out = append(out, ss...)
		}
	}
	if team != nil {
		var free []treeNode
		for _, s := range team.Free {
			if m.keep(s, name, "") {
				free = append(free, treeNode{kind: "session", key: root + "/free/" + sessionName(s), depth: 1, label: sessionName(s), sess: s})
			}
		}
		if len(free) > 0 {
			fk := "free:" + root
			out = append(out, treeNode{kind: "free", key: fk, label: fmt.Sprintf("free sessions (%d)", len(free)), fold: true, team: team})
			if !m.folded[fk] {
				out = append(out, free...)
			}
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
		switch {
		case n.st != nil && n.st.ClaimErr != nil:
			tail = cWarn.Render("claim file damaged")
		case n.st != nil && len(n.st.Proposals) > 0:
			tail = cWarn.Render(fmt.Sprintf("%d to approve", len(n.st.Proposals)))
		case n.st != nil && n.st.Inbox > 0:
			tail = cWarn.Render(fmt.Sprintf("%d message(s)", n.st.Inbox))
		case !n.fold:
			tail = cDim.Render("no session")
		}
	case "info":
		return cDim.Render(line)
	case "team":
		if c := len(m.teamWarnings(n)); c > 0 {
			tail = cWarn.Render(fmt.Sprintf("%d for you", c))
		}
	case "host":
		tail = stateGlyph(n.host.State)
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

// teamWarnings are the items for the user in a team row's team: on this
// host's row, its own and, tagged, those only another host reports.
func (m *model) teamWarnings(n treeNode) []string {
	o := n.org
	if o == nil {
		o = m.org
	}
	var out []string
	pick := func(list []string, tag string) {
		for _, a := range list {
			if strings.HasPrefix(a, n.team.Name+":") {
				out = append(out, strings.TrimPrefix(a, n.team.Name+": ")+tag)
			}
		}
	}
	if o != nil {
		pick(o.Attention, "")
	}
	if n.host == nil {
		for _, sp := range m.splits() {
			if sp.own != nil && sp.shared[n.team.ID] != nil {
				pick(sp.own.Attention, " (on "+sp.h.Name+")")
			}
		}
	}
	return out
}

func teamAttention(o *core.Org, t *core.OrgTeam) int {
	if o == nil {
		return 0
	}
	n := 0
	for _, a := range o.Attention {
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
	return m.treeBox(m.inner())
}

// treeBox draws the tree of the tab in front at height h.
func (m *model) treeBox(h int) string {
	nodes := m.treeNodes()
	sel := m.treeSel()
	if *sel >= len(nodes) {
		*sel = max(0, len(nodes)-1)
	}
	leftW := min(46, m.w/2)
	title := "Sessions"
	if m.teamScope() {
		title = "Team"
	}
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
		right = m.details2(nodes[*sel], m.w-leftW-6)
	}
	if m.inputPrompt != "" {
		right = append(right, "", cWarn.Render(m.inputPrompt+": ")+m.inputText+"█", cDim.Render("enter sends · esc cancels"))
	}
	if m.confirm != "" {
		right = append(right, "", cWarn.Render(m.confirm))
	}
	return m.panesH(title, left, *sel, right, m.orgScroll, h)
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
		shown := 0
		for _, a := range m.org.Attention {
			if n.team != nil {
				if !strings.HasPrefix(a, n.team.Name+":") {
					continue
				}
				a = strings.TrimPrefix(a, n.team.Name+": ")
			}
			out = append(out, wrap("• "+a, w))
			shown++
		}
		if n.team == nil {
			for _, sp := range m.splits() {
				if sp.own == nil {
					continue
				}
				for _, a := range sp.own.Attention {
					out = append(out, wrap("• "+a+" (on "+sp.h.Name+")", w))
					shown++
				}
			}
		}
		if shown == 0 {
			out = append(out, cDim.Render("nothing"))
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
		for _, a := range m.teamWarnings(n) {
			out = append(out, cWarn.Render(wrap("• "+a, w)))
		}
	case "agent":
		if n.st != nil {
			return m.agentDetails(n.st, w+2)
		}
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
	case "host", "info":
		return hostDetails(n.host, w, m.org)
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
		if n.host != nil {
			out = append(out, "", cDim.Render("on "+n.host.Name+", as of its last snapshot ("+hub.Age(n.host.SnapAge)+" old)"),
				"", cDim.Render("m message (through the hub) · other actions only on "+n.host.Name))
			break
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
	sel := m.treeSel()
	var n *treeNode
	if *sel < len(nodes) {
		n = &nodes[*sel]
	}
	switch k {
	case "up", "k":
		*sel = max(0, *sel-1)
		m.orgScroll = 0
	case "down", "j":
		*sel = min(max(0, len(nodes)-1), *sel+1)
		m.orgScroll = 0
	case "left":
		if n != nil && n.fold && !m.folded[n.key] {
			m.folded[n.key] = true
		} else if n != nil {
			// Up to the parent.
			for i := *sel - 1; i >= 0; i-- {
				if nodes[i].depth < n.depth {
					*sel = i
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
		case n.kind == "session" && n.host != nil:
			m.note = "that session is on " + n.host.Name + "; go to it there"
		case n.kind == "session":
			m.note = m.goToSession(n.sess)
		case n.fold:
			m.folded[n.key] = !m.folded[n.key]
		}
	case "/":
		m.inputPrompt, m.inputText = "filter", m.filter
		m.inputDone = func(text string) { m.filter, *sel = text, 0 }
	case "f":
		m.filterMode, *sel = (m.filterMode+1)%len(filterModes), 0
	case "b":
		if m.hosts == nil || m.teamScope() {
			return false
		}
		m.byHost, *sel = !m.byHost, 0
	case "esc":
		if !m.filtering() {
			return false
		}
		m.filter, m.filterMode, *sel = "", 0, 0
	case "m":
		if n == nil || n.kind != "session" {
			m.note = "select a session to message"
			return true
		}
		s := n.sess
		if h := n.host; h != nil {
			addr := remoteAddress(s)
			if addr == "" {
				m.note = "that session has no address another host can use"
				return true
			}
			m.inputPrompt, m.inputText = "message to "+h.Name+":"+addr, ""
			m.inputDone = func(text string) {
				if strings.TrimSpace(text) != "" {
					m.note = m.remoteSend(h.Name+":"+addr, text)
				}
			}
			return true
		}
		m.inputPrompt, m.inputText = "message to "+sessionName(s), ""
		m.inputDone = func(text string) {
			if strings.TrimSpace(text) != "" {
				m.note = m.sendTo(s, text)
			}
		}
	case "R", "K":
		if n != nil && n.host != nil && n.kind == "session" {
			m.note = "only on " + n.host.Name + ": reopen and close run on the session's own host"
			return true
		}
		if k == "K" {
			return m.closeKey(n)
		}
		if n == nil || n.kind != "session" || n.sess.SessionID == "" {
			m.note = "select a session with a session ID to reopen"
			return true
		}
		s := n.sess
		m.confirm = "Reopen " + sessionName(s) + " in a tmux pane? Exit it where it runs first. y to reopen, any other key to cancel"
		m.confirmDo = func() { m.note = m.reopen(s) }
	default:
		return false
	}
	return true
}

// closeKey asks to close an agent's session (K).
func (m *model) closeKey(n *treeNode) bool {
	if n == nil || n.kind != "session" {
		m.note = "select a session to close"
		return true
	}
	s := n.sess
	if s.Agent == "" && !m.isFree(s) {
		m.note = "only an agent's session or a free session sunstack started can be closed here; close this one in its own pane"
		return true
	}
	m.confirm = "Close " + sessionName(s) + "'s pane now? Unsaved work is lost. y to close, any other key to cancel"
	m.confirmDo = func() { m.note = m.kill(s) }
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

func realIsFree(s *core.HostSession) bool {
	_, _, ok := core.FreeByPane(s.Pane)
	return ok
}

func realKill(s *core.HostSession) string {
	if s.Agent == "" {
		label, sock, ok := core.FreeByPane(s.Pane)
		if !ok {
			return sessionName(s) + " is not a free session sunstack started"
		}
		out, err := core.KillFree(s.Pane, sock, label)
		if err != nil {
			return err.Error()
		}
		return out
	}
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
