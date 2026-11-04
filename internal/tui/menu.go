package tui

// The actions menu (§40.7): right click on a row, or ".", lists what the row
// can do. An item runs the key it names, so every action keeps its own
// checks and confirmations and nothing is mouse-only.

import (
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// menuItem runs do when set, else the key it names. on marks the current
// choice in a chip's menu.
type menuItem struct {
	label, key string
	do         func()
	on         bool
	sep        bool // a divider line, not an action
}

var menuSep = menuItem{sep: true}

// chip is an option shown on the row under a list's title: its value and a
// ▾, and a click opens a menu of its choices (§40.8).
type chip struct {
	text    string
	choices []menuItem
}

var cChip = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

// chipRows draw chips within width w, on as many rows as they need, and
// record where each sits.
func (m *model) chipRows(chips []chip, w int) []string {
	var rows []string
	var b strings.Builder
	x := 0
	m.titleX = nil
	for i, c := range chips {
		t := fit(c.text, max(1, w-2)) + " ▾"
		if x > 0 && x+2+lipgloss.Width(t) > w {
			rows, x = append(rows, b.String()), 0
			b.Reset()
		}
		if x > 0 {
			b.WriteString("  ")
			x += 2
		}
		m.titleX = append(m.titleX, titleSpan{x, x + lipgloss.Width(t), len(rows), i})
		b.WriteString(cChip.Render(t))
		x += lipgloss.Width(t)
	}
	m.chipList = chips
	return append(rows, b.String())
}

// openChip opens chip i's menu under it.
func (m *model) openChip(i, x, y int) {
	if i < len(m.chipList) {
		m.openItems(m.chipList[i].choices, x, y)
	}
}

// choice is a menu item that sets an option; on marks the current one.
func choice(label string, on bool, do func()) menuItem { return menuItem{label: label, on: on, do: do} }

type menu struct {
	items      []menuItem
	sel        int
	x, y, w, h int // where the box is drawn, border included
}

// titleSpan is a clickable part of a list title: a click runs its key.
type titleSpan struct {
	x0, x1, y int // y: the chip row, from the first
	chip      int // index into the chips drawn
}

// menuItems are the actions for the selected row of the tree in front.
func (m *model) menuItems() []menuItem {
	nodes := m.treeNodes()
	sel := *m.treeSel()
	var out []menuItem
	if sel < len(nodes) {
		n := nodes[sel]
		switch n.kind {
		case "session":
			s := n.sess
			if n.host != nil {
				out = append(out, menuItem{label: "Peek at its pane", key: "p"}, menuItem{label: "Message…", key: "m"}, menuItem{label: "Start a session beside it", key: "s"})
				break
			}
			out = append(out, menuItem{label: "Go to its pane", key: "enter"}, menuItem{label: "Rename…", key: "N"}, menuItem{label: "Message…", key: "m"}, menuItem{label: "Start a session beside it", key: "s"})
			if s.SessionID != "" {
				out = append(out, menuItem{label: "Reopen in tmux", key: "R"})
			}
			if s.Agent != "" || m.isFree(s) {
				out = append(out, menuItem{label: "Close…", key: "K"})
			}
		case "agent":
			out = append(out, menuItem{label: "Start a session", key: "s"}, menuItem{label: "Files", key: "v"})
		default:
			if n.fold {
				out = append(out, menuItem{label: "Fold or unfold", key: "enter"})
			}
		}
	}
	// The row's actions above a divider, the list's options below it.
	if len(out) > 0 {
		out = append(out, menuSep)
	}
	if m.compactMode() {
		out = append(out, menuItem{label: "Details", key: "d"})
	}
	if !m.teamScope() {
		out = append(out, menuItem{label: "Order by " + orderNames[m.nextOrder()], key: "b"})
		if m.showAll {
			out = append(out, menuItem{label: "Running sessions only", key: "a"})
		} else {
			out = append(out, menuItem{label: "All sessions", key: "a"})
		}
	}
	return append(out, menuItem{label: "Show: " + filterModes[(m.filterMode+1)%len(filterModes)], key: "f"}, menuItem{label: "Filter…", key: "/"})
}

// openMenu opens the actions menu with its corner at x, y.
func (m *model) openMenu(x, y int) { m.openItems(m.menuItems(), x, y) }

func (m *model) openItems(items []menuItem, x, y int) {
	if len(items) == 0 {
		return
	}
	w := 0
	for _, it := range items {
		w = max(w, lipgloss.Width(it.label))
	}
	w += 6 // border, padding and the current-choice mark
	h := len(items) + 2
	sel := 0
	for i, it := range items {
		if it.on {
			sel = i
		}
	}
	m.menu = &menu{items: items, sel: sel, x: max(0, min(x, m.w-w)), y: max(0, min(y, m.h-h)), w: w, h: h}
}

// menuKey handles a key while the menu is open.
func (m *model) menuKey(k string) tea.Cmd {
	mu := m.menu
	switch k {
	case "up", "k":
		mu.move(-1)
	case "down", "j":
		mu.move(1)
	case "enter":
		return m.runMenu(mu.sel)
	default:
		m.menu = nil // esc or any other key closes it
	}
	return nil
}

// move steps the selection over dividers, staying inside the menu.
func (mu *menu) move(d int) {
	for i := mu.sel + d; i >= 0 && i < len(mu.items); i += d {
		if !mu.items[i].sep {
			mu.sel = i
			return
		}
	}
}

func (m *model) runMenu(i int) tea.Cmd {
	it := m.menu.items[i]
	if it.sep {
		return nil // the menu stays open
	}
	m.menu = nil
	if it.do != nil {
		it.do()
		m.setSel(0) // the list in front changed
		return nil
	}
	return m.key(it.key)
}

// menuMouse handles a press while the menu is open: a click on an item runs
// it, a click outside closes the menu.
func (m *model) menuMouse(e tea.MouseMsg) tea.Cmd {
	mu := m.menu
	in := e.X >= mu.x && e.X < mu.x+mu.w && e.Y > mu.y && e.Y < mu.y+mu.h-1
	switch e.Button {
	case tea.MouseButtonWheelUp:
		return m.menuKey("up")
	case tea.MouseButtonWheelDown:
		return m.menuKey("down")
	case tea.MouseButtonLeft:
		if in {
			return m.runMenu(e.Y - mu.y - 1)
		}
	}
	m.menu = nil
	return nil
}

// withMenu draws the open menu over the screen.
func (m *model) withMenu(screen string) string {
	mu := m.menu
	var rows []string
	for i, it := range mu.items {
		if it.sep {
			rows = append(rows, cDim.Render(strings.Repeat("─", mu.w-4)))
			continue
		}
		mark := "  "
		if it.on {
			mark = "● "
		}
		l := mark + fit(it.label, mu.w-6)
		l += strings.Repeat(" ", max(0, mu.w-4-lipgloss.Width(l)))
		if i == mu.sel {
			l = cSel.Render(l)
		}
		rows = append(rows, l)
	}
	box := strings.Split(cBox.BorderForeground(lipgloss.Color("12")).Render(strings.Join(rows, "\n")), "\n")
	lines := strings.Split(screen, "\n")
	for i, b := range box {
		y := mu.y + i
		for len(lines) <= y {
			lines = append(lines, "")
		}
		l := lines[y]
		lw := ansi.StringWidth(l)
		left := ansi.Truncate(l, mu.x, "")
		left += strings.Repeat(" ", max(0, mu.x-ansi.StringWidth(left)))
		right := ""
		if end := mu.x + lipgloss.Width(b); end < lw {
			right = ansi.Cut(l, end, lw)
		}
		lines[y] = left + "\x1b[0m" + b + right
	}
	return strings.Join(lines, "\n")
}

// listTop is the screen line of the list's first row, or -1 with no list.
func (m *model) listTop() int {
	if m.listN == 0 {
		return -1
	}
	return m.listY0
}

// bodyTop is the screen line where the body starts, under the title and tabs.
func (m *model) bodyTop() int {
	if m.showTabs() {
		return 2
	}
	return 1
}

// renameKey renames the selected session through its CLI's /rename, or,
// when it cannot be typed now, goes to its pane to rename it there.
func (m *model) renameKey(n *treeNode) {
	if n == nil || n.kind != "session" || n.host != nil {
		m.note = "select a session on this host to rename"
		return
	}
	s := n.sess
	m.inputPrompt, m.inputText = "rename "+sessionName(s)+" to", ""
	m.inputDone = func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		// Typing into the pane takes about a second: off the UI's loop.
		m.note = "renaming " + sessionName(s) + "…"
		rename := m.rename
		m.pending = func() tea.Msg { return renameMsg{s, text, rename(s, text)} }
	}
}

type renameMsg struct {
	s    *core.HostSession
	name string
	err  error
}

// renamed reports a rename, or goes to the pane to rename it there.
func (m *model) renamed(r renameMsg) {
	if r.err == nil {
		m.note = "renamed " + sessionName(r.s) + " to " + r.name + " (the next scan shows it)"
		return
	}
	if go2 := m.goToSession(r.s); strings.HasPrefix(go2, "switched") {
		m.note = "could not rename here (" + r.err.Error() + "): type /rename in its pane"
	} else {
		m.note = "could not rename: " + r.err.Error()
	}
}

// treeChips are the session tree's options: order, which sessions, the
// show filter and the text filter.
func (m *model) treeChips() []chip {
	var out []chip
	if !m.teamScope() {
		var orders []menuItem
		for i, name := range orderNames {
			if i == orderHost && m.hosts == nil {
				continue
			}
			orders = append(orders, choice("by "+name, m.order == i, func() { m.order = i }))
		}
		out = append(out, chip{"by " + orderNames[m.order], orders})
		text := "running only"
		if m.showAll {
			text = "all sessions"
		}
		out = append(out, chip{text, []menuItem{
			choice("running only", !m.showAll, func() { m.showAll = false }),
			choice("all sessions", m.showAll, func() { m.showAll = true }),
		}})
	}
	var shows []menuItem
	for i, f := range filterModes {
		shows = append(shows, choice(f, m.filterMode == i, func() { m.filterMode = i }))
	}
	out = append(out, chip{"show: " + filterModes[m.filterMode], shows})
	return append(out, m.textChip(m.filter, func() { m.filter = "" }))
}

// textChip is a text filter's chip: type one, or clear it.
func (m *model) textChip(text string, clear func()) chip {
	items := []menuItem{{label: "Filter…", key: "/"}}
	if text == "" {
		return chip{"filter", items}
	}
	return chip{"filter: " + text, append(items, choice("Clear filter", false, clear))}
}
