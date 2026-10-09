package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The Tasks tab (§21.2): the delegation ledger, open tasks first, grouped by
// recipient. Read only: it never requeues or moves a message.

type tasksState struct {
	sel, scroll int
	all         bool // a: closed tasks too
	list        []*core.TaskDetail
}

type taskRow struct {
	to string // a group heading when t is nil
	n  int
	t  *core.TaskDetail
}

func (m *model) loadTasks() {
	if m.p != nil {
		m.tasks.list = m.p.TaskDetails(time.Now())
	}
}

func (m *model) taskRows() []taskRow {
	groups := map[string][]*core.TaskDetail{}
	for _, t := range m.tasks.list {
		if t.Open || m.tasks.all {
			groups[t.To] = append(groups[t.To], t)
		}
	}
	tos := make([]string, 0, len(groups))
	for to := range groups {
		tos = append(tos, to)
	}
	// Recipients with open tasks first, then by name.
	open := func(to string) bool {
		for _, t := range groups[to] {
			if t.Open {
				return true
			}
		}
		return false
	}
	sort.Slice(tos, func(i, j int) bool {
		if open(tos[i]) != open(tos[j]) {
			return open(tos[i])
		}
		return tos[i] < tos[j]
	})
	var rows []taskRow
	for _, to := range tos {
		ts := groups[to]
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].Open && !ts[j].Open })
		rows = append(rows, taskRow{to: to, n: len(ts)})
		for _, t := range ts {
			rows = append(rows, taskRow{to: to, t: t})
		}
	}
	return rows
}

func taskAge(at string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", at)
	if err != nil {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func taskGoal(t *core.TaskDetail) string {
	if g := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t.Goal, "Goal:"), "goal:")); g != "" {
		return g
	}
	return t.ID
}

// lifecycleText is a task's state in a few words, for its row; the details
// pane has it in full.
func lifecycleText(t *core.TaskDetail) string {
	s := t.Lifecycle
	switch {
	case strings.HasPrefix(s, "taken by"):
		s = "taken"
	case s == "taken, session gone":
		s = "session gone"
	case s == "pending, no live session":
		s = "no session"
	case s == "done reply":
		s = "replied"
	}
	switch {
	case len(t.Findings) > 0:
		return cWarn.Render(s + " " + strings.Join(t.Findings, " "))
	case s == "taken" || s == "replied":
		return cOn.Render(s)
	case s == "session gone" || s == "no session":
		return cWarn.Render(s)
	}
	return cDim.Render(s)
}

func (m *model) tasksView() string {
	if m.p == nil {
		return ""
	}
	rows := m.taskRows()
	title := "Open tasks"
	if m.tasks.all {
		title = "All tasks"
	}
	rightW := m.w - min(46, m.w/2) - 6
	if len(rows) == 0 {
		msg := "No open tasks. Agents hand work to each other with the message skill (type task)."
		if !m.tasks.all {
			msg += " a shows closed ones."
		}
		return m.panes(title, []string{cDim.Render("nothing open")}, -1, []string{wrap(msg, rightW)}, 0)
	}
	leftW := min(46, m.w/2) - 2
	var left []string
	for _, r := range rows {
		if r.t == nil {
			left = append(left, cHeader.Render(fmt.Sprintf("to %s (%d)", r.to, r.n)))
			continue
		}
		left = append(left, rowText("  "+taskGoal(r.t), taskAge(r.t.At)+"  "+lifecycleText(r.t), leftW))
	}
	m.tasks.sel = min(m.tasks.sel, len(rows)-1)
	return m.panes(title, left, m.tasks.sel, m.taskDetails(rows[m.tasks.sel], rightW), m.tasks.scroll)
}

func (m *model) taskDetails(r taskRow, w int) []string {
	f := func(label, value string) string { return detailField(label, value, 9, w) }
	if r.t == nil {
		return []string{cHeader.Render("Tasks to " + r.to), "", fmt.Sprintf("%d task(s) listed", r.n)}
	}
	t := r.t
	out := []string{wrap(cHeader.Render(taskGoal(t)), w), "",
		f("id", t.ID), f("from", t.From), f("to", t.To), f("sent", t.At), f("state", t.Lifecycle)}
	if len(t.Findings) > 0 {
		out = append(out, cWarn.Render(f("findings", strings.Join(t.Findings, ", "))))
	}
	if t.Follows != "" {
		follows := t.Follows
		for _, o := range m.tasks.list {
			if o.ID == t.Follows {
				follows += " (" + taskGoal(o) + ")"
			}
		}
		out = append(out, f("follows", follows))
	}
	out = append(out, "", cHeader.Render("Task"), core.VisibleControls(strings.TrimRight(t.Body, "\n")))
	for _, rp := range t.Replies {
		v := "no verified: line"
		if rp.Verified {
			v = "verified: line present"
		}
		out = append(out, "", cHeader.Render("Reply "+rp.ID), cDim.Render(rp.From+", "+v), core.VisibleControls(strings.TrimRight(rp.Body, "\n")))
	}
	out = append(out, "", cDim.Render("v opens the message file   a "+map[bool]string{true: "open only", false: "closed too"}[m.tasks.all]))
	var wrapped []string
	for _, l := range out {
		wrapped = append(wrapped, wrap(l, w))
	}
	return wrapped
}

// tasksKey handles keys on the Tasks tab; false when the key is not its own.
func (m *model) tasksKey(k string) bool {
	rows := m.taskRows()
	switch k {
	case "up", "k":
		m.tasks.sel, m.tasks.scroll = max(0, m.tasks.sel-1), 0
	case "down", "j":
		m.tasks.sel, m.tasks.scroll = min(max(0, len(rows)-1), m.tasks.sel+1), 0
	case "pgdown", "pgup":
		step := 10
		if k == "pgup" {
			step = -10
		}
		m.tasks.scroll = max(0, m.tasks.scroll+step)
	case "a":
		m.tasks.all, m.tasks.sel = !m.tasks.all, 0
	case "v", "enter":
		if len(rows) == 0 || rows[min(m.tasks.sel, len(rows)-1)].t == nil {
			return true
		}
		t := rows[min(m.tasks.sel, len(rows)-1)].t
		if f, ok := m.p.MessageViewFile(t.ID); ok {
			m.openViewer(m.p, []core.ViewFile{f}, "")
		} else {
			m.note = "the message " + t.ID + " is no longer on disk"
		}
	default:
		return false
	}
	return true
}
