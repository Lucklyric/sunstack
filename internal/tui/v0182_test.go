package tui

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// In a popup, the prefix and F (or S) close it; the prefix then another key
// is an ordinary key (§42.1).
func TestPopupClosesOnPrefixChord(t *testing.T) {
	m := treeModel(t)
	m.popup, m.prefix = true, prefixKey("C-b")
	if m.prefix != "ctrl+b" || prefixKey("C-Space") != "" || prefixKey("M-a") != "" {
		t.Fatalf("prefix names: %q", m.prefix)
	}
	ctrlB := tea.KeyMsg{Type: tea.KeyCtrlB}
	key := func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
	quits := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	_, cmd := m.Update(ctrlB)
	if quits(cmd) || !m.armed {
		t.Fatal("the prefix alone should only arm")
	}
	if _, cmd = m.Update(key('F')); !quits(cmd) {
		t.Error("prefix then F should close the popup")
	}
	m.armed = false
	m.Update(ctrlB)
	if _, cmd = m.Update(key('x')); quits(cmd) || m.armed {
		t.Error("prefix then x should not close it")
	}
	if _, cmd = m.Update(key('S')); quits(cmd) {
		t.Error("S without the prefix should not close it")
	}
}

// The last scan is kept and read back while fresh (§42.2).
func TestOrgCache(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	if loadOrgCache(time.Now()) != nil {
		t.Fatal("no file, yet a cache")
	}
	saveOrgCache(treeOrg())
	o := loadOrgCache(time.Now())
	if o == nil || len(o.Teams) != len(treeOrg().Teams) || o.Teams[0].Agents[0].Sessions[0].Pane != "%9" {
		t.Fatalf("round trip: %+v", o)
	}
	if loadOrgCache(time.Now().Add(2*time.Hour)) != nil {
		t.Error("an old cache was used")
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(orgCachePath(), []byte("{broken"), 0o600))
	if loadOrgCache(time.Now()) != nil {
		t.Error("a broken cache was used")
	}
}
