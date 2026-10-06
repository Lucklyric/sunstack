package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
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
