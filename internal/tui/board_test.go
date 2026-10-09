package tui

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Lucklyric/sunstack/internal/core"
)

// boardTeam is a team with objectives, key results on two boards, a task and
// a reply, for the Board and Tasks tabs and the viewer.
func boardTeam(t *testing.T) (*model, *core.Project) {
	t.Helper()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	glamourStyle = "notty"
	root := t.TempDir()
	p := newTeam(t, root, "builder.a")
	today := time.Now().Format("2006-01-02")
	files := map[string]string{
		"sunstack/BOARD.md":             "# Board\n\n## Objectives\n- 2026-01-01 O1 Ship the export\n- 2026-01-01 O2 Nothing planned\n\n## User\n- " + today + " KR1 [O1] Review the draft\n\n## Directives\n- " + today + " D1 Use the new folder layout (to: all)\n\n<!-- hidden note -->\n",
		"sunstack/builder.a/board.md":   "## Now\n- " + today + " KR1 [O1] Write the exporter (needs: KR2)\n- " + today + " KR3 [O1] Late one (due: 2026-01-02)\n## Next\n- " + today + " KR2 [O1] Plan the format\n## Done\n- " + today + " KR4 [O1] Shipped the parser (verified: go test)\n",
		"sunstack/builder.a/context.md": "notes \x1b[31mred\x1b[0m here\n",
		"sunstack/_local/inbox/builder.a/20260101T100000Z-user-aaaaa1.md":   "---\nid: 20260101T100000Z-user-aaaaa1\nfrom: user\nto: builder.a\nat: 2026-01-01T10:00:00Z\ntype: task\n---\nGoal: export the data\nThe brief says FINDME.\n",
		"sunstack/_local/log/messages/20260101T090000Z-user-aaaaa0.md":      "---\nid: 20260101T090000Z-user-aaaaa0\nfrom: user\nto: builder.a\nat: 2026-01-01T09:00:00Z\ntype: task\n---\nGoal: an old one\n",
		"sunstack/_local/log/messages/20260101T091000Z-builder.a-bbbbb0.md": "---\nid: 20260101T091000Z-builder.a-bbbbb0\nfrom: builder.a\nto: user\nat: 2026-01-01T09:10:00Z\ntype: done\nreply_to: 20260101T090000Z-user-aaaaa0\n---\ndone\nverified: ran it\n",
	}
	for rel, text := range files {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(p, false)
	m.w, m.h = 100, 30
	m.reload()
	return m, p
}

func dirSum(t *testing.T, dir string) [32]byte {
	t.Helper()
	h := sha256.New()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(path)
			h.Write([]byte(path))
			h.Write(b)
		}
		return nil
	})
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func boardSelect(t *testing.T, m *model, ref string) {
	t.Helper()
	for i, r := range m.boardRows() {
		if r.kind == "kr" && r.kr.Ref() == ref {
			m.board.sel = i
			return
		}
	}
	t.Fatalf("no row %s", ref)
}

// The Board tab: objectives with progress, key results with one status,
// needs below them, unaligned directives on top, filters, and keys (§21.1).
func TestBoardTab(t *testing.T) {
	m, p := boardTeam(t)
	before := dirSum(t, p.Root)
	m.Update(key("B"))
	if m.view != viewBoard {
		t.Fatal("B opens the Board tab")
	}
	for _, w := range []int{100, 80, 60} {
		m.w = w
		v := m.View()
		fits(t, v, w, "board")
	}
	m.w = 100
	v := ansi.Strip(m.View())
	requireAll(t, v, "D1 not aligned", "O1 Ship the export", "1 of 5 done", "KR1 user", "KR1 builder.a", "■ blocked", "! overdue", "○ planned",
		"needs KR2", "▸ 1 done", "O2 Nothing planned", "no key results")

	// A need jumps to its target.
	for i, r := range m.boardRows() {
		if r.kind == "need" {
			m.board.sel = i
		}
	}
	m.Update(key("enter"))
	if r := m.boardRows()[m.board.sel]; r.kr == nil || r.kr.Ref() != "builder.a#KR2" {
		t.Errorf("enter on a need selected %+v", r)
	}
	// The done row opens.
	for i, r := range m.boardRows() {
		if r.kind == "done" {
			m.board.sel = i
		}
	}
	m.Update(key("enter"))
	requireAll(t, ansi.Strip(m.View()), "▾ 1 done", "KR4 builder.a")

	// Filters: f cycles, / matches text, esc clears.
	m.Update(key("f")) // open
	m.Update(key("f")) // blocked
	rows := m.boardRows()
	for _, r := range rows {
		if r.kind == "kr" && r.kr.Status != "blocked" {
			t.Errorf("blocked filter shows %s %s", r.kr.Ref(), r.kr.Status)
		}
	}
	m.Update(key("esc"))
	m.Update(key("/"))
	for _, k := range "parser" {
		m.Update(key(string(k)))
	}
	if m.view != viewBoard || m.board.text != "" {
		t.Fatal("typing in the filter box reached other keys")
	}
	m.Update(key("enter"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "KR4 builder.a") || strings.Contains(v, "KR2 builder.a") {
		t.Errorf("text filter:\n%s", v)
	}
	m.Update(key("esc"))

	// enter on an agent's key result selects it on the Team tab; on the
	// user's it opens BOARD.md.
	boardSelect(t, m, "builder.a#KR3")
	m.Update(key("enter"))
	if m.view != viewTeam {
		t.Error("enter on an agent's key result opens the Team tab")
	}
	m.Update(key("B"))
	boardSelect(t, m, "user#KR1")
	m.Update(key("enter"))
	if m.viewer == nil || !strings.HasSuffix(m.viewer.file.Path, "BOARD.md") {
		t.Fatal("enter on the user's key result opens BOARD.md")
	}
	m.Update(key("esc"))
	if after := dirSum(t, p.Root); after != before {
		t.Error("browsing the board changed a file")
	}
}

// The Tasks tab: open tasks by recipient with their state, a shows closed
// ones with replies, v opens the message (§21.2).
func TestTasksTab(t *testing.T) {
	m, p := boardTeam(t)
	before := dirSum(t, p.Root)
	m.Update(key("T"))
	if m.view != viewTasks {
		t.Fatal("T opens the Tasks tab")
	}
	for _, w := range []int{100, 80, 60} {
		m.w = w
		fits(t, m.View(), w, "tasks")
	}
	m.w = 100
	v := ansi.Strip(m.View())
	requireAll(t, v, "to builder.a (1)", "export the data", "no session overdue")
	if strings.Contains(v, "an old one") {
		t.Error("a closed task shown without a")
	}
	m.Update(key("down"))
	requireAll(t, ansi.Strip(m.View()), "The brief says FINDME.", "pending, no live session")
	m.Update(key("a"))
	m.tasks.sel = 0
	for i, r := range m.taskRows() {
		if r.t != nil && strings.Contains(r.t.Goal, "old one") {
			m.tasks.sel = i
		}
	}
	requireAll(t, ansi.Strip(m.View()), "done reply", "Reply 20260101T091000Z-builder.a-bbbbb0", "verified: line present")
	m.Update(key("v"))
	if m.viewer == nil || !strings.Contains(m.viewer.file.Rel, "20260101T090000Z-user-aaaaa0") {
		t.Fatal("v opens the task's message")
	}
	m.Update(key("esc"))
	if after := dirSum(t, p.Root); after != before {
		t.Error("browsing tasks moved or changed a message")
	}
}

// The viewer: a list first for several files, control characters made
// visible, find, scrolling keys kept from the tabs, and esc back (§21.3).
func TestViewer(t *testing.T) {
	m, p := boardTeam(t)
	before := dirSum(t, p.Root)
	m.Update(key("v")) // the Team tab's agent row
	if m.viewer == nil || !m.viewer.list || len(m.viewer.files) != 3 {
		t.Fatalf("v on an agent lists its files: %+v", m.viewer)
	}
	requireAll(t, ansi.Strip(m.View()), "sunstack/builder.a/AGENT.md", "sunstack/builder.a/board.md", "sunstack/builder.a/context.md")
	m.Update(key("down"))
	m.Update(key("down"))
	m.Update(key("enter"))
	v := m.View()
	if strings.Contains(v, "\x1b[31mred") {
		t.Error("a control sequence from the file reached the screen")
	}
	requireAll(t, ansi.Strip(v), "context.md", "^[[31mred", "changed by builder.a at its saves")
	fits(t, v, m.w, "viewer")

	// Keys go to the viewer: g, o and B do not reach the tabs.
	for _, k := range []string{"g", "o", "B", "G"} {
		m.Update(key(k))
	}
	if m.viewer == nil || m.view != viewTeam {
		t.Fatal("a key escaped the viewer")
	}
	m.Update(key("/"))
	for _, k := range "notes" {
		m.Update(key(string(k)))
	}
	m.Update(key("enter"))
	if len(m.viewer.matches) != 1 {
		t.Errorf("find: %d matches", len(m.viewer.matches))
	}
	m.Update(key("esc")) // clears the find
	m.Update(key("l"))
	if !m.viewer.list {
		t.Error("l returns to the file list")
	}
	m.Update(key("esc"))
	if m.viewer != nil {
		t.Error("esc closes the viewer")
	}

	// A symlink out of the team is refused and shown as such.
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("SECRET"), 0o644)
	link := filepath.Join(p.Dir, "README.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("no symlinks here")
	}
	defer os.Remove(link)
	m.openViewer(p, p.TeamViewFiles()[3:], "")
	if v := ansi.Strip(m.View()); strings.Contains(v, "SECRET") || !strings.Contains(v, "symlink") {
		t.Errorf("a linked file:\n%s", v)
	}
	m.viewer = nil
	os.Remove(link)
	if after := dirSum(t, p.Root); after != before {
		t.Error("viewing changed a file")
	}
}
