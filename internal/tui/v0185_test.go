package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// With no teams indexed the picker draws "no teams yet" and a rule above
// its actions: a click on any row selects the action drawn there, and none
// panics (review of v0.18.4, design §45).
func TestPickerClicksWithNoTeams(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	m := newModel(nil, false)
	m.cwd = t.TempDir()
	m.w, m.h = 100, 30
	m.reload()
	if m.view != viewPicker || len(m.teams) != 0 {
		t.Fatalf("want an empty picker, got view %d with %d teams", m.view, len(m.teams))
	}
	lines := strings.Split(m.View(), "\n")
	acts := m.actions()
	if len(acts) == 0 {
		t.Fatal("no actions")
	}
	for i, a := range acts {
		row := -1
		for y, l := range lines {
			if strings.Contains(l, a.key+"  "+a.label) {
				row = y
				break
			}
		}
		if row < 0 {
			t.Fatalf("action %q not drawn", a.label)
		}
		m.pickSel = (i + 1) % len(acts) // not this one, so the click only selects
		m.Update(tea.MouseMsg{X: 4, Y: row, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if m.view != viewPicker {
			t.Fatalf("a click on %q left the picker", a.label)
		}
		if m.pickSel != i {
			t.Errorf("click on %q selected row %d, want %d", a.label, m.pickSel, i)
		}
		m.View()
	}
	// Every other row, and below the list: nothing selected out of range.
	for y := 0; y < m.h; y++ {
		m.Update(tea.MouseMsg{X: 4, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		m.view = viewPicker
		if m.pickSel < 0 || m.pickSel >= len(acts) {
			t.Fatalf("click at row %d set the selection to %d of %d", y, m.pickSel, len(acts))
		}
		m.View()
	}
}
