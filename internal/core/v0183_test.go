package core

import (
	"sync"
	"testing"
)

// Changes made at once are all kept (§43).
func TestUpdatePanelPrefsKeepsEveryChange(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			UpdatePanelPrefs(func(p *PanelPrefs) {
				switch i % 3 {
				case 0:
					p.Side = "right"
				case 1:
					p.Scope = "team"
				default:
					p.Size = "50"
				}
			})
		}()
	}
	wg.Wait()
	if p := LoadPanelPrefs(); p.Side != "right" || p.Scope != "team" || p.Size != "50" {
		t.Errorf("a change was lost: %+v", p)
	}
}
