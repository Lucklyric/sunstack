package tui

import (
	"os"
	"os/exec"
	"runtime"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A long agent row, long events and wide characters stay on one line each,
// inside the screen, so the boxes keep their shape.
func TestRowsFitTheScreen(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUNSTACK_HOME", filepath.Join(root, ".home"))
	t.Setenv("HOME", root)
	dir := filepath.Join(root, "sunstack")
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("PROTOCOL.md", "# Sunstack protocol\n")
	write("project-manager.lead-with-a-long-name/AGENT.md", "---\ntitle: project-manager\nfrom: custom\n---\n## Role\n"+strings.Repeat("Runs the project. ", 20)+"\n")
	// A claim on a tmux server that is gone: the row says pane closed.
	write("_local/live/project-manager.lead-with-a-long-name/aaaaaaaaaaaaaaaa.json",
		`{"tool":"claude","host":"h","token":"aaaaaaaaaaaaaaaa","claimed":"2026-10-01T00:00:00Z","last_contact":"2026-10-01T00:00:00Z","tmux_pane":"%9","tmux_socket":"`+filepath.Join(root, "gone.sock")+`","tmux_server":"1"}`)
	write("pm.lead/AGENT.md", "---\ntitle: pm\nfrom: custom\n---\n## Role\nPlans.\n")
	write("_local/live/pm.lead/bbbbbbbbbbbbbbbb.json",
		`{"tool":"claude","host":"h","token":"bbbbbbbbbbbbbbbb","claimed":"2026-10-01T00:00:00Z","last_contact":"2026-10-01T00:00:00Z","tmux_pane":"%8","tmux_socket":"`+filepath.Join(root, "gone.sock")+`","tmux_server":"1"}`)
	write("_local/log/events.log", "2026-10-06T00:00:00Z send "+strings.Repeat("问题在哪里现在 ", 30)+"\n")
	p, err := core.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{60, 100, 160} {
		m := &model{p: p, w: w, h: 30}
		m.reload()
		for _, l := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("width %d: line is %d wide: %q", w, lipgloss.Width(l), l)
			}
		}
		if n := strings.Count(m.View(), "\n") + 1; n > m.h {
			t.Errorf("width %d: %d lines on a %d-line screen", w, n, m.h)
		}
		for _, v := range []view{viewLog, viewOrg} {
			m.view = v
			for _, l := range strings.Split(m.View(), "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("width %d, view %d: line is %d wide", w, v, lipgloss.Width(l))
				}
			}
		}
		m.view = viewTeam
		// Without tmux (Windows CI) a pane cannot be checked, so it is not called gone.
		if _, err := exec.LookPath("tmux"); err == nil && w >= 100 && !strings.Contains(m.View(), "pm.lead              1 session, pane gone") {
			t.Errorf("width %d: the closed pane is not shown:\n%s", w, m.View())
		}
	}
}

func TestFitCountsWideCharacters(t *testing.T) {
	s := fit(strings.Repeat("问", 50), 20)
	if lipgloss.Width(s) > 20 || !strings.HasSuffix(s, "…") {
		t.Errorf("fit: %q is %d wide", s, lipgloss.Width(s))
	}
	if fit("short", 20) != "short" {
		t.Error("a short line is kept")
	}
}

// With --org outside a team the dashboard is the host view: team keys stay
// on it, and esc or t opens the team picker.
func TestHostOnly(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	m := newModel(nil, true)
	if m.Init() == nil {
		t.Fatal("the host view starts a scan at once")
	}
	m.w, m.h = 100, 30
	m.org = &core.Org{Host: core.HostInfo{Name: "h"}}
	m.reload()
	for _, k := range []string{"o", "l", "i", "g", "c", "up", "down", "2", "r", "n"} {
		m.Update(key(k))
		if m.view != viewOrg {
			t.Fatalf("key %q left the host view", k)
		}
		v := m.View()
		fits(t, v, m.w, k)
		if strings.Contains(v, "l log") || !strings.Contains(v, "host h") {
			t.Errorf("key %q: host view header or help:\n%s", k, v)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != viewPicker {
		t.Error("esc in the host view opens the team picker")
	}
}

// Inside a team, --org opens on the host view, and o or esc goes back.
func TestStartOnOrg(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SUNSTACK_HOME", filepath.Join(root, ".home"))
	if err := os.MkdirAll(filepath.Join(root, "sunstack"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "sunstack", "PROTOCOL.md"), []byte("# Sunstack protocol\n"), 0o644)
	p, err := core.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(p, true)
	if m.view != viewOrg || m.Init() == nil {
		t.Fatal("--org starts on the host view with a scan")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != viewTeam {
		t.Error("esc goes back to the team in a team folder")
	}
	if newModel(p, false).view != viewTeam {
		t.Error("without --org the team view comes first")
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func fits(t *testing.T, v string, w int, label string) {
	t.Helper()
	for _, l := range strings.Split(v, "\n") {
		if lipgloss.Width(l) > w {
			t.Errorf("%s: line %d wide: %q", label, lipgloss.Width(l), l)
		}
	}
}

// newTeam makes a team folder with one agent and registers it.
func newTeam(t *testing.T, root, agent string) *core.Project {
	t.Helper()
	for rel, text := range map[string]string{
		"sunstack/PROTOCOL.md":            "# Sunstack protocol\n",
		"sunstack/" + agent + "/AGENT.md": "---\ntitle: " + strings.SplitN(agent, ".", 2)[0] + "\nfrom: custom\n---\n## Role\nWorks.\n",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := core.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Register(); err != nil {
		t.Fatal(err)
	}
	return p
}

// Outside a team, the dashboard starts on a picker of this host's teams.
func TestStartPicker(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	base := t.TempDir()
	newTeam(t, filepath.Join(base, "alpha"), "builder.a")
	beta := newTeam(t, filepath.Join(base, "beta"), "builder.b")
	m := newModel(nil, false)
	m.cwd = t.TempDir()
	m.w, m.h = 100, 30
	m.reload()
	if m.view != viewPicker {
		t.Fatalf("outside a team the picker comes first, got view %d", m.view)
	}
	v := m.View()
	fits(t, v, m.w, "picker")
	requireAll(t, v, "Teams on this host", "alpha", "beta", "Host view", "Find teams under this folder")
	// The right pane shows the selected team.
	requireAll(t, v, "agents: builder.a", "sessions: 0")
	if strings.Contains(v, "Set up a team") {
		t.Error("set up is offered only in a git project")
	}
	m.Update(key("down"))
	m.Update(key("enter"))
	if m.view != viewTeam || m.p == nil || m.p.Root != beta.Root {
		t.Fatalf("enter opens the selected team, got %v", m.p)
	}
	requireAll(t, m.View(), "builder.b")
	m.Update(key("t"))
	if m.view != viewPicker {
		t.Error("t opens the team picker from a team")
	}
	m.Update(key("o"))
	if m.view != viewOrg {
		t.Error("o in the picker opens the host view")
	}
}

// s finds teams under the current folder; n sets one up in a git project
// after a yes.
func TestPickerScanAndSetUp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	cwd := t.TempDir()
	newTeam(t, filepath.Join(cwd, "nested", "gamma"), "builder.g")
	// Forget it, so only the scan can find it.
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	m := newModel(nil, false)
	m.cwd = cwd
	m.w, m.h = 100, 30
	m.reload()
	if strings.Contains(m.View(), "gamma") {
		t.Fatal("gamma is not indexed yet")
	}
	m.Update(key("s"))
	requireAll(t, m.View(), "gamma")

	if out, err := exec.Command("git", "-C", cwd, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	m.reload()
	requireAll(t, m.View(), "Set up a team in this folder")
	m.Update(key("n"))
	requireAll(t, m.View(), "Create sunstack/ in "+filepath.Base(cwd))
	m.Update(key("x"))
	if _, err := os.Stat(filepath.Join(cwd, "sunstack", "PROTOCOL.md")); err == nil {
		t.Fatal("anything but y cancels")
	}
	m.Update(key("n"))
	m.Update(key("y"))
	if _, err := os.Stat(filepath.Join(cwd, "sunstack", "PROTOCOL.md")); err != nil {
		t.Fatal("y sets up the team")
	}
	if m.view != viewTeam || m.p == nil {
		t.Error("the new team opens")
	}
}

// A team shows its tabs; n opens the ranked next list, tab cycles, ? helps.
func TestTabsNextAndHelp(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	p := newTeam(t, t.TempDir(), "builder.a")
	m := newModel(p, false)
	m.w, m.h = 100, 30
	m.reload()
	requireAll(t, m.View(), "Team", "Next", "Org")
	m.Update(key("n"))
	if m.view != viewNext {
		t.Fatal("n opens the next tab")
	}
	v := m.View()
	fits(t, v, m.w, "next")
	// A team with no board: the left lists the item, the right says what to run.
	requireAll(t, v, "1. [", "do: ", "owner: ")
	for _, want := range []view{viewOrg, viewTeam, viewNext} {
		m.Update(key("tab"))
		if m.view != want {
			t.Fatalf("tab: got view %d, want %d", m.view, want)
		}
	}
	m.Update(key("?"))
	requireAll(t, m.View(), "t  teams", "n  next", "o  host view", "q  quit")
	m.Update(key("x"))
	if m.view != viewNext {
		t.Error("any key closes help and returns")
	}
}

func requireAll(t *testing.T, s string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("missing %q in:\n%s", w, s)
		}
	}
}

// The org tab lists sections on the left and shows the selected one on the
// right.
func TestOrgTwoPanes(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	p := newTeam(t, t.TempDir(), "builder.a")
	m := newModel(p, true)
	m.w, m.h = 110, 30
	m.org = &core.Org{Host: core.HostInfo{Name: "h"}, Attention: []string{"alpha: a session waits"},
		Teams: []*core.OrgTeam{{Name: "alpha", Objectives: []core.OrgObjective{{Key: "O1", Text: "Ship it"}}}}}
	m.orgBusy = false
	v := m.View()
	fits(t, v, m.w, "org")
	requireAll(t, v, "Needs you (1)", "alpha (0)", "All sessions on h", "alpha: a session waits")
	m.Update(key("down"))
	v = m.View()
	requireAll(t, v, "O1 Ship it")
	if strings.Contains(v, "a session waits") {
		t.Errorf("the right pane shows only the selected section:\n%s", v)
	}
	if n := strings.Count(v, "\n") + 1; n > m.h {
		t.Errorf("%d lines on a %d-line screen", n, m.h)
	}
}

// The dashboard reopens on the tab and team used last, kept in
// ~/.sunstack/tui.json; a team folder still opens its own team.
func TestRemembersLastTabAndTeam(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	base := t.TempDir()
	alpha := newTeam(t, filepath.Join(base, "alpha"), "builder.a")
	beta := newTeam(t, filepath.Join(base, "beta"), "builder.b")

	m := startModel(alpha, false)
	m.w, m.h = 100, 30
	m.Update(key("n"))
	m.Update(key("q"))
	if b, err := os.ReadFile(filepath.Join(home, "tui.json")); err != nil || !strings.Contains(string(b), `"next"`) {
		t.Fatalf("state not saved: %s %v", b, err)
	}

	// Outside a team: the last team, on the last tab.
	m = startModel(nil, false)
	if m.p == nil || m.p.Root != alpha.Root || m.view != viewNext {
		t.Fatalf("outside a team: got team %v view %d", m.p, m.view)
	}
	// In another team's folder: that team, on the last tab.
	m = startModel(beta, false)
	if m.p.Root != beta.Root || m.view != viewNext {
		t.Fatalf("in beta: got %s view %d", m.p.Root, m.view)
	}
	// --org wins over the saved tab.
	if m = startModel(beta, true); m.view != viewOrg {
		t.Errorf("--org opens the host view, got %d", m.view)
	}
	// Switching team through the picker is remembered too.
	m = startModel(alpha, false)
	m.Update(key("t"))
	for i := 0; i < 5 && m.view == viewPicker; i++ {
		if m.pickSel < len(m.teams) && m.teams[m.pickSel].Root == beta.Root {
			m.Update(key("enter"))
			break
		}
		m.Update(key("down"))
	}
	m.Update(key("o"))
	m.Update(key("q"))
	if m = startModel(nil, false); m.p == nil || m.p.Root != beta.Root || m.view != viewOrg {
		t.Fatalf("after switching: got %v view %d", m.p, m.view)
	}

	// The last team is gone: the picker, with no error.
	os.RemoveAll(beta.Root)
	if m = startModel(nil, false); m.view != viewPicker || m.p != nil {
		t.Errorf("a removed team falls back to the picker, got %v view %d", m.p, m.view)
	}
	// A broken file is ignored.
	os.WriteFile(filepath.Join(home, "tui.json"), []byte("{not json"), 0o644)
	if m = startModel(nil, false); m.view != viewPicker {
		t.Errorf("a broken file falls back to the picker, got %d", m.view)
	}
	if m = startModel(alpha, false); m.view != viewTeam || m.p.Root != alpha.Root {
		t.Errorf("a broken file opens the folder's team on the team tab, got %d", m.view)
	}
}

// Saving never writes through a symlink left at the temporary name.
func TestStateSaveIgnoresSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	os.WriteFile(sentinel, []byte("keep"), 0o644)
	if err := os.Symlink(sentinel, filepath.Join(home, "tui.json.tmp")); err != nil {
		t.Fatal(err)
	}
	p := newTeam(t, t.TempDir(), "builder.a")
	m := startModel(p, false)
	m.Update(key("n"))
	if b, _ := os.ReadFile(sentinel); string(b) != "keep" {
		t.Errorf("the save wrote through the symlink: %q", b)
	}
	if s, ok := loadState(); !ok || s.View != "next" {
		t.Errorf("state not saved: %+v %v", s, ok)
	}
}

// pgdown scrolls the Next tab's detail pane, so a long item's action shows.
func TestNextDetailScrolls(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	p := newTeam(t, t.TempDir(), "builder.a")
	m := newModel(p, false)
	m.w, m.h = 90, 12
	m.view = viewNext
	m.nextItems = []core.Issue{{Kind: "user", Owner: "user", Text: strings.Repeat("a long decision to read ", 40), Do: "the action at the end"}}
	if strings.Contains(m.View(), "the action at the end") {
		t.Fatal("the test needs the action below the fold")
	}
	for i := 0; i < 3 && !strings.Contains(m.View(), "the action at the end"); i++ {
		m.Update(key("pgdown"))
	}
	if !strings.Contains(m.View(), "the action at the end") {
		t.Errorf("pgdown did not scroll the detail:\n%s", m.View())
	}
}
