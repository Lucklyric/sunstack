//go:build !windows

package main

// v0.18.0 (§40): sunstack tmux panel, popup and install, against a fake tmux
// that logs its calls.

import (
	"os"
	"path/filepath"
	"strings"
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
  "list-panes -a -F #{pane_id}	#{pane_start_command}") cat "$STATE/panes" ;;
  *"#{pane_current_path}") echo /work ;;
  "display-message -p #{window_id}	#{window_zoomed_flag}") cat "$STATE/front" ;;
  "display-message -p -t %99 #{window_id}	#{window_panes}") cat "$STATE/at" ;;
  "display-message -p -t %99 #{pane_width}	#{window_zoomed_flag}	#{window_layout}") cat "$STATE/width" 2>/dev/null || printf '52\t0\tx\n' ;;
  "set-option -g @sunstack-size "*) echo "$4" > "$STATE/size" ;;
  "display-message -p -t @5 #{window_id}	#{window_zoomed_flag}") printf '@5\t0\n' ;;
  split-window*) echo %99 ;;
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
	must(t, os.Remove(filepath.Join(state, "log")))
	return string(b)
}

func TestTmuxPanelShowsFollowsAndHides(t *testing.T) {
	env, state := tmuxEnv(t)
	dir := t.TempDir()
	r := sh(t, dir, env, "tmux", "panel")
	expect(t, r, 0, "panel")
	log := tmuxLog(t, state)
	for _, want := range []string{"split-window -d -t %1 -f -h -l 35% -b -c /work -P -F #{pane_id}", "tui --compact --org",
		"set-option -g @sunstack-panel %99", "set-hook -g session-window-changed[42] run-shell -b", "set-hook -g client-session-changed[42]", "tmux follow --window '#{window_id}'"} {
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
	if log := tmuxLog(t, state); !strings.Contains(log, "join-pane -d -f -h -l 61 -s %99 -t @5") || strings.Contains(log, "@sunstack-size 200") {
		t.Errorf("zoomed panel width: %s", log)
	}
	must(t, os.Remove(filepath.Join(state, "width")))
	// The same command again hides it and drops the hooks.
	r = sh(t, dir, env, "tmux", "panel")
	expect(t, r, 0, "hide")
	log = tmuxLog(t, state)
	for _, want := range []string{"set-option -g @sunstack-size 52", "set-hook -gu session-window-changed[42]", "set-hook -gu client-session-changed[42]", "kill-pane -t %99"} {
		if !strings.Contains(log, want) {
			t.Errorf("hide: no %q in\n%s", want, log)
		}
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
	for _, want := range []string{`bind-key S run-shell -b "`, ` tmux panel --pane '#{pane_id}'"`, `bind-key F run-shell -b "`, `--client '#{client_name}'"`} {
		if !strings.Contains(string(f), want) {
			t.Errorf("sunstack.tmux lacks %q:\n%s", want, f)
		}
	}
}

// prefix S hides a panel whose ID the option lost, found by how it was
// started (§42.1).
func TestTmuxPanelHidesWithoutOption(t *testing.T) {
	env, state := tmuxEnv(t)
	must(t, os.WriteFile(filepath.Join(state, "panes"), []byte("%1\tzsh\n%7\t/opt/dev/sunstack tui --compact --org\n"), 0o644))
	r := sh(t, t.TempDir(), env, "tmux", "panel")
	expect(t, r, 0, "panel")
	if log := tmuxLog(t, state); !strings.Contains(r.out, "panel hidden") || !strings.Contains(log, "kill-pane -t %7") {
		t.Errorf("no hide of the found panel: %s\n%s", r.out, log)
	}
}

func TestPanelCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/Users/a/.local/bin/sunstack tui --compact --org": true,
		"'/Users/a b/sunstack-dev' tui --compact":          true,
		"vim -- /tmp/sunstack-notes tui --compact":         false,
		"/usr/bin/sunstack tui --compact --org extra":      false,
		"/usr/bin/sunstack tui --popup --org":              false,
		"'/Users/a b/sunstack tui --compact":               false,
		"/opt/notsunstack/vim tui --compact":               false,
	} {
		if got := panelCommand(cmd); got != want {
			t.Errorf("panelCommand(%q) = %v", cmd, got)
		}
	}
}
