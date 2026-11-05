package tui

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lucklyric/sunstack/internal/core"
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

// Rows from the cache are to look at only, until the first scan (§42.2).
func TestCachedRowsAreNotActionable(t *testing.T) {
	m := treeModel(t)
	m.showAll, m.orgCached = true, true
	killed := 0
	m.kill = func(*core.HostSession) string { killed++; return "closed" }
	s := m.org.Teams[0].Free[0]
	if got := m.goToSession(s); got != stillCached {
		t.Errorf("jump from the cache: %q", got)
	}
	selectRow(t, m, "pm.lead_cloud")
	m.key("K")
	if m.confirmDo == nil {
		t.Fatal("no confirmation for close")
	}
	m.confirmDo()
	if killed != 0 || m.note != stillCached {
		t.Errorf("close from the cache ran: %d %q", killed, m.note)
	}
	// The first scan drops a prompt opened on a cached row.
	m.key("K")
	m.Update(orgMsg{treeOrg()})
	if m.orgCached || m.confirmDo != nil || m.confirm != "" {
		t.Errorf("prompt kept after the scan: %v %q", m.orgCached, m.confirm)
	}
}

// Saves that overlap leave a whole cache, readable only by the user.
func TestOrgCacheConcurrentSaves(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); saveOrgCache(treeOrg()) }()
	}
	wg.Wait()
	if loadOrgCache(time.Now()) == nil {
		t.Fatal("cache not readable after overlapping saves")
	}
	if st, _ := os.Stat(orgCachePath()); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(orgCachePath()), ".org-cache-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}
