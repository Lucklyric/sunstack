package main

// sunstack tmux (§40): the dashboard as a side panel that follows the user
// to every tmux window and session, or as a floating popup, and the key
// bindings that open them.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The tmux user options the commands read, with their defaults.
const (
	optSide  = "@sunstack-side"  // left or right
	optSize  = "@sunstack-size"  // the panel's width: columns or a percentage
	optScope = "@sunstack-scope" // org (every team) or team (the folder's team)
	optPanel = "@sunstack-panel" // set by sunstack: the panel's pane ID
)

// panelHook is the index the follow hooks use in tmux's hook arrays, so the
// user's own hooks on the same events stay as they are.
const panelHook = "[42]"

var panelHooks = []string{"session-window-changed" + panelHook, "client-session-changed" + panelHook}

// tmuxOut runs tmux and returns its output without the final newline.
func tmuxOut(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

func tmuxOpt(name, def string) string {
	if v, err := tmuxOut("show-options", "-gqv", name); err == nil && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func runTmux(rest []string, stdout io.Writer) error {
	sub := ""
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	a, err := parse(rest, "pane client window", "yes")
	if err != nil {
		return err
	}
	if err := a.atMost(0, "tmux "+sub); err != nil {
		return err
	}
	switch sub {
	case "panel":
		return panelToggle(a.flags["pane"], stdout)
	case "follow":
		return panelFollow(a.flags["window"])
	case "popup":
		return popup(a.flags["pane"], a.flags["client"])
	case "install":
		return tmuxInstall(a.has("yes"), stdout)
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "usage: sunstack tmux panel|popup|install [--yes]"}
}

func needTmux() error {
	if os.Getenv("TMUX") == "" {
		return &core.Error{Code: core.ExitUsage, Reason: "no_tmux", Msg: "run this inside tmux"}
	}
	return nil
}

// selfCmd is this binary with arguments, quoted for the shell tmux runs.
func selfCmd(args ...string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "sunstack"
	}
	parts := []string{core.ShellQuote(exe)}
	for _, a := range args {
		parts = append(parts, core.ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

// tmuxDQ quotes a value in tmux's double quotes, as a config file and a
// hook command parse them.
func tmuxDQ(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(v) + `"`
}

// tuiArgs are the dashboard's flags for the scope option.
func tuiArgs(mode string) []string {
	args := []string{"tui", mode}
	if tmuxOpt(optScope, "org") != "team" {
		args = append(args, "--org")
	}
	return args
}

// livePanel is the panel's pane ID while that pane still exists.
func livePanel() string {
	id := tmuxOpt(optPanel, "")
	if id == "" {
		return ""
	}
	panes, _ := tmuxOut("list-panes", "-a", "-F", "#{pane_id}")
	for _, p := range strings.Split(panes, "\n") {
		if p == id {
			return id
		}
	}
	return ""
}

// unhookPanel drops the follow hooks and the panel's option.
func unhookPanel() {
	for _, h := range panelHooks {
		tmuxOut("set-hook", "-gu", h)
	}
	tmuxOut("set-option", "-gu", optPanel)
}

// splitArgs place a full-height pane on the panel's side at size.
func splitArgs(size string) []string {
	args := []string{"-f", "-h", "-l", size}
	if tmuxOpt(optSide, "left") != "right" {
		args = append(args, "-b")
	}
	return args
}

// panelToggle shows the panel beside pane (default: this pane), or hides it
// when it is open anywhere.
func panelToggle(pane string, stdout io.Writer) error {
	if err := needTmux(); err != nil {
		return err
	}
	if id := livePanel(); id != "" {
		panelWidth(id) // a width the user dragged to stays for the next show (§41.1)
		unhookPanel()
		tmuxOut("kill-pane", "-t", id)
		fmt.Fprintln(stdout, "sunstack: panel hidden")
		return nil
	}
	if pane == "" {
		pane = os.Getenv("TMUX_PANE")
	}
	dir, _ := tmuxOut("display-message", "-p", "-t", pane, "#{pane_current_path}")
	args := append([]string{"split-window", "-d", "-t", pane}, splitArgs(tmuxOpt(optSize, "35%"))...)
	if dir != "" {
		args = append(args, "-c", dir)
	}
	args = append(args, "-P", "-F", "#{pane_id}", selfCmd(tuiArgs("--compact")...))
	id, err := tmuxOut(args...)
	if err != nil || id == "" {
		return &core.Error{Code: core.ExitFail, Reason: "tmux", Msg: "tmux could not open the panel"}
	}
	tmuxOut("set-option", "-g", optPanel, id)
	// The hook names the window that changed: a background run-shell has no
	// client of its own to ask.
	follow := "run-shell -b " + tmuxDQ(selfCmd("tmux", "follow")+" --window '#{window_id}'")
	for _, h := range panelHooks {
		tmuxOut("set-hook", "-g", h, follow)
	}
	fmt.Fprintf(stdout, "sunstack: panel shown (%s); it follows you to every window and session\n", id)
	return nil
}

// panelFollow moves the panel into window (default: the one in front). The
// hooks run it.
func panelFollow(window string) error {
	id := livePanel()
	if id == "" {
		unhookPanel() // the panel was closed (q): nothing left to follow
		return nil
	}
	args := []string{"display-message", "-p"}
	if window != "" {
		args = append(args, "-t", window)
	}
	info, err := tmuxOut(append(args, "#{window_id}\t#{window_zoomed_flag}")...)
	if err != nil {
		return nil
	}
	f := strings.SplitN(info, "\t", 2)
	at, _ := tmuxOut("display-message", "-p", "-t", id, "#{window_id}\t#{window_panes}")
	g := strings.SplitN(at, "\t", 2)
	switch {
	case len(f) < 2 || len(g) < 2, f[0] == g[0]:
		return nil
	case f[1] == "1": // a zoomed window keeps its one pane
		return nil
	case g[1] == "1": // moving the panel would close the window it is in
		return nil
	}
	// It moves at its current width, so a width the user dragged to stays.
	tmuxOut(append(append([]string{"join-pane", "-d"}, splitArgs(panelWidth(id))...), "-s", id, "-t", f[0])...)
	return nil
}

// panelWidth is the panel's width in columns, kept as @sunstack-size for the
// next show. In a zoomed window tmux reports the whole window's width, so
// then the kept size stands.
func panelWidth(id string) string {
	out, _ := tmuxOut("display-message", "-p", "-t", id, "#{pane_width}\t#{window_zoomed_flag}")
	if w, zoomed, ok := strings.Cut(out, "\t"); ok && w != "" && zoomed == "0" {
		tmuxOut("set-option", "-g", optSize, w)
		return w
	}
	return tmuxOpt(optSize, "35%")
}

// popup opens the dashboard in a floating window over the client; a jump
// closes it.
func popup(pane, client string) error {
	if err := needTmux(); err != nil {
		return err
	}
	if pane == "" {
		pane = os.Getenv("TMUX_PANE")
	}
	args := []string{"display-popup", "-E", "-w", "90%", "-h", "85%", "-T", " sunstack "}
	if client != "" {
		args = append(args, "-c", client)
	}
	if dir, _ := tmuxOut("display-message", "-p", "-t", pane, "#{pane_current_path}"); dir != "" {
		args = append(args, "-d", dir)
	}
	_, err := tmuxOut(append(args, selfCmd(tuiArgs("--popup")...))...)
	if err != nil {
		return &core.Error{Code: core.ExitFail, Reason: "tmux", Msg: "tmux could not open the popup (tmux 3.2 or newer has popups)"}
	}
	return nil
}

// tmuxConf is the user's tmux config: ~/.tmux.conf, or the XDG one when only
// that exists.
func tmuxConf(home string) string {
	dot := filepath.Join(home, ".tmux.conf")
	if _, err := os.Stat(dot); err == nil {
		return dot
	}
	if x := filepath.Join(home, ".config", "tmux", "tmux.conf"); fileExists(x) {
		return x
	}
	return dot
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// tmuxInstall writes the key bindings file and sources it from the user's
// tmux config, after --yes.
func tmuxInstall(yes bool, stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
	}
	file := filepath.Join(home, ".config", "sunstack", "sunstack.tmux")
	conf := tmuxConf(home)
	// tmux expands ~ in the path, so the line is the same on every host
	// whose config comes from one dotfiles repo (§41.3); an older install's
	// absolute line is replaced.
	source := "source-file -q ~/.config/sunstack/sunstack.tmux"
	absolute := "source-file -q " + core.ShellQuote(file)
	body := "# Sunstack panel and popup, written by sunstack tmux install (§40).\n" +
		"# Options: set -g @sunstack-side left|right, @sunstack-size 35%, @sunstack-scope org|team\n" +
		"bind-key S run-shell -b " + tmuxDQ(selfCmd("tmux", "panel")+" --pane '#{pane_id}'") + "\n" +
		"bind-key F run-shell -b " + tmuxDQ(selfCmd("tmux", "popup")+" --pane '#{pane_id}' --client '#{client_name}'") + "\n"
	old, _ := os.ReadFile(conf)
	// Whole lines only: a comment or a longer path is not the source line.
	lines := strings.SplitAfter(string(old), "\n")
	sourced, staleAt := false, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case source:
			sourced = true
		case absolute:
			staleAt = i
		}
	}
	stale := !sourced && staleAt >= 0
	if !yes {
		fmt.Fprintf(stdout, "sunstack tmux install will:\n  write %s (prefix S: panel, prefix F: popup)\n", file)
		switch {
		case sourced:
			fmt.Fprintf(stdout, "  keep %s, which already sources it\n", conf)
		case stale:
			fmt.Fprintf(stdout, "  replace in %s: %s\n  with: %s\n", conf, absolute, source)
		default:
			fmt.Fprintf(stdout, "  add to %s: %s\n", conf, source)
		}
		if l, err := os.Readlink(conf); err == nil {
			fmt.Fprintf(stdout, "  (%s links to %s)\n", conf, l)
		}
		fmt.Fprintln(stdout, "Run sunstack tmux install --yes to do it.")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
	}
	fmt.Fprintf(stdout, "wrote %s\n", file)
	if stale {
		lines[staleAt] = strings.Replace(lines[staleAt], absolute, source, 1)
		if err := os.WriteFile(conf, []byte(strings.Join(lines, "")), 0o644); err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		fmt.Fprintf(stdout, "replaced in %s: %s\n", conf, source)
	} else if !sourced {
		f, err := os.OpenFile(conf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		sep := ""
		if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
			sep = "\n"
		}
		_, err = fmt.Fprintf(f, "%s\n# Sunstack panel and popup (sunstack tmux install)\n%s\n", sep, source)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		fmt.Fprintf(stdout, "added to %s: %s\n", conf, source)
	}
	if os.Getenv("TMUX") != "" {
		if _, err := tmuxOut("source-file", file); err == nil {
			fmt.Fprintln(stdout, "loaded into this tmux: prefix S panel, prefix F popup")
		}
	}
	return nil
}
