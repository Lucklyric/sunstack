package core

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The read-only file viewer's side in core (§21.3): which files an item may
// show, and reading one safely. Nothing here creates or changes a file.

// ViewLimit is how much of a file the viewer reads.
const ViewLimit = 1 << 20

// ViewFile is one file the viewer may open, with who changes it.
type ViewFile struct {
	Path   string // absolute
	Rel    string // relative to the project root
	Writer string // a hint for the footer
}

// TeamViewFiles are the team's own files.
func (p *Project) TeamViewFiles() []ViewFile {
	return p.viewFiles([]string{"BOARD.md", "PILLARS.md", "PROTOCOL.md", "README.md"}, p.Dir, map[string]string{
		"BOARD.md":   "objectives change through sunstack amend, the User section with the user",
		"PILLARS.md": "changed through sunstack amend",
	}, "changed by hand")
}

// AgentViewFiles are an agent's files; its pillars file only when it has one.
func (p *Project) AgentViewFiles(id string) []ViewFile {
	names := []string{"AGENT.md", "board.md", "context.md"}
	if _, err := os.Lstat(filepath.Join(p.AgentDir(id), "pillars.md")); err == nil {
		names = append(names, "pillars.md")
	}
	return p.viewFiles(names, p.AgentDir(id), map[string]string{
		"AGENT.md":   "changed through sunstack amend",
		"pillars.md": "changed through sunstack amend",
	}, "changed by "+id+" at its saves")
}

// MessageViewFile is a message of this team by ID, wherever it is now.
func (p *Project) MessageViewFile(id string) (ViewFile, bool) {
	m := p.findMessage(id)
	if m == nil || m.path == "" {
		return ViewFile{}, false
	}
	return p.viewFile(m.path, "a message is never edited; it moves when taken or acked"), true
}

func (p *Project) viewFiles(names []string, dir string, writers map[string]string, other string) []ViewFile {
	var out []ViewFile
	for _, n := range names {
		w := writers[n]
		if w == "" {
			w = other
		}
		out = append(out, p.viewFile(filepath.Join(dir, n), w))
	}
	return out
}

func (p *Project) viewFile(path, writer string) ViewFile {
	rel, err := filepath.Rel(p.Root, path)
	if err != nil {
		rel = path
	}
	return ViewFile{Path: path, Rel: rel, Writer: writer}
}

// ViewText is a file as the viewer shows it.
type ViewText struct {
	Text      string // control characters made visible
	Lines     int
	Truncated bool // only the first ViewLimit bytes
	Modified  time.Time
	Read      time.Time
}

// ReadView reads a file of this team for the viewer. It refuses paths
// outside the team's sunstack folder and any symlink on the way, and a
// missing file is an error, never created.
func (p *Project) ReadView(path string) (*ViewText, error) {
	rel, err := filepath.Rel(p.Dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fail(ExitFail, "outside", "%s is outside the team's sunstack folder", path)
	}
	if err := p.noSymlink(path); err != nil {
		return nil, err
	}
	st, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return nil, fail(ExitFail, "not_found", "%s does not exist", rel)
	case err != nil:
		return nil, fail(ExitFail, "fs", "%v", err)
	case !st.Mode().IsRegular():
		return nil, fail(ExitFail, "fs", "%s is not a regular file", rel)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, ViewLimit+1))
	if err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	v := &ViewText{Modified: st.ModTime(), Read: time.Now()}
	if len(b) > ViewLimit {
		b, v.Truncated = b[:ViewLimit], true
	}
	v.Text = VisibleControls(strings.ReplaceAll(string(b), "\r\n", "\n"))
	v.Lines = strings.Count(v.Text, "\n")
	if v.Text != "" && !strings.HasSuffix(v.Text, "\n") {
		v.Lines++
	}
	return v, nil
}

// VisibleControls replaces terminal control characters, apart from newline
// and tab, with their visible ^X form, so a file cannot drive the terminal.
func VisibleControls(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20:
			b.WriteByte('^')
			b.WriteRune(r + '@')
		case r == 0x7f:
			b.WriteString("^?")
		case r >= 0x80 && r < 0xa0:
			b.WriteString("^[" + string(rune(r-0x40)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
