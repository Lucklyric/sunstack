package tui

import (
	"strings"
	"testing"
)

// The side panel marks the row of the session in the pane the user is in
// (§41.2), and follows the focus on each tick.
func TestPanelMarksFocusedSession(t *testing.T) {
	m := treeModel(t)
	m.showAll = true
	marked := func() []string {
		var rows []string
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, "▶ ") {
				rows = append(rows, l)
			}
		}
		return rows
	}
	if r := marked(); len(r) != 0 {
		t.Fatalf("no focus, yet marked: %q", r)
	}
	focus := "/tmp/tmux-1/default\t%9"
	m.focus = func() string { return focus }
	m.Update(tick{})
	if r := marked(); len(r) != 1 || !strings.Contains(r[0], "pm.lead_cloud") {
		t.Errorf("focus on %%9 should mark pm.lead_cloud: %q", r)
	}
	focus = "/tmp/tmux-1/default\t%10"
	m.Update(tick{})
	if r := marked(); len(r) != 1 || !strings.Contains(r[0], "scratch") {
		t.Errorf("focus on %%10 should mark scratch: %q", r)
	}
	focus = "/tmp/tmux-1/default\t%77"
	m.Update(tick{})
	if r := marked(); len(r) != 0 {
		t.Errorf("a pane with no session marked a row: %q", r)
	}
}
