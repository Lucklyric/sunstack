package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
	"github.com/Lucklyric/sunstack/internal/setup"
)

// The Org tab is a tree in one of three orders (§28), each level one kind of
// row: by project (teams, then the groups of sessions outside teams), by
// team (teams, then one "no team" row), or by host. The right pane shows the
// selected node; session nodes take actions.

const (
	orderProject = iota
	orderTeam
	orderHost
)

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
	if m.hosts != nil && m.order == orderHost {
		return m.byHostRows(splits)
	}
	// By project or team (§18.7): every team once, with the sessions of
	// every host, then the teams only another host has.
	out = append(out, m.teamRows(o, "", 0, nil, splits, "")...)
	out = append(out, m.remoteTeamRows(splits)...)
	if m.order != orderTeam {
		return append(out, m.groupRows(splits, 0)...)
	}
	// By team: the sessions outside teams under one row, by group.
	if groups := m.groupRows(splits, 1); len(groups) > 0 {
		out = append(out, treeNode{kind: "outside", key: "noteam", label: "no team", fold: true, org: o})
		if !m.folded["noteam"] {
			out = append(out, groups...)
		}
	}
	return out
}

// groupRows are the groups of sessions outside teams on every host, at depth
// d. A group keyed by a git remote is one row across hosts; one keyed by a
// folder is per host, so the same path on two hosts is two rows (§28.2).
func (m *model) groupRows(splits []hostSplit, d int) []treeNode {
	type part struct {
		g *core.FreeGroup
		o *core.Org
		h *hub.HostView
	}
	var keys []string
	parts := map[string][]part{}
	add := func(o *core.Org, h *hub.HostView) {
		if o == nil {
			return
		}
		for _, g := range o.Free {
			k := "group:" + g.Group
			if isFolder(g.Group) && h != nil {
				k = "host:" + h.ID + "/" + k
			}
			if _, ok := parts[k]; !ok {
				keys = append(keys, k)
			}
			parts[k] = append(parts[k], part{g, o, h})
		}
	}
	add(m.org, nil)
	for _, sp := range splits {
		add(sp.own, sp.h)
	}
	var out []treeNode
	for _, k := range keys {
		ps := parts[k]
		var ss []treeNode
		merged := &core.FreeGroup{Group: ps[0].g.Group}
		for _, p := range ps {
			for _, s := range p.g.Sessions {
				if m.keep(s, "", "") {
					ss = append(ss, treeNode{kind: "session", key: k + "/" + sessionName(s) + "@" + hostID(p.h), depth: d + 1, label: m.sessLabel(s, p.h), sess: s, org: p.o, host: p.h})
					merged.Sessions = append(merged.Sessions, s)
				}
			}
		}
		if len(ss) == 0 {
			continue
		}
		label := path.Base(strings.ReplaceAll(strings.TrimSuffix(ps[0].g.Group, ".git"), `\`, "/"))
		if isFolder(ps[0].g.Group) && m.hosts != nil {
			label += " @" + m.hostName(ps[0].h)
		}
		out = append(out, treeNode{kind: "group", key: k, depth: d, label: label, fold: true, group: merged, org: ps[0].o, host: ps[0].h})
		if !m.folded[k] {
			out = append(out, ss...)
		}
	}
	return out
}

// isFolder says whether a group is keyed by a folder rather than a git
// remote, whatever system the host that sent it runs: /..., C:\... or \\...
func isFolder(g string) bool {
	if strings.HasSuffix(g, ".git") {
		return false // a remote given as a path (/srv/git/x.git) is still a remote
	}
	return g == "" || g == "(unknown folder)" || strings.HasPrefix(g, "/") || strings.HasPrefix(g, `\\`) || filepath.IsAbs(g) ||
		(len(g) >= 3 && g[1] == ':' && (g[2] == '\\' || g[2] == '/'))
}

func hostID(h *hub.HostView) string {
	if h == nil {
		return "here"
	}
	return h.ID
}

// hostName is a host's name, this one's for nil.
func (m *model) hostName(h *hub.HostView) string {
	if h != nil {
		return h.Name
	}
	if m.hosts != nil {
		for _, x := range m.hosts.Hosts {
			if x.You {
				return x.Name // the org's name for this host
			}
		}
	}
	if m.org != nil {
		return m.org.Host.Name
	}
	return ""
}

// sessLabel is a session row's text: in an org, outside host order, it
// names the host the session runs on (§28.1).
func (m *model) sessLabel(s *core.HostSession, h *hub.HostView) string {
	if m.hosts == nil || m.order == orderHost {
		return sessionName(s)
	}
	return sessionName(s) + " @" + m.hostName(h)
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
	label := h.Label()
	if m.latest != "" && h.Version != "" && setup.Older(h.Version, m.latest) {
		label += "  update " + h.Version
	}
	out = append(out, treeNode{kind: "host", key: hk, label: label, fold: len(rows) > 0, host: h, org: h.Org})
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
	return append(m.teamRows(o, prefix, d, h, splits, ""), m.outsideRows(o, prefix, d, h)...)
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
						ss = append(ss, treeNode{kind: "session", key: ak + "/" + sessionName(s), depth: d + 2, label: m.sessLabel(s, h), sess: s, org: o, host: h})
					}
				}
			}
			for _, sp := range splits {
				// Every host that has the agent: one only other hosts run
				// gets its sessions from each of them.
				if rt := sp.shared[t.ID]; rt != nil {
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
				free = append(free, treeNode{kind: "session", key: tk + "/free/" + sessionName(s), depth: d + 2, label: m.sessLabel(s, h), sess: s, org: o, host: h})
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
		// Another host's session as of its last snapshot: not live when the
		// host stopped reporting.
		if n.host != nil && !n.host.You && n.host.State != "live" {
			tail = cDim.Render(n.sess.Status + ", " + n.host.State)
		}
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
		return cDim.Render(fit(line, w))
	case "team":
		if c := len(m.teamWarnings(n)); c > 0 {
			tail = cWarn.Render(fmt.Sprintf("%d for you", c))
		}
	case "host":
		tail = stateGlyph(n.host.State)
	}
	return rowText(line, tail, w)
}

// rowText preserves the status at the right edge when a label is long.
func rowText(label, tail string, w int) string {
	if tail == "" {
		return fit(label, w)
	}
	tail = fit(tail, max(0, w-4))
	label = fit(label, max(0, w-lipgloss.Width(tail)-1))
	return label + strings.Repeat(" ", max(1, w-lipgloss.Width(label)-lipgloss.Width(tail))) + tail
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
	title := "Sessions by " + []string{"project", "team", "host"}[m.order] // b cycles (§28)
	if m.teamScope() {
		title = "Team"
	}
	if m.filter != "" {
		title += "  filter: " + m.filter
	}
	if m.filterMode != 0 {
		title += "  show: " + filterModes[m.filterMode]
	}
	var left []string
	for _, n := range nodes {
		left = append(left, m.row(n, leftW-2))
	}
	if len(nodes) == 0 {
		left = append(left, cDim.Render("No matching sessions."), cDim.Render("Esc clears filters."))
	}
	var right []string
	if len(nodes) > 0 {
		right = m.details2(nodes[*sel], m.w-leftW-6)
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
	field := func(k, v string) string { return detailField(k, v, 7, w) }
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
			out = append(out, cDim.Render("Nothing needs you right now."))
		}
		for _, note := range m.org.Notes {
			out = append(out, "", cWarn.Render(wrap("note: "+note, w)))
		}
	case "team":
		t := n.team
		out = append(out, wrap(cHeader.Render(t.Name), w), cDim.Render(wrap(t.Root, w)), "", cHeader.Render("Objectives"))
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
			out = append(out, "", fmt.Sprintf("%d board item(s) need attention (Next tab, or sunstack next)", len(t.Issues)))
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
		out = append(out, wrap(cHeader.Render(sessionName(s)), w), glyph(s)+" "+s.Status+"  "+s.Tool, field("where", placeText(s)))
		if s.TeamName != "" {
			out = append(out, field("team", s.TeamName))
		}
		if s.Agent != "" {
			out = append(out, field("agent", s.Agent))
		}
		if s.Reach != "" {
			out = append(out, field("reach", s.Reach))
		}
		out = append(out, "")
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
			out = append(out, "", cDim.Render("on "+n.host.Name+", as of its last snapshot ("+hub.Age(n.host.SnapAge)+" old)"))
			if alias := core.LoadSSH().Hosts[n.host.ID]; alias != "" && s.Where != "" {
				out = append(out, field("attach", core.SSHAttach(alias, strings.SplitN(s.Where, ":", 2)[0])))
			}
			if rp, ok := m.remotePanes[n.host.ID+"|"+remoteAddress(s)]; ok {
				out = append(out, "", cDim.Render("── pane on "+n.host.Name+", "+hub.Age(time.Since(rp.at))+" ago ──"))
				out = append(out, strings.Split(strings.TrimRight(rp.text, "\n"), "\n")...)
			}
			out = append(out, "", cDim.Render(wrap("m message  p peek  s start beside it through the hub. Other actions only on "+n.host.Name+".", w)))
			break
		}
		if tail := m.peek(s); tail != "" {
			out = append(out, "", cDim.Render("── pane ──"))
			out = append(out, strings.Split(strings.TrimRight(tail, "\n"), "\n")...)
		}
		out = append(out, "", cDim.Render(wrap("enter go to pane  m message  R reopen  K close", w)))
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
	case "v":
		m.treeFiles(n)
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
		if m.teamScope() {
			return false
		}
		n := 3
		if m.hosts == nil {
			n = 2 // one host: no host order
		}
		m.order, *sel = (m.order+1)%n, 0
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
	case "p":
		if n == nil || n.kind != "session" || n.host == nil {
			return false
		}
		addr := remoteAddress(n.sess)
		if addr == "" {
			m.note = "that session has no address another host can use"
			return true
		}
		if err := core.UserOnly("peek"); err != nil {
			m.note = err.Error()
			return true
		}
		h := n.host
		m.note = "asking " + h.Name + " for the pane…"
		m.pending = func() tea.Msg {
			text, err := m.peekRemote(h.Name, addr)
			return peekMsg{key: h.ID + "|" + addr, text: text, err: err}
		}
	case "s":
		return m.spawnKey(n)
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
	out := cDim.Render(fmt.Sprintf("%s  %s  ", teams, plural(sessions, "session", "sessions")))
	// This host's items and the other hosts' own, as the Next tab lists them.
	if n := len(m.org.Attention) + len(m.otherHostsNext()); n > 0 {
		return out + cWarn.Render(fmt.Sprintf("%d for you", n))
	}
	return out + cDim.Render("0 for you")
}

type remotePane struct {
	text string
	at   time.Time
}

type peekMsg struct {
	key, text string
	err       error
}

func realPeekRemote(host, target string) (string, error) {
	r, err := hub.Peek(host, target, 30)
	if err != nil {
		return "", err
	}
	return r.Text, nil
}

func realSpawnRemote(host string, a hub.SpawnArgs) string {
	r, err := hub.Spawn(host, a)
	if err != nil {
		return err.Error()
	}
	return "started " + r.Label + " on " + host + " in " + r.Place
}

func realSpawnLocal(n treeNode, tool, note string) string {
	switch n.kind {
	case "agent":
		p, err := core.FindProject(n.team.Root)
		if err != nil {
			return err.Error()
		}
		r, err := p.Spawn(core.SpawnOptions{Arg: n.label, Tool: tool, Note: note, Place: "team"})
		if err != nil {
			return err.Error()
		}
		return "started " + r.Name + " in " + r.Home
	case "team":
		r, err := core.SpawnFree(core.FreeOptions{Tool: tool, Note: note, Root: n.team.Root})
		if err != nil {
			return err.Error()
		}
		return "started " + r.Label + " in " + r.Home
	}
	beside := n.sess.SessionID
	if beside == "" {
		beside = sessionName(n.sess)
	}
	r, err := core.SpawnFree(core.FreeOptions{Tool: tool, Note: note, Beside: beside})
	if err != nil {
		return err.Error()
	}
	return "started " + r.Label + " in " + r.Home
}

// spawnKey (s) starts a session from the selected row (§20.4): an agent row
// starts that agent, a team row a free session in the team, a session row a
// free session beside it. Rows of another host start it there.
func (m *model) spawnKey(n *treeNode) bool {
	if n == nil || (n.kind != "agent" && n.kind != "team" && n.kind != "session") || (n.kind != "session" && n.team == nil) {
		m.note = "select an agent, a team or a session to start a session from"
		return true
	}
	if err := core.UserOnly("starting a session"); err != nil {
		m.note = err.Error()
		return true
	}
	node := *n
	where := "this host"
	if node.host != nil {
		where = node.host.Name
	}
	var what string
	var a hub.SpawnArgs
	switch node.kind {
	case "agent":
		what = node.label
		a = hub.SpawnArgs{Team: node.team.Name, Agent: node.label}
	case "team":
		what = "a free session in " + node.team.Name
		a = hub.SpawnArgs{Team: node.team.Name, Free: true}
	default:
		what = "a free session beside " + sessionName(node.sess)
		a = hub.SpawnArgs{Free: true, Beside: node.sess.SessionID}
		if node.host != nil && !core.IsSessionID(a.Beside) {
			m.note = "that session has no ID another host can use"
			return true
		}
	}
	m.inputPrompt, m.inputText = "start "+what+" on "+where+": [claude|codex] [first instruction]", ""
	m.inputDone = func(text string) {
		tool, note := "", strings.TrimSpace(text)
		if f := strings.Fields(note); len(f) > 0 && (f[0] == "claude" || f[0] == "codex") {
			tool, note = f[0], strings.TrimSpace(strings.TrimPrefix(note, f[0]))
		}
		a.Tool, a.Note = tool, note
		m.confirm = "Start " + what + " on " + where + "? It starts a paid session. y to start, any other key to cancel"
		m.confirmDo = func() {
			m.note = "starting " + what + " on " + where + "…"
			m.pending = func() tea.Msg {
				if node.host != nil {
					return hubMsg{m.spawnRemote(node.host.Name, a)}
				}
				return hubMsg{m.spawnLocal(node, tool, note)}
			}
		}
	}
	return true
}

// otherHostsNext are the other hosts' items for the Next tab, as sunstack
// next lists them; the ones this host reports too are left out (§28.1).
func (m *model) otherHostsNext() []core.Issue {
	var out []core.Issue
	if m.org == nil {
		return nil // no scan yet: nothing to tell this host's items from theirs
	}
	for _, sp := range m.splits() {
		if sp.own != nil {
			for _, a := range sp.own.Attention {
				out = append(out, core.Issue{Kind: "user", Owner: "user", Text: a, Do: "on " + sp.h.Name})
			}
		}
	}
	return out
}

// hostNext is the Next tab's list: in a team its ranked items, in the host
// view this host's attention across teams; then the other hosts' (§28.1).
func (m *model) hostNext() []core.Issue {
	var out []core.Issue
	switch {
	case m.p != nil:
		out = m.p.Next("", time.Now())
	case m.org != nil:
		for _, a := range m.org.Attention {
			out = append(out, core.Issue{Kind: "user", Owner: "user", Text: a, Do: "in that team"})
		}
	}
	out = append(out, m.otherHostsNext()...)
	core.Rank(out, "") // the other hosts' decisions above local housekeeping
	return out
}

// remoteTeamRows are the teams this host lacks, one row per TEAM ID however
// many other hosts have it: the first host's copy, with the others'
// sessions merged in as for a shared team (§28.1).
func (m *model) remoteTeamRows(splits []hostSplit) []treeNode {
	var out []treeNode
	seen := map[string]bool{}
	for i, sp := range splits {
		if sp.own == nil {
			continue
		}
		for _, t := range sp.own.Teams {
			// A team without a TEAM file is keyed by its root: per host,
			// never matched across hosts (§18.7).
			key := t.ID
			if !hub.SharedID(t.ID) {
				key = sp.h.ID + "/" + t.ID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			names := []string{sp.h.Name}
			var more []hostSplit
			one := *sp.own
			one.Teams = []*core.OrgTeam{t}
			one.Attention = append([]string(nil), sp.own.Attention...)
			for _, sp2 := range splits[i+1:] {
				if sp2.own == nil {
					continue
				}
				for _, t2 := range sp2.own.Teams {
					if t2.ID == t.ID && hub.SharedID(t.ID) {
						more = append(more, hostSplit{h: sp2.h, shared: map[string]*core.OrgTeam{t.ID: t2}})
						names = append(names, sp2.h.Name)
						// Its items for this team, on the merged row.
						for _, a := range sp2.own.Attention {
							if strings.HasPrefix(a, t.Name+":") {
								one.Attention = append(one.Attention, a+" (on "+sp2.h.Name+")")
							}
						}
					}
				}
			}
			out = append(out, m.teamRows(&one, "host:"+sp.h.ID+"/", 0, sp.h, more, "  (only on "+strings.Join(names, ", ")+")")...)
		}
	}
	return out
}
