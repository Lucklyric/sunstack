package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

// The Hosts tab (design §18.6): the org as a star around its hub, drawn
// from the local files only, so drawing it never needs the network.

var cAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

// stateGlyph marks a host's link to the hub.
func stateGlyph(state string) string {
	switch state {
	case "live":
		return cBusy.Render("●")
	case "stale":
		return cWait.Render("◐")
	case "offline":
		return cGone.Render("✕")
	}
	return cDim.Render("○")
}

// hostCounts are a host's teams, sessions and items for the user; this
// host's come from its own scan.
func hostCounts(h *hub.HostView, local *core.Org) (int, int, int) {
	if h.You && local != nil {
		return hub.OrgCounts(local)
	}
	return h.Counts()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// linkText is the state of a host's link, as shown in its box.
func linkText(h *hub.HostView, outbox int) string {
	if h.You {
		t := "this host"
		if outbox > 0 {
			t += fmt.Sprintf(" · outbox %d", outbox)
		}
		return t
	}
	if h.State == "not synced yet" {
		return "○ not synced yet"
	}
	t := stateGlyph(h.State) + " " + h.State + " " + hub.Age(h.ContactAge)
	if h.Waiting > 0 {
		t += fmt.Sprintf(" · %d waiting", h.Waiting)
	}
	return t
}

// hostBox is one host's box: name, counts and link.
func hostBox(h *hub.HostView, local *core.Org, outbox int, selected, linkUp, linkDown bool) []string {
	name := h.Name
	if h.Hub {
		name = "◆ " + name + "  (hub)"
	}
	if h.You {
		name += "  (you)"
	}
	teams, sessions, needs := hostCounts(h, local)
	counts := plural(teams, "team", "teams") + " · " + plural(sessions, "session", "sessions")
	if needs > 0 {
		counts += " · " + cWarn.Render(fmt.Sprintf("%d for you", needs))
	}
	lines := []string{cHeader.Render(name), counts, linkText(h, outbox)}
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	border := cDim
	if selected {
		border = cAccent
	}
	edge := func(left, right string, join bool, mark rune) string {
		r := []rune(strings.Repeat("─", w+2))
		if join {
			r[(w+2)/2] = mark
		}
		return border.Render(left + string(r) + right)
	}
	out := []string{edge("┌", "┐", linkUp, '┴')}
	for _, l := range lines {
		out = append(out, border.Render("│")+" "+l+strings.Repeat(" ", w-lipgloss.Width(l))+" "+border.Render("│"))
	}
	return append(out, edge("└", "┘", linkDown, '┬'))
}

// linkCol is the column of a box's link mark: the middle of its border.
func linkCol(box []string) int { return 1 + (lipgloss.Width(box[0])-2)/2 }

// graph draws the org: the hub's box on top, every other host's box below
// it joined by links when they fit in width, otherwise one line per host.
func graph(v *hub.OrgView, sel int, width int, local *core.Org) []string {
	if v == nil || len(v.Hosts) == 0 {
		return []string{cDim.Render("this host is in no org (sunstack org join <hub>)")}
	}
	hubAt := 0
	for i, h := range v.Hosts {
		if h.Hub {
			hubAt = i
		}
	}
	var kids []int
	for i := range v.Hosts {
		if i != hubAt {
			kids = append(kids, i)
		}
	}
	top := hostBox(v.Hosts[hubAt], local, v.Outbox, sel == hubAt, false, len(v.Hosts) > 1)
	boxes := make([][]string, len(kids))
	total := 0
	for k, i := range kids {
		boxes[k] = hostBox(v.Hosts[i], local, v.Outbox, sel == i, true, false)
		if k > 0 {
			total += 2
		}
		total += lipgloss.Width(boxes[k][0])
	}
	topW := lipgloss.Width(top[0])
	if len(kids) == 0 {
		return top
	}
	if total > width {
		// Narrow: the hub, then one line per host.
		out := append([]string{}, hostBox(v.Hosts[hubAt], local, v.Outbox, sel == hubAt, false, false)...)
		for k, i := range kids {
			h := v.Hosts[i]
			branch := "├─ "
			if k == len(kids)-1 {
				branch = "└─ "
			}
			teams, sessions, _ := hostCounts(h, local)
			line := cDim.Render(branch) + h.Name
			if h.You {
				line += " (you)"
			}
			line += "  " + linkText(h, v.Outbox) + cDim.Render(fmt.Sprintf(" · %s · %s", plural(teams, "team", "teams"), plural(sessions, "session", "sessions")))
			if sel == i {
				line = cAccent.Render("▸ ") + line
			} else {
				line = "  " + line
			}
			out = append(out, line)
		}
		return out
	}
	// Wide: a star. W is the drawing's width; boxes are centered in it.
	W := max(total, topW)
	topX := (W - topW) / 2
	hubC := topX + linkCol(top)
	x := (W - total) / 2
	var centers []int
	starts := make([]int, len(kids))
	for k := range kids {
		starts[k] = x
		bw := lipgloss.Width(boxes[k][0])
		centers = append(centers, x+linkCol(boxes[k]))
		x += bw + 2
	}
	out := []string{}
	for _, l := range top {
		out = append(out, strings.Repeat(" ", topX)+l)
	}
	// The bus: from the leftmost to the rightmost link.
	lo, hi := min(centers[0], hubC), max(centers[len(centers)-1], hubC)
	bus := []rune(strings.Repeat(" ", W))
	for c := lo; c <= hi; c++ {
		bus[c] = '─'
	}
	for _, c := range centers {
		switch {
		case lo == hi:
			bus[c] = '│'
		case c == lo:
			bus[c] = '┌'
		case c == hi:
			bus[c] = '┐'
		default:
			bus[c] = '┬'
		}
	}
	if lo != hi {
		switch {
		case hubC == lo:
			bus[hubC] = '└'
			if contains(centers, hubC) {
				bus[hubC] = '├'
			}
		case hubC == hi:
			bus[hubC] = '┘'
			if contains(centers, hubC) {
				bus[hubC] = '┤'
			}
		default:
			bus[hubC] = '┴'
			if contains(centers, hubC) {
				bus[hubC] = '┼'
			}
		}
	}
	out = append(out, cDim.Render(strings.TrimRight(string(bus), " ")))
	for row := 0; row < len(boxes[0]); row++ {
		var b strings.Builder
		col := 0
		for k := range kids {
			b.WriteString(strings.Repeat(" ", starts[k]-col))
			b.WriteString(boxes[k][row])
			col = starts[k] + lipgloss.Width(boxes[k][row])
		}
		out = append(out, b.String())
	}
	return out
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// hostDetails is the right pane for a host.
func hostDetails(h *hub.HostView, w int, local *core.Org) []string {
	if h == nil {
		return nil
	}
	field := func(k, v string) string { return cDim.Render(fmt.Sprintf("%-9s", k)) + " " + wrap(v, w-10) }
	title := h.Name
	if h.Hub {
		title += " (hub)"
	}
	if h.You {
		title += " (this host)"
	}
	out := []string{cHeader.Render(title), ""}
	if h.You {
		out = append(out, field("link", "this host"))
	} else {
		out = append(out, field("link", stateGlyph(h.State)+" "+h.State))
		if h.State != "not synced yet" {
			out = append(out, field("contact", hub.Age(h.ContactAge)+" ago"))
		}
		if h.HasSnap {
			out = append(out, field("snapshot", hub.Age(h.SnapAge)+" old"))
		}
	}
	send := "read only"
	if h.CanSend {
		send = "can send"
	}
	out = append(out, field("messages", send))
	if h.Fingerprint != "" {
		out = append(out, field("key", h.Fingerprint))
	}
	if h.KeyChanged {
		out = append(out, cWarn.Render(wrap("Its key changed: mail from it is refused. Compare fingerprints (sunstack org keys on both machines), then sunstack org trust "+h.Name+".", w)))
	}
	if h.Waiting > 0 {
		out = append(out, field("waiting", fmt.Sprintf("%d message(s) on the hub", h.Waiting)))
	}
	o := h.Org
	if h.You {
		o = local
	}
	if o == nil {
		return append(out, "", cDim.Render("no snapshot yet"))
	}
	out = append(out, "", cHeader.Render("Teams"))
	if len(o.Teams) == 0 {
		out = append(out, cDim.Render("none"))
	}
	for _, t := range o.Teams {
		counts := map[string]int{}
		for _, a := range t.Agents {
			for _, s := range a.Sessions {
				counts[s.Status]++
			}
		}
		var parts []string
		for _, st := range []string{"busy", "waiting", "blocked", "idle"} {
			if counts[st] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[st], st))
			}
		}
		line := t.Name + "  " + plural(len(t.Agents), "agent", "agents")
		if len(parts) > 0 {
			line += " · " + strings.Join(parts, ", ")
		}
		out = append(out, wrap(line, w))
	}
	if len(o.Attention) > 0 {
		out = append(out, "", cHeader.Render("Needs you"))
		for _, a := range o.Attention {
			out = append(out, cWarn.Render(wrap("• "+a, w)))
		}
	}
	return out
}

// remoteAddress is how another host addresses a session: its team and
// label, or its session ID.
func remoteAddress(s *core.HostSession) string {
	if s.Agent != "" {
		team := s.Team
		if team == "" || strings.HasPrefix(team, "/") {
			team = s.TeamName
		}
		if team != "" {
			return team + "/" + sessionName(s)
		}
	}
	if core.IsSessionID(s.SessionID) {
		return s.SessionID
	}
	return ""
}

// realRemoteSend sends a message to another host through the hub.
func realRemoteSend(to, text string) string {
	s, err := hub.Queue(to, "user", "fyi", text, "")
	if err != nil {
		return err.Error()
	}
	if hub.ConnectorRunning() {
		return "sent " + s.Mail.ID + "; the connector takes it to the hub"
	}
	if err := hub.FlushOnce(); err != nil {
		return "queued " + s.Mail.ID + "; " + err.Error()
	}
	return "sent " + s.Mail.ID + " to the hub"
}

type hubMsg struct{ note string }

// realRefresh fetches the roster and snapshots in one call, off the UI loop.
func realRefresh() tea.Msg {
	if hub.ConnectorRunning() {
		return hubMsg{"the connector keeps the hosts current"}
	}
	if _, err := hub.PullOnce(); err != nil {
		return hubMsg{"hub not reached: " + err.Error()}
	}
	return hubMsg{"hosts refreshed"}
}

// hostsView is the Hosts tab: the graph on the left, the selected host on
// the right.
func (m *model) hostsView() string {
	v := m.hosts
	if v == nil {
		return cBox.Width(m.w - 4).Render("This host is in no org. sunstack org join <hub> connects it (design §18).")
	}
	m.hostSel = min(max(0, m.hostSel), len(v.Hosts)-1)
	leftW := max(40, m.w*3/5)
	rightW := m.w - leftW - 4
	h := m.inner()
	title := cHeader.Render(fmt.Sprintf("Org %s", v.OrgName))
	left := append([]string{title, ""}, graph(v, m.hostSel, leftW-2, m.org)...)
	right := hostDetails(v.Hosts[m.hostSel], rightW-2, m.org)
	l := cBox.Width(leftW).Height(h).Render(strings.Join(clip(fitAll(left, leftW-2), h), "\n"))
	r := cBox.Width(rightW).Height(h).Render(strings.Join(clip(fitAll(right, rightW-2), h), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, l, r)
}

// hostsKey handles keys on the Hosts tab; false when the key is not its own.
func (m *model) hostsKey(k string) (bool, tea.Cmd) {
	if m.hosts == nil {
		return false, nil
	}
	n := len(m.hosts.Hosts)
	switch k {
	case "up", "left", "k":
		m.hostSel = (m.hostSel + n - 1) % n
	case "down", "right", "j":
		m.hostSel = (m.hostSel + 1) % n
	case "enter":
		h := m.hosts.Hosts[m.hostSel]
		m.view = viewOrg
		key := "host:" + h.ID
		delete(m.folded, key)
		m.orgSel = 0
		if !h.You {
			for i, node := range m.treeNodes() {
				if node.key == key {
					m.orgSel = i
				}
			}
		}
		if m.org == nil && !m.orgBusy {
			m.orgBusy = true
			return true, orgCmd()
		}
	case "r":
		return true, m.refreshHub
	default:
		return false, nil
	}
	return true, nil
}

// loadHosts reads the org view from the local files.
func (m *model) loadHosts() {
	if m.loadView != nil {
		m.hosts = m.loadView(time.Now())
	}
}
