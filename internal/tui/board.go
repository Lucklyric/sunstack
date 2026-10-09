package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The Board tab (§21.1): objectives, the key results serving them and their
// direct needs, read from the boards. Read only.

var boardFilters = []string{"all", "open", "blocked", "overdue"}

type boardState struct {
	sel, scroll int
	filter      int    // index into boardFilters
	text        string // / filter
	doneOpen    map[string]bool
	plan        []*core.ObjectiveView
	unaligned   []*boardDirective
	findings    []core.Issue
}

type boardDirective struct {
	item   core.Item
	agents []string
}

type boardRow struct {
	kind string // directive, objective, kr, need, done
	obj  *core.ObjectiveView
	kr   *core.KRView
	need *core.NeedView
	dir  *boardDirective
}

var statusGlyph = map[string]string{"done": "✓", "blocked": "■", "overdue": "!", "stale": "~", "active": "●", "planned": "○", "open": "·"}

func statusText(s string) string {
	t := statusGlyph[s] + " " + s
	switch s {
	case "done", "active":
		return cOn.Render(t)
	case "blocked", "overdue", "stale":
		return cWarn.Render(t)
	}
	return cDim.Render(t)
}

func (m *model) loadBoard() {
	if m.p == nil {
		return
	}
	now := time.Now()
	b := m.p.LoadBoards()
	m.board.plan = b.Plan(now)
	m.board.findings = m.p.Findings(now)
	m.board.unaligned = nil
	byKey := map[string]*boardDirective{}
	ids := make([]string, 0, len(b.Agents))
	for id := range b.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, d := range b.Unaligned(id) {
			if byKey[d.Key] == nil {
				byKey[d.Key] = &boardDirective{item: d}
				m.board.unaligned = append(m.board.unaligned, byKey[d.Key])
			}
			byKey[d.Key].agents = append(byKey[d.Key].agents, id)
		}
	}
	if m.board.doneOpen == nil {
		m.board.doneOpen = map[string]bool{}
	}
}

// boardMatch says whether a key result passes the filters.
func (m *model) boardMatch(k *core.KRView) bool {
	switch boardFilters[m.board.filter] {
	case "open":
		if k.Done {
			return false
		}
	case "blocked":
		if k.Status != "blocked" {
			return false
		}
	case "overdue":
		if !slices.Contains(k.Conditions, "overdue") {
			return false
		}
	}
	if t := strings.ToLower(m.board.text); t != "" {
		return strings.Contains(strings.ToLower(k.Ref()+" "+k.Text), t)
	}
	return true
}

func (m *model) boardRows() []boardRow {
	var rows []boardRow
	for _, d := range m.board.unaligned {
		rows = append(rows, boardRow{kind: "directive", dir: d})
	}
	filtered := m.board.filter != 0 || m.board.text != ""
	for _, o := range m.board.plan {
		var open, done []*core.KRView
		for _, k := range o.KRs {
			if !m.boardMatch(k) {
				continue
			}
			if k.Done {
				done = append(done, k)
			} else {
				open = append(open, k)
			}
		}
		if filtered && len(open)+len(done) == 0 {
			continue
		}
		rows = append(rows, boardRow{kind: "objective", obj: o})
		for _, k := range open {
			rows = append(rows, boardRow{kind: "kr", obj: o, kr: k})
			for i := range k.NeedViews {
				rows = append(rows, boardRow{kind: "need", obj: o, kr: k, need: &k.NeedViews[i]})
			}
		}
		if len(done) > 0 {
			rows = append(rows, boardRow{kind: "done", obj: o})
			if m.board.doneOpen[o.Key] || filtered {
				for _, k := range done {
					rows = append(rows, boardRow{kind: "kr", obj: o, kr: k})
				}
			}
		}
	}
	return rows
}

func objName(o *core.ObjectiveView) string {
	if o.Key == "" {
		return "no objective"
	}
	return o.Key + " " + o.Text
}

func (m *model) boardView() string {
	if m.p == nil {
		return ""
	}
	rows := m.boardRows()
	leftW := min(46, m.w/2) - 2
	var left []string
	for _, r := range rows {
		switch r.kind {
		case "directive":
			left = append(left, rowText(cWarn.Render(r.dir.item.Key)+" not aligned", strings.Join(r.dir.agents, ", "), leftW))
		case "objective":
			tail := o2tail(r.obj)
			left = append(left, rowText(cHeader.Render(objName(r.obj)), tail, leftW))
		case "kr":
			owner := r.kr.Owner
			left = append(left, rowText("  "+r.kr.Key+" "+owner, statusText(r.kr.Status), leftW))
		case "need":
			left = append(left, rowText("      needs "+r.need.Raw, cDim.Render(r.need.Status), leftW))
		case "done":
			mark := "▸"
			if m.board.doneOpen[r.obj.Key] {
				mark = "▾"
			}
			left = append(left, cDim.Render(fmt.Sprintf("  %s %d done", mark, r.obj.Done)))
		}
	}
	title := "Board"
	if f := boardFilters[m.board.filter]; f != "all" {
		title += ", " + f
	}
	if m.board.text != "" {
		title += ", matching " + m.board.text
	}
	if len(rows) == 0 {
		msg := "No objectives yet. The user sets them in sunstack/BOARD.md (sunstack amend)."
		if m.board.filter != 0 || m.board.text != "" {
			msg = "Nothing matches the filter (f, /)."
		}
		return m.panes(title, []string{cDim.Render("nothing to show")}, -1, []string{wrap(msg, m.w-min(46, m.w/2)-6)}, 0)
	}
	m.board.sel = min(m.board.sel, len(rows)-1)
	right := m.boardDetails(rows[m.board.sel], m.w-min(46, m.w/2)-6)

	// Warnings at the bottom: the same findings sunstack board prints.
	var warn []string
	for _, f := range m.board.findings {
		warn = append(warn, cWarn.Render("⚠ ")+f.Text)
	}
	if len(warn) > 3 {
		warn = append(warn[:3], cDim.Render(fmt.Sprintf("… %d more (n shows them ranked)", len(m.board.findings)-3)))
	}
	if len(warn) == 0 {
		return m.panes(title, left, m.board.sel, right, m.board.scroll)
	}
	box := cBox.Width(m.w - 2).Render(strings.Join(fitAll(warn, m.w-4), "\n"))
	h := max(4, m.inner()-lipgloss.Height(box))
	return lipgloss.JoinVertical(lipgloss.Left, m.panesH(title, left, m.board.sel, right, m.board.scroll, h), box)
}

func o2tail(o *core.ObjectiveView) string {
	switch o.Status {
	case "no key results", "all done":
		return cDim.Render(o.Status)
	}
	return fmt.Sprintf("%d of %d done  ", o.Done, len(o.KRs)) + statusText(o.Status)
}

func (m *model) boardDetails(r boardRow, w int) []string {
	f := func(label, value string) string { return detailField(label, value, 9, w) }
	var out []string
	switch r.kind {
	case "directive":
		d := r.dir.item
		out = append(out, cHeader.Render(d.Key+" directive"), wrap(d.Text, w), "", f("to align", strings.Join(r.dir.agents, ", ")),
			"", cDim.Render(wrap("Each agent aligns with it at its next save (board skill).", w)))
	case "objective", "done":
		o := r.obj
		out = append(out, wrap(cHeader.Render(objName(o)), w), "")
		if o.Due != "" {
			out = append(out, f("due", o.Due))
		}
		out = append(out, f("progress", fmt.Sprintf("%d of %d key results done", o.Done, len(o.KRs))), f("status", o.Status))
		if r.kind == "done" {
			out = append(out, "", cDim.Render("enter shows or hides the done key results"))
		}
	case "kr", "need":
		k := r.kr
		out = append(out, wrap(cHeader.Render(k.Ref()), w), wrap(k.Text, w), "",
			f("owner", k.Owner), f("section", k.Section), f("updated", orUnknown(k.Date)))
		if k.Due != "" {
			out = append(out, f("due", k.Due))
		}
		out = append(out, f("status", strings.Join(k.Conditions, ", ")))
		if k.Verified != "" {
			out = append(out, f("verified", k.Verified))
		}
		if len(k.NeedViews) > 0 {
			out = append(out, "", cHeader.Render("Needs"))
			for _, n := range k.NeedViews {
				target := n.Raw
				if n.Ref != "" && n.Ref != n.Raw {
					target += " (" + n.Ref + ")"
				}
				out = append(out, f(n.Status, target))
			}
		}
		hint := "enter selects " + k.Owner + " on the Team tab   v files"
		switch {
		case r.kind == "need" && r.need.Ref != "":
			hint = "enter jumps to " + r.need.Ref + "   v files"
		case r.kind == "need":
			hint = "a waiting reason, not an entry on a board"
		case k.Owner == "user":
			hint = "enter opens BOARD.md"
		}
		out = append(out, "", cDim.Render(wrap(hint, w)))
	}
	return out
}

func orUnknown(s string) string {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "unknown"
	}
	return s
}

// boardKey handles keys on the Board tab; false when the key is not its own.
func (m *model) boardKey(k string) bool {
	rows := m.boardRows()
	switch k {
	case "up", "k":
		m.board.sel, m.board.scroll = max(0, m.board.sel-1), 0
	case "down", "j":
		m.board.sel, m.board.scroll = min(max(0, len(rows)-1), m.board.sel+1), 0
	case "pgdown", "pgup":
		step := 10
		if k == "pgup" {
			step = -10
		}
		m.board.scroll = max(0, m.board.scroll+step)
	case "f":
		m.board.filter = (m.board.filter + 1) % len(boardFilters)
		m.board.sel = 0
	case "/":
		m.inputPrompt, m.inputText = "filter key results", m.board.text
		m.inputDone = func(s string) { m.board.text, m.board.sel = strings.TrimSpace(s), 0 }
	case "esc":
		if m.board.text == "" && m.board.filter == 0 {
			return false
		}
		m.board.text, m.board.filter, m.board.sel = "", 0, 0
	case "enter", "right", "left", "v":
		if len(rows) == 0 {
			return true
		}
		r := rows[min(m.board.sel, len(rows)-1)]
		if k == "v" {
			m.boardFiles(r)
			return true
		}
		switch r.kind {
		case "done":
			m.board.doneOpen[r.obj.Key] = !m.board.doneOpen[r.obj.Key]
		case "need":
			if k == "enter" && r.need.Ref != "" {
				m.boardJump(r.need.Ref)
			}
		case "kr":
			if k != "enter" {
				return true
			}
			if r.kr.Owner == "user" {
				m.openViewer(m.p, m.p.TeamViewFiles()[:1], "")
				return true
			}
			m.view = viewTeam
			for i, n := range m.teamNodes() {
				if n.st != nil && n.st.ID == r.kr.Owner {
					m.teamSel = i
				}
			}
		}
	default:
		return false
	}
	return true
}

// boardJump selects the key result ref, opening its done list if needed.
func (m *model) boardJump(ref string) {
	for _, o := range m.board.plan {
		for _, k := range o.KRs {
			if k.Ref() == ref && k.Done {
				m.board.doneOpen[o.Key] = true
			}
		}
	}
	for i, r := range m.boardRows() {
		if r.kind == "kr" && r.kr.Ref() == ref {
			m.board.sel, m.board.scroll = i, 0
			return
		}
	}
	m.note = ref + " is hidden by the filter"
}

func (m *model) boardFiles(r boardRow) {
	owner := "user"
	switch {
	case r.kind == "need" && r.need.Ref != "":
		owner = strings.SplitN(r.need.Ref, "#", 2)[0]
	case r.kr != nil:
		owner = r.kr.Owner
	}
	if owner == "user" || !m.p.HasAgent(owner) {
		m.openViewer(m.p, m.p.TeamViewFiles(), "")
		return
	}
	m.openViewer(m.p, m.p.AgentViewFiles(owner), "")
}
