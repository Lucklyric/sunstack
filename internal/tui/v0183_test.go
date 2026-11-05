package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The line between the list and the details drags, and the width is kept
// with the other options (§43).
func TestDividerDrags(t *testing.T) {
	m := treeModel(t)
	m.View() // lays out the body, so the footer line is known
	lw := m.leftW()
	y := m.bodyTop() + 3
	m.Update(tea.MouseMsg{X: lw + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.dragging {
		t.Fatalf("a press on the divider at %d did not start a drag", lw+1)
	}
	m.Update(tea.MouseMsg{X: 61, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 61, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m.dragging || m.leftW() != 60 || m.options().Split != 60 {
		t.Errorf("after the drag: dragging %v, list %d, kept %d", m.dragging, m.leftW(), m.options().Split)
	}
	fits(t, m.View(), m.w, "dragged")
	// Each side keeps room to read.
	m.setSplit(2)
	if m.leftW() != 24 {
		t.Errorf("narrowest list %d", m.leftW())
	}
	m.setSplit(500)
	if m.leftW() != m.w-30 {
		t.Errorf("widest list %d", m.leftW())
	}
	// [ and ] move it by keys.
	m.setSplit(50)
	m.key("]")
	if m.leftW() != 54 {
		t.Errorf("] gave %d", m.leftW())
	}
	// A press elsewhere is an ordinary click.
	m.Update(tea.MouseMsg{X: 3, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.dragging {
		t.Error("a click in the list started a drag")
	}
}

// In the tmux side panel: < and > resize it, its width is kept unless
// zoomed, q keeps it closed, and the menu sets its side and scope (§43).
func TestPanelKeysAndWidth(t *testing.T) {
	m := treeModel(t)
	m.compact, m.panel = true, true
	t.Setenv("TMUX_PANE", "%99")
	var calls []string
	zoomed := "0"
	m.run = func(name string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "window_zoomed_flag") {
			return zoomed, nil
		}
		return "", nil
	}
	m.key(">")
	m.key("<")
	if got := strings.Join(calls, "|"); !strings.Contains(got, "resize-pane -t %99 -R 2") || !strings.Contains(got, "resize-pane -t %99 -L 2") {
		t.Errorf("left panel resize: %s", got)
	}
	core.UpdatePanelPrefs(func(p *core.PanelPrefs) { p.Side = "right" })
	calls = nil
	m.key(">")
	if got := strings.Join(calls, "|"); !strings.Contains(got, "resize-pane -t %99 -L 2") {
		t.Errorf("right panel grows to the left: %s", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 57, Height: 30})
	if core.LoadPanelPrefs().Size != "57" {
		t.Errorf("width not kept: %+v", core.LoadPanelPrefs())
	}
	zoomed = "1"
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
	if core.LoadPanelPrefs().Size != "57" {
		t.Errorf("zoomed width kept: %+v", core.LoadPanelPrefs())
	}
	var labels []string
	for _, it := range m.menuItems() {
		labels = append(labels, it.label)
	}
	if got := strings.Join(labels, "|"); !strings.Contains(got, "Panel on the left") || !strings.Contains(got, "Panel: this team only") {
		t.Errorf("panel menu: %s", got)
	}
	core.UpdatePanelPrefs(func(p *core.PanelPrefs) { p.Open = true })
	m.key("q")
	if core.LoadPanelPrefs().Open {
		t.Error("q left the panel marked open")
	}
}

// Help shows no divider, and a release missed outside the window does not
// leave the drag on (§43).
func TestDividerEdgeCases(t *testing.T) {
	m := treeModel(t)
	m.View()
	lw, y := m.leftW(), m.bodyTop()+3
	m.help = true
	m.Update(tea.MouseMsg{X: lw + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.dragging || m.help {
		t.Errorf("help: dragging %v, help still open %v", m.dragging, m.help)
	}
	m.Update(tea.MouseMsg{X: lw + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	before := m.leftW()
	// No release arrives; the next press is in the list.
	m.Update(tea.MouseMsg{X: 3, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(tea.MouseMsg{X: 70, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if m.dragging || m.leftW() != before {
		t.Errorf("stale drag: dragging %v, list %d (was %d)", m.dragging, m.leftW(), before)
	}
}
