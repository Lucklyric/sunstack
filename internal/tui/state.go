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

var viewNames = map[view]string{viewTeam: "team", viewLog: "team", viewInbox: "team", viewNext: "next", viewOrg: "org"}

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
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, path) == nil {
		m.saved = s
	}
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
			}
		}
	}
	m.reload()
	return m
}
