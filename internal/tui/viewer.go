package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The read-only file viewer (§21.3): full screen, on bubbles' viewport and
// text input, with Markdown rendered by glamour. It shows a snapshot of the
// file taken when opened (r reloads) and never changes or runs anything.

// glamourStyle is glamour's style: dark or light from the terminal at start,
// notty in tests.
var glamourStyle = "dark"

type viewer struct {
	p       *core.Project
	files   []core.ViewFile
	note    string // above the list or the file: "this host's copy"
	list    bool   // choosing among files
	sel     int
	file    core.ViewFile
	text    *core.ViewText
	err     error
	vp      viewport.Model
	find    textinput.Model
	finding bool
	query   string
	matches []int // line numbers of the rendered text
	match   int
	plain   []string // the rendered lines without color, for find
}

// openViewer opens files of p: the file itself when there is one, else a
// short list first.
func (m *model) openViewer(p *core.Project, files []core.ViewFile, note string) {
	if p == nil || len(files) == 0 {
		m.note = "no files to show"
		return
	}
	v := &viewer{p: p, files: files, note: note, list: len(files) > 1}
	v.find = textinput.New()
	v.find.Prompt = "find: "
	v.find.CharLimit = 200
	if !v.list {
		v.open(files[0], m.w, m.viewerHeight())
	}
	m.viewer = v
}

func (m *model) viewerHeight() int { return max(3, m.h-4) } // header, rule, footer two lines

func (v *viewer) open(f core.ViewFile, w, h int) {
	v.file, v.list = f, false
	v.text, v.err = v.p.ReadView(f.Path)
	v.vp = viewport.New(w, h)
	v.query, v.matches, v.match = "", nil, 0
	v.render(w)
}

// render lays the file out for width w: Markdown through glamour, anything
// else as it is, wrapped. The text had its control characters made visible
// when it was read, before any styling.
func (v *viewer) render(w int) {
	if v.err != nil {
		v.vp.SetContent(cWarn.Render(v.err.Error()))
		v.plain = nil
		return
	}
	body := v.text.Text
	if strings.HasSuffix(strings.ToLower(v.file.Path), ".md") {
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(glamourStyle), glamour.WithWordWrap(max(20, w-2))); err == nil {
			if out, err := r.Render(body); err == nil {
				body = strings.TrimRight(out, "\n")
			}
		}
	} else {
		body = wrap(body, w)
	}
	v.vp.Width = w
	v.vp.SetContent(body)
	v.plain = strings.Split(ansi.Strip(body), "\n")
}

func (v *viewer) header(w int) string {
	if v.list {
		return fit(cTitle.Render("Files")+"  "+cDim.Render(v.note), w)
	}
	parts := []string{v.file.Rel}
	if v.text != nil {
		parts = append(parts, fmt.Sprintf("%d lines", v.text.Lines), "changed "+v.text.Modified.Format("2006-01-02 15:04"), "read "+v.text.Read.Format("15:04:05"))
		if v.text.Truncated {
			parts = append(parts, "first 1 MB only")
		}
	}
	if v.note != "" {
		parts = append(parts, v.note)
	}
	return fit(cTitle.Render(parts[0])+"  "+cDim.Render(strings.Join(parts[1:], "   ")), w)
}

func (m *model) viewerView() string {
	v := m.viewer
	w := m.w
	var body string
	if v.list {
		var rows []string
		for i, f := range v.files {
			l := fit(f.Rel, w-2)
			if i == v.sel {
				l = cSel.Render(l)
			}
			rows = append(rows, l)
		}
		body = strings.Join(clip(rows, m.viewerHeight()), "\n")
		body += strings.Repeat("\n", max(0, m.viewerHeight()-len(rows)))
	} else {
		body = v.vp.View()
	}
	var foot string
	switch {
	case v.finding:
		foot = v.find.View() + "\n" + cDim.Render("enter find  esc cancel")
	case v.list:
		foot = cDim.Render("↑↓ select  enter open  esc close") + "\n"
	default:
		hint := "j/k pgup/pgdn g/G scroll  / find  n next  r reload  esc close"
		if len(v.files) > 1 {
			hint += "  l files"
		}
		info := v.file.Writer
		if v.query != "" {
			info = fmt.Sprintf("%q: %d match(es)", v.query, len(v.matches))
		}
		foot = cDim.Render(fit(info, w)) + "\n" + cDim.Render(fit(hint, w))
	}
	return strings.Join([]string{v.header(w), cDim.Render(strings.Repeat("─", w)), body, foot}, "\n")
}

// viewerKey handles every key while the viewer is open, before the tabs do.
func (m *model) viewerKey(msg tea.KeyMsg) tea.Cmd {
	v := m.viewer
	k := msg.String()
	if v.finding {
		switch k {
		case "esc":
			v.finding = false
			v.find.Blur()
		case "enter":
			v.finding = false
			v.find.Blur()
			v.search(v.find.Value())
		default:
			var cmd tea.Cmd
			v.find, cmd = v.find.Update(msg)
			return cmd
		}
		return nil
	}
	if v.list {
		switch k {
		case "up", "k":
			v.sel = max(0, v.sel-1)
		case "down", "j":
			v.sel = min(len(v.files)-1, v.sel+1)
		case "enter":
			v.open(v.files[v.sel], m.w, m.viewerHeight())
		case "esc", "q":
			m.viewer = nil
		}
		return nil
	}
	switch k {
	case "esc", "q":
		if v.query != "" {
			v.query, v.matches = "", nil
			return nil
		}
		m.viewer = nil
	case "l":
		if len(v.files) > 1 {
			v.list = true
		}
	case "/":
		v.finding = true
		v.find.SetValue("")
		return v.find.Focus()
	case "n":
		if len(v.matches) > 0 {
			v.match = (v.match + 1) % len(v.matches)
			v.vp.SetYOffset(v.matches[v.match])
		}
	case "r":
		v.open(v.file, m.w, m.viewerHeight())
	case "g", "home":
		v.vp.GotoTop()
	case "G", "end":
		v.vp.GotoBottom()
	default:
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return cmd
	}
	return nil
}

func (v *viewer) search(q string) {
	v.query, v.matches, v.match = q, nil, 0
	if q == "" {
		return
	}
	lq := strings.ToLower(q)
	for i, l := range v.plain {
		if strings.Contains(strings.ToLower(l), lq) {
			v.matches = append(v.matches, i)
		}
	}
	if len(v.matches) > 0 {
		v.vp.SetYOffset(v.matches[0])
	}
}

// resize keeps the open file laid out for the window.
func (m *model) resizeViewer() {
	if v := m.viewer; v != nil && !v.list {
		v.vp.Height = m.viewerHeight()
		v.render(m.w)
	}
}

// treeFiles opens the files of the selected tree row: an agent's own, or its
// team's. A team on another host resolves by team ID to this host's
// checkout, never by a path from that host's snapshot.
func (m *model) treeFiles(n *treeNode) {
	if n == nil {
		return
	}
	if n.st != nil && m.p != nil {
		m.openViewer(m.p, m.p.AgentViewFiles(n.st.ID), "")
		return
	}
	if n.team == nil {
		m.note = "v shows files of a team or an agent"
		return
	}
	var p *core.Project
	note := ""
	if n.host == nil {
		p, _ = core.FindProject(n.team.Root)
	} else {
		for _, ip := range core.IndexedProjects() {
			if t, ok := ip.Team(); ok && t.ID != "" && t.ID == n.team.ID {
				p, note = ip, "this host's copy"
			}
		}
		if p == nil {
			m.note = "its files are on " + n.host.Name
			return
		}
	}
	if p == nil {
		m.note = "this team's folder is not on this host"
		return
	}
	agent := ""
	if n.agent != nil {
		agent = n.agent.ID
	} else if n.sess != nil {
		agent = n.sess.Agent
	}
	if agent != "" && p.HasAgent(agent) {
		m.openViewer(p, p.AgentViewFiles(agent), note)
		return
	}
	m.openViewer(p, p.TeamViewFiles(), note)
}
