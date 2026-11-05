package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// PanelPrefs are the tmux side panel's settings as the user last left them
// (§43): kept in ~/.sunstack/panel.json, so they outlive a tmux restart. A
// field left empty falls back to the tmux option of the same name (the
// user's config), then to the built-in default.
type PanelPrefs struct {
	Side  string `json:"side,omitempty"`  // left or right
	Size  string `json:"size,omitempty"`  // columns, or a percentage
	Scope string `json:"scope,omitempty"` // org or team
	Open  bool   `json:"open,omitempty"`  // shown when tmux last had it
}

var panelMu sync.Mutex

func panelPath() string { return filepath.Join(Home(), "panel.json") }

// LoadPanelPrefs reads the panel's settings; a missing or broken file is
// all defaults.
func LoadPanelPrefs() PanelPrefs {
	var p PanelPrefs
	if b, err := os.ReadFile(panelPath()); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

// UpdatePanelPrefs changes the settings with f and writes them back.
func UpdatePanelPrefs(f func(*PanelPrefs)) error {
	panelMu.Lock()
	defer panelMu.Unlock()
	p := LoadPanelPrefs()
	f(&p)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Home(), ".panel-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(b, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), panelPath())
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
