package core

import (
	"path/filepath"
	"strings"
)

// Session titles (§48). A session keeps its short name (an agent's
// <id>_<task>, a free session's label) for commands. The title the user sees
// in tmux, Claude Code and Codex says which project it belongs to and what it
// does: <project>/<who>, or <project>/<who>:<task>. A free session's who is
// its label. The user may give a free session a title of their own (--title),
// which is kept as given.

// Slug is a project's part of a session title: lowercase letters, digits and
// -, at most 20 characters.
func Slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 20 {
		s = strings.TrimRight(s[:20], "-")
	}
	if s == "" {
		s = "project"
	}
	return s
}

// Slug is the team's part of a session title: its name, else its folder's.
func (p *Project) Slug() string {
	if tf, ok := p.Team(); ok && tf.Name != "" {
		return Slug(tf.Name)
	}
	return Slug(filepath.Base(p.Root))
}

// Title is a session's default title: <project>/<who>[:<task>].
func Title(project, who, task string) string {
	t := project + "/" + who
	if task != "" {
		t += ":" + task
	}
	return t
}

// Title is the title of an agent session holding this claim on id.
func (l *Live) Title(p *Project, id string) string {
	return Title(p.Slug(), id, l.Task)
}

// ValidTitle checks a title the user gives: one line of at most 80
// characters, with no single quote (it is passed to the CLI in quotes).
func ValidTitle(t string) bool {
	return t != "" && len(t) <= 80 && !strings.ContainsAny(t, "'\n\r\t")
}
