package tui

// Clickable footer keys and header counts (§40.9): every hint in the footer
// runs its key, and a count in the Org header opens what it counts.

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// clickSpan is a clickable stretch of a line: it runs key. A key
// "show:<filter>" sets the Org tree's show filter.
type clickSpan struct {
	x0, x1 int
	key    string
}

func (s clickSpan) run(m *model) tea.Cmd {
	if f, ok := strings.CutPrefix(s.key, "show:"); ok {
		m.filterMode, m.view = indexOf(filterModes, f), viewOrg
		*m.treeSel() = 0
		return nil
	}
	return m.key(s.key)
}

// footKey is the key a footer hint's first word names, or "" for words that
// only describe keys (arrows, page keys).
func footKey(w string) string {
	switch w {
	case "↑↓", "←→", "↑↓←→", "pgup/pgdn", "":
		return ""
	}
	return w
}

// footLine draws the footer hints, the keys in the chip color, and records
// where each hint sits.
func (m *model) footLine(text string) string {
	m.footX = nil
	var b strings.Builder
	x := 0
	for i, tok := range strings.Split(text, "  ") {
		if i > 0 {
			b.WriteString(cDim.Render("  "))
			x += 2
		}
		w := ansi.StringWidth(tok)
		word, rest, _ := strings.Cut(tok, " ")
		if k := footKey(word); k != "" && strings.TrimSpace(tok) != "" {
			m.footX = append(m.footX, clickSpan{x0: x, x1: x + w, key: k})
			b.WriteString(cChip.Render(word))
			if rest != "" {
				b.WriteString(cDim.Render(" " + rest))
			}
		} else {
			b.WriteString(cDim.Render(tok))
		}
		x += w
	}
	return b.String()
}

var countRe = regexp.MustCompile(`\d+ (busy|waiting|blocked|for you)`)

// headSpans are the Org header's counts: what needs the user opens Next,
// a state count shows only those sessions.
func headSpans(head string) []clickSpan {
	plain := ansi.Strip(head)
	var out []clickSpan
	for _, loc := range countRe.FindAllStringSubmatchIndex(plain, -1) {
		x0 := ansi.StringWidth(plain[:loc[0]])
		s := clickSpan{x0: x0, x1: x0 + ansi.StringWidth(plain[loc[0]:loc[1]])}
		switch plain[loc[2]:loc[3]] {
		case "for you":
			s.key = "n"
		case "busy":
			s.key = "show:busy"
		default:
			s.key = "show:needs you"
		}
		out = append(out, s)
	}
	return out
}
