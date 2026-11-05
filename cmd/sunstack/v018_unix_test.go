//go:build !windows

package main

// v0.18.0 (§40): sunstack tmux panel, popup and install, against a fake tmux
// that logs its calls.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

const fakeTmux = `#!/bin/sh
echo "$*" >> "$STATE/log"
case "$*" in
  "show-options -gqv @sunstack-panel") cat "$STATE/panel" 2>/dev/null ;;
  "show-options -gqv @sunstack-side") cat "$STATE/side" 2>/dev/null ;;
  "show-options -gqv @sunstack-size") cat "$STATE/size" 2>/dev/null ;;
  "show-options -gqv "*) ;;
  "set-option -g @sunstack-panel "*) echo "$4" > "$STATE/panel" ;;
  "set-option -gu @sunstack-panel") rm -f "$STATE/panel" ;;
  "list-panes -a -F #{pane_id}	#{@sunstack-is-panel}") cat "$STATE/panes" ;;
  *"#{pane_current_path}") echo /work ;;
  "display-message -p #{window_id}	#{window_zoomed_flag}") cat "$STATE/front" ;;
  "display-message -p -t %99 #{window_id}	#{window_panes}") cat "$STATE/at" ;;
  "display-message -p -t %99 #{pane_width}	#{window_zoomed_flag}	#{window_layout}") cat "$STATE/width" 2>/dev/null || printf '52\t0\tx\n' ;;
  "set-option -g @sunstack-size "*) echo "$4" > "$STATE/size" ;;
  "display-message -p -t @5 #{window_id}	#{window_zoomed_flag}") printf '@5\t0\n' ;;
  split-window*) [ -f "$STATE/nosplit" ] && exit 1; echo %99 ;;
esac
`

func tmuxEnv(t *testing.T) ([]string, string) {
	t.Helper()
	env := isolatedEnv(t)
	dir, state := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "tmux"), []byte(fakeTmux), 0o755))
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\n%2\n"), 0o644))
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "STATE="+state, "TMUX=/tmp/fake,1,0", "TMUX_PANE=%1"), state
}

func tmuxLog(t *testing.T, state string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(state, "log"))
	os.Remove(filepath.Join(state, "log")) // no calls: no log
	return string(b)
}

func TestTmuxPanelShowsFollowsAndHides(t *testing.T) {
	env, state := tmuxEnv(t)
	dir := t.TempDir()
	r := sh(t, dir, env, "tmux", "panel")
	expect(t, r, 0, "panel")
	log := tmuxLog(t, state)
	for _, want := range []string{"split-window -d -t %1 -f -h -l 35% -b -c /work -P -F #{pane_id}", "tui --compact --org",
		"set-option -g @sunstack-panel %99", "set-option -p -t %99 @sunstack-is-panel 1", "set-hook -g session-window-changed[42] run-shell -b", "set-hook -g client-session-changed[42]", "tmux follow --window '#{window_id}'"} {
		if !strings.Contains(log, want) {
			t.Errorf("panel: no %q in\n%s", want, log)
		}
	}
	// It follows into the window in front...
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\n%2\n%99\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "front"), []byte("@2\t0\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "at"), []byte("@1\t3\n"), 0o644))
	expect(t, sh(t, dir, env, "tmux", "follow"), 0, "follow")
	// At its current width, so a width the user dragged to stays (§41.1).
	if log := tmuxLog(t, state); !strings.Contains(log, "join-pane -d -f -h -l 52 -b -s %99 -t @2") {
		t.Errorf("follow: no join-pane in\n%s", log)
	}
	// The hook names the window that changed.
	expect(t, sh(t, dir, env, "tmux", "follow", "--window", "@5"), 0, "follow --window")
	if log := tmuxLog(t, state); !strings.Contains(log, "join-pane -d -f -h -l 52 -b -s %99 -t @5") {
		t.Errorf("follow --window: %s", log)
	}
	// ...but not into a zoomed window, nor out of a window it is alone in.
	for _, c := range []struct{ front, at string }{{"@2\t1\n", "@1\t3\n"}, {"@2\t0\n", "@1\t1\n"}, {"@1\t0\n", "@1\t3\n"}} {
		must(t, os.WriteFile(filepath.Join(state, "front"), []byte(c.front), 0o644))
		must(t, os.WriteFile(filepath.Join(state, "at"), []byte(c.at), 0o644))
		expect(t, sh(t, dir, env, "tmux", "follow"), 0, "follow")
		if log := tmuxLog(t, state); strings.Contains(log, "join-pane") {
			t.Errorf("follow %q %q moved the panel:\n%s", c.front, c.at, log)
		}
	}
	// The right side.
	must(t, os.WriteFile(filepath.Join(state, "side"), []byte("right\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "front"), []byte("@2\t0\n"), 0o644))
	expect(t, sh(t, dir, env, "tmux", "follow"), 0, "follow right")
	if log := tmuxLog(t, state); !strings.Contains(log, "join-pane -d -f -h -l 52 -s %99") {
		t.Errorf("follow right: %s", log)
	}
	// A zoomed panel reports the whole window's width: its width comes from
	// the unzoomed layout, here 61 after a drag.
	must(t, os.WriteFile(filepath.Join(state, "width"), []byte("200\t1\t9557,200x50,0,0{61x50,0,0,99,138x50,62,0,1}\n"), 0o644))
	expect(t, sh(t, dir, env, "tmux", "follow", "--window", "@5"), 0, "follow zoomed panel")
	if log := tmuxLog(t, state); !strings.Contains(log, "join-pane -d -f -h -l 61 -s %99 -t @5") || strings.Contains(panelJSON(t, env), `"size": "200"`) {
		t.Errorf("zoomed panel width: %s", log)
	}
	must(t, os.Remove(filepath.Join(state, "width")))
	// The same command again hides it and drops the hooks.
	r = sh(t, dir, env, "tmux", "panel")
	expect(t, r, 0, "hide")
	log = tmuxLog(t, state)
	for _, want := range []string{"set-hook -gu session-window-changed[42]", "set-hook -gu client-session-changed[42]", "kill-pane -t %99"} {
		if !strings.Contains(log, want) {
			t.Errorf("hide: no %q in\n%s", want, log)
		}
	}
	// Hiding kept its width and that it is closed (§43).
	if j := panelJSON(t, env); !strings.Contains(j, `"size": "52"`) || strings.Contains(j, `"open"`) {
		t.Errorf("panel.json after hide: %s", j)
	}
	// A panel closed with q: follow drops the hooks and moves nothing.
	must(t, os.WriteFile(filepath.Join(state, "panel"), []byte("%99\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\n"), 0o644))
	expect(t, sh(t, dir, env, "tmux", "follow"), 0, "follow gone")
	if log := tmuxLog(t, state); !strings.Contains(log, "set-hook -gu session-window-changed[42]") || strings.Contains(log, "join-pane") {
		t.Errorf("follow after q: %s", log)
	}
}

func TestTmuxPopupAndOutsideTmux(t *testing.T) {
	env, state := tmuxEnv(t)
	expect(t, sh(t, t.TempDir(), env, "tmux", "popup", "--client", "/dev/ttys9"), 0, "popup")
	if log := tmuxLog(t, state); !strings.Contains(log, "display-popup -E -w 90% -h 85% -T  sunstack  -c /dev/ttys9 -d /work") || !strings.Contains(log, "tui --popup --org") {
		t.Errorf("popup: %s", log)
	}
	out := sh(t, t.TempDir(), append(env, "TMUX="), "tmux", "panel")
	expect(t, out, 2, "panel outside tmux")
	if !strings.Contains(out.stderr, "inside tmux") {
		t.Errorf("outside tmux: %s", out.stderr)
	}
}

func TestTmuxInstall(t *testing.T) {
	env, _ := tmuxEnv(t)
	env = append(env, "TMUX=")
	home := envValue(env, "HOME")
	conf := filepath.Join(home, ".tmux.conf")
	must(t, os.WriteFile(conf, []byte("set -g mouse on"), 0o644))
	r := sh(t, t.TempDir(), env, "tmux", "install")
	expect(t, r, 0, "install plan")
	if !strings.Contains(r.out, "--yes") || fileExists(filepath.Join(home, ".config", "sunstack", "sunstack.tmux")) {
		t.Fatalf("without --yes nothing is written: %s", r.out)
	}
	for range 2 { // twice: the source line goes in once
		expect(t, sh(t, t.TempDir(), env, "tmux", "install", "--yes"), 0, "install")
	}
	b, _ := os.ReadFile(conf)
	if got := string(b); !strings.HasPrefix(got, "set -g mouse on\n") || strings.Count(got, "source-file -q ~/.config/sunstack/sunstack.tmux\n") != 1 {
		t.Errorf("tmux.conf:\n%s", got)
	}
	// A comment or a longer path is not the source line (§41.3).
	for _, other := range []string{"# source-file -q ~/.config/sunstack/sunstack.tmux", "source-file -q ~/.config/sunstack/sunstack.tmux.backup"} {
		must(t, os.WriteFile(conf, []byte(other+"\n"), 0o644))
		expect(t, sh(t, t.TempDir(), env, "tmux", "install", "--yes"), 0, "install beside "+other)
		if b, _ := os.ReadFile(conf); !strings.HasPrefix(string(b), other+"\n") || !strings.Contains(string(b), "\nsource-file -q ~/.config/sunstack/sunstack.tmux\n") {
			t.Errorf("beside %q:\n%s", other, b)
		}
	}
	// An older install's absolute line becomes the portable one (§41.3).
	abs := "source-file -q " + core.ShellQuote(filepath.Join(home, ".config", "sunstack", "sunstack.tmux"))
	must(t, os.WriteFile(conf, []byte("set -g mouse on\n"+abs+"\nset -g x y\n"), 0o644))
	expect(t, sh(t, t.TempDir(), env, "tmux", "install", "--yes"), 0, "install over an absolute line")
	if b, _ := os.ReadFile(conf); string(b) != "set -g mouse on\nsource-file -q ~/.config/sunstack/sunstack.tmux\nset -g x y\n" {
		t.Errorf("absolute line not replaced:\n%s", b)
	}
	f, _ := os.ReadFile(filepath.Join(home, ".config", "sunstack", "sunstack.tmux"))
	for _, want := range []string{`set-hook -g session-created[42] "run-shell -b `, ` tmux restore --pane '#{pane_id}'`, `bind-key S run-shell -b "`, ` tmux panel --pane '#{pane_id}'"`, `bind-key F run-shell -b "`, `--client '#{client_name}'"`} {
		if !strings.Contains(string(f), want) {
			t.Errorf("sunstack.tmux lacks %q:\n%s", want, f)
		}
	}
}

// prefix S hides a panel whose ID the option lost, found by its pane tag;
// a pane that only looks like one is left alone (§42.1).
func TestTmuxPanelHidesWithoutOption(t *testing.T) {
	env, state := tmuxEnv(t)
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n%5\t\n%7\t1\n"), 0o644))
	r := sh(t, t.TempDir(), env, "tmux", "panel")
	expect(t, r, 0, "panel")
	if log := tmuxLog(t, state); !strings.Contains(r.out, "panel hidden") || !strings.Contains(log, "kill-pane -t %7") {
		t.Errorf("no hide of the found panel: %s\n%s", r.out, log)
	}
}

func panelJSON(t *testing.T, env []string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(envValue(env, "SUNSTACK_HOME"), "panel.json"))
	return string(b)
}

func writePanelJSON(t *testing.T, env []string, body string) {
	t.Helper()
	dir := envValue(env, "SUNSTACK_HOME")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "panel.json"), []byte(body), 0o600))
}

// The user's last choices win over the tmux options and outlive tmux
// (§43): width, side, scope, and whether it was open.
func TestPanelPrefs(t *testing.T) {
	env, state := tmuxEnv(t)
	dir := t.TempDir()
	writePanelJSON(t, env, `{"side": "right", "size": "60", "scope": "team"}`)
	expect(t, sh(t, dir, env, "tmux", "panel"), 0, "panel")
	log := tmuxLog(t, state)
	if !strings.Contains(log, "split-window -d -t %1 -f -h -l 60 -c /work") || strings.Contains(log, "--org") || strings.Contains(log, "-l 60 -b") {
		t.Errorf("prefs not used: %s", log)
	}
	if !strings.Contains(panelJSON(t, env), `"open": true`) {
		t.Errorf("open not kept: %s", panelJSON(t, env))
	}
	// Restore does nothing while a panel is open...
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n%99\t1\n"), 0o644))
	expect(t, sh(t, dir, env, "tmux", "restore", "--pane", "%1"), 0, "restore with a panel")
	if log := tmuxLog(t, state); strings.Contains(log, "split-window") {
		t.Errorf("restore opened a second panel: %s", log)
	}
	// ...and opens it after a tmux restart, when it was open.
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n"), 0o644))
	must(t, os.Remove(filepath.Join(state, "panel")))
	expect(t, sh(t, dir, env, "tmux", "restore", "--pane", "%1"), 0, "restore")
	if log := tmuxLog(t, state); !strings.Contains(log, "split-window -d -t %1") {
		t.Errorf("restore did not open it: %s", log)
	}
	// Closed on purpose: restore leaves it closed.
	writePanelJSON(t, env, `{"open": false}`)
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n"), 0o644))
	must(t, os.Remove(filepath.Join(state, "panel")))
	expect(t, sh(t, dir, env, "tmux", "restore", "--pane", "%1"), 0, "restore closed")
	if log := tmuxLog(t, state); strings.Contains(log, "split-window") {
		t.Errorf("restore opened a closed panel: %s", log)
	}
	// A new side moves the open panel: out of its window, then back in.
	must(t, os.WriteFile(filepath.Join(state, "panel"), []byte("%99\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n%99\t1\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "at"), []byte("@1\t3\n"), 0o644))
	r := sh(t, dir, env, "tmux", "side", "left")
	expect(t, r, 0, "side left")
	if log := tmuxLog(t, state); !strings.Contains(log, "break-pane -d -s %99") || !strings.Contains(log, "join-pane -d -f -h -l 52 -b -s %99 -t @1") {
		t.Errorf("side move: %s", log)
	}
	if !strings.Contains(panelJSON(t, env), `"side": "left"`) {
		t.Errorf("side not kept: %s", panelJSON(t, env))
	}
	expect(t, sh(t, dir, env, "tmux", "side", "up"), 2, "bad side")
	expect(t, sh(t, dir, env, "tmux", "scope", "org"), 0, "scope")
	if !strings.Contains(panelJSON(t, env), `"scope": "org"`) {
		t.Errorf("scope not kept: %s", panelJSON(t, env))
	}
}

// Several sessions created at once each run restore: one panel opens (§43).
func TestPanelRestoreOnce(t *testing.T) {
	p := t.TempDir()
	s := privateTmux(t, p, isolatedEnv(t))
	env := append(append([]string{}, s.env...), "SUNSTACK_TEST_DEFAULT_SOCKET="+s.socket)
	writePanelJSON(t, env, `{"open": true}`)
	s.tmux(t, "set-option", "-g", "remain-on-exit", "on") // keep a panel whose dashboard exits here
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := sh(t, p, env, "tmux", "restore", "--pane", s.pane); r.code != 0 {
				t.Errorf("restore: %d %s %s", r.code, r.out, r.stderr)
			}
		}()
	}
	wg.Wait()
	out := s.tmux(t, "list-panes", "-a", "-F", "#{@sunstack-is-panel}")
	if n := strings.Count(out, "1"); n != 1 {
		t.Errorf("%d panels after concurrent restores:\n%s", n, out)
	}
}

// A hide whose closed state cannot be saved leaves the panel up and fails,
// so restore cannot reopen a panel the user hid (§43).
func TestPanelHideNeedsSavedState(t *testing.T) {
	env, state := tmuxEnv(t)
	dir := t.TempDir()
	writePanelJSON(t, env, `{"open": true}`)
	must(t, os.WriteFile(filepath.Join(state, "panel"), []byte("%99\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\t\n%99\t1\n"), 0o644))
	held := filepath.Join(envValue(env, "SUNSTACK_HOME"), "locks", "panel-prefs")
	must(t, os.MkdirAll(held, 0o755))
	must(t, os.WriteFile(filepath.Join(held, "owner"), []byte("another sunstack\n"), 0o644))
	r := sh(t, dir, env, "tmux", "panel")
	if r.code == 0 || strings.Contains(r.out, "panel hidden") {
		t.Errorf("hide with the settings locked: %d %s", r.code, r.out)
	}
	if log := tmuxLog(t, state); strings.Contains(log, "kill-pane") {
		t.Errorf("the panel was closed anyway: %s", log)
	}
	if !strings.Contains(panelJSON(t, env), `"open": true`) {
		t.Errorf("panel.json changed: %s", panelJSON(t, env))
	}
}

// A show that tmux refuses keeps the closed state (§43).
func TestPanelFailedShowStaysClosed(t *testing.T) {
	env, state := tmuxEnv(t)
	writePanelJSON(t, env, `{"size": "70"}`)
	must(t, os.WriteFile(filepath.Join(state, "nosplit"), nil, 0o644))
	r := sh(t, t.TempDir(), env, "tmux", "panel")
	if r.code == 0 || strings.Contains(panelJSON(t, env), `"open": true`) {
		t.Errorf("failed show: %d %s", r.code, panelJSON(t, env))
	}
}

// A show whose open state cannot be saved closes the new panel again (§43).
func TestPanelShowNeedsSavedState(t *testing.T) {
	env, state := tmuxEnv(t)
	writePanelJSON(t, env, `{"size": "70"}`)
	held := filepath.Join(envValue(env, "SUNSTACK_HOME"), "locks", "panel-prefs")
	must(t, os.MkdirAll(held, 0o755))
	must(t, os.WriteFile(filepath.Join(held, "owner"), []byte("another sunstack\n"), 0o644))
	r := sh(t, t.TempDir(), env, "tmux", "panel")
	log := tmuxLog(t, state)
	if r.code == 0 || !strings.Contains(log, "kill-pane -t %99") || strings.Contains(log, "set-hook -g") || strings.Contains(panelJSON(t, env), `"open"`) {
		t.Errorf("show with the settings locked: %d %s\n%s\n%s", r.code, r.out, log, panelJSON(t, env))
	}
}
