package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The dashboard reopens where it was left: the last tab and team, kept in
// ~/.sunstack/tui.json. It is a convenience: a missing, broken or stale file
// only means the usual start.

type savedState struct {
	View string `json:"view"` // team, next or org
	Team string `json:"team"` // the team's root, or "" for the host view
}

var viewNames = map[view]string{viewTeam: "team", viewLog: "team", viewInbox: "team", viewNext: "next", viewOrg: "org", viewHosts: "hosts"}

func statePath() string { return filepath.Join(core.Home(), "tui.json") }

func loadState() (savedState, bool) {
	var s savedState
	b, err := os.ReadFile(statePath())
	if err != nil || json.Unmarshal(b, &s) != nil {
		return savedState{}, false
	}
	return s, true
}

// saveState records the current tab and team. The picker is not a place to
// come back to, so it is not saved.
func (m *model) saveState() {
	name, ok := viewNames[m.view]
	if !ok {
		return
	}
	s := savedState{View: name}
	if m.p != nil {
		s.Team = m.p.Root
	}
	if s == m.saved {
		return
	}
	b, _ := json.Marshal(s)
	path := statePath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	// A fresh temporary file of our own: never one left at a fixed name, and
	// never shared with another dashboard saving at the same time.
	f, err := os.CreateTemp(filepath.Dir(path), ".tui-*.json")
	if err != nil {
		return
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), path) != nil {
		os.Remove(f.Name())
		return
	}
	m.saved = s
}

// startModel opens the dashboard: the folder's team (or, outside a team, the
// last team) on the last tab, or the host view with org.
func startModel(p *core.Project, org bool) *model {
	s, ok := loadState()
	if p == nil && ok && s.Team != "" {
		if last, err := core.FindProject(s.Team); err == nil && last.Root == s.Team {
			p = last
		}
	}
	m := newModel(p, org)
	if ok {
		m.saved = s
		if !org && p != nil {
			switch s.View {
			case "next":
				m.view = viewNext
			case "org":
				m.view, m.orgBusy = viewOrg, true
			case "hosts":
				m.view = viewHosts
			}
		}
	}
	m.reload()
	if m.view == viewHosts && m.hosts == nil {
		m.view = viewTeam // this host left its org
	}
	return m
}
