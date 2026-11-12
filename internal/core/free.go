package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Free sessions (§20.2) are plain Claude or Codex sessions that hold no
// agent. They open in a home like agent sessions (window free), and their
// pane carries the marks that name them: @sunstack_launch, @sunstack_scope
// (the team or folder group), @sunstack_label, @sunstack_tool and
// @sunstack_root.

// FreeOptions are the inputs of spawn --free.
type FreeOptions struct {
	Tool        string // claude or codex; empty: DefaultTool, else claude
	DefaultTool string
	Name        string // the label; empty: <tool>-<n>
	Note        string // the first prompt
	Beside      string // a session (ID, label or name) whose folder to use
	Dir         string // a folder; relative to the team root in a team, else to the working directory
	Root        string // the caller's team root, if any
	OverCap     bool
	// Access: read-only or full-auto; empty is the user's own settings (§47).
	Access string
	// Model and Effort for this session; empty is the CLI's own setting.
	Model, Effort string
	// Remote: another host asked (§20.4); Dir must then stay inside Root.
	Remote  bool
	Request string // that request's ID, marked on the pane
	// Where, as for spawn: team (the scope's home), here (beside Caller,
	// else a new window) or window; empty is here inside tmux. Outside
	// tmux (no Socket) and for another host, always the home.
	Place, Socket, Caller string
}

// FreeResult is a started free session.
type FreeResult struct {
	Label, Tool, Pane, Home, Cwd string // Home: the home it opened in, if any
	Running, Window              bool   // Window: a new window, not beside the caller
	Prompt                       string // a trust screen answered, or waiting (§37)
}

var labelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`)

// freePane is a live free session's pane on the default tmux server.
type freePane struct{ pane, scope, label, tool string }

// freePanes lists the free sessions on the default tmux server.
func freePanes() ([]freePane, string) {
	sock, _ := DefaultSocket()
	cmd := exec.Command("tmux", "-u", "-S", sock, "list-panes", "-a", "-F", "#{pane_id}\t#{@sunstack_launch}\t#{@sunstack_scope}\t#{@sunstack_label}\t#{@sunstack_tool}")
	cmd.Env = withoutTMUX(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return nil, sock
	}
	var ps []freePane
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(l, "\t")
		if len(f) == 5 && f[1] != "" {
			ps = append(ps, freePane{f[0], f[2], f[3], f[4]})
		}
	}
	return ps, sock
}

// onDefaultServer says whether socket is the default tmux server's.
func onDefaultServer(socket string) bool {
	def, _ := DefaultSocket()
	a, err1 := os.Stat(socket)
	b, err2 := os.Stat(def)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}

// SpawnFree starts a free session.
func SpawnFree(o FreeOptions) (*FreeResult, error) {
	if o.Tool == "" {
		o.Tool = o.DefaultTool
	}
	if o.Tool == "" {
		o.Tool = "claude"
	}
	if o.Tool != "claude" && o.Tool != "codex" {
		return nil, fail(ExitUsage, "usage", "--tool must be claude or codex")
	}
	if o.Name != "" && !labelRe.MatchString(o.Name) {
		return nil, fail(ExitUsage, "usage", "invalid --name %q: lowercase letters, digits and -, at most 20 characters", o.Name)
	}
	if o.Model != "" && !modelRe.MatchString(o.Model) {
		return nil, fail(ExitUsage, "usage", "invalid --model %q: a model name or alias, like sonnet or gpt-5", o.Model)
	}
	switch o.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, fail(ExitUsage, "usage", "--effort is low, medium, high, xhigh or max")
	}
	if !noteOK(o.Note) || strings.HasPrefix(o.Note, "-") {
		return nil, fail(ExitUsage, "usage", "keep single quotes, line breaks and other control characters out of the note, and do not start it with -")
	}
	place := o.Place
	switch {
	case place == "":
		place = "here"
	case place != "team" && place != "here" && place != "window":
		return nil, fail(ExitUsage, "usage", "--place must be team, here or window")
	}
	// Free sessions are found and counted on the default server only.
	if o.Socket == "" || o.Remote || !onDefaultServer(o.Socket) {
		place = "team"
	}
	if o.Beside != "" && o.Dir != "" {
		return nil, fail(ExitUsage, "usage", "use --beside or --dir, not both")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fail(ExitFail, "no_tmux", "spawn opens a tmux pane, and tmux is not on PATH")
	}
	if _, err := exec.LookPath(o.Tool); err != nil {
		return nil, fail(ExitFail, "no_cli", "%s is not on PATH", o.Tool)
	}
	cwd, err := freeFolder(o)
	if err != nil {
		return nil, err
	}
	// The scope is the team holding the folder, else its group (§20.1).
	var home, scope string
	team, terr := FindProject(cwd)
	var unlock func()
	if terr == nil {
		if date, reason, ok := team.Halted(); ok {
			return nil, fail(ExitFail, "halted", "the team is halted since %s: %s; nothing new starts until the user runs sunstack halt --off", date, reason)
		}
		if unlock, err = team.lock("_spawn_"); err != nil {
			return nil, err
		}
		home, scope = team.homeOf()
		if !o.OverCap {
			if err := team.checkCap(); err != nil {
				unlock()
				return nil, err
			}
		}
	} else {
		g := gitRemote(cwd)
		if g == "" {
			g = cwd
		}
		scope = "group:" + g
		home = HomeName(strings.TrimSuffix(filepath.Base(g), ".git"), "", g)
		if unlock, err = LockDir(filepath.Join(Home(), "locks", "free"), "free sessions"); err != nil {
			return nil, err
		}
	}
	// Names are given under one lock, shared with sunstack name (§46.1).
	names, err := lockNames()
	if err != nil {
		unlock()
		return nil, err
	}
	inner := unlock
	unlock = func() { names(); inner() }
	defer func() { unlock() }() // the current value: the success path releases early
	taken := map[string]bool{}
	ps, sock := freePanes()
	for _, fp := range ps {
		if fp.scope == scope {
			taken[fp.label] = true
		}
	}
	// A session sunstack did not start that was given a name holds it too.
	if out, err := quickTmux(sock, "list-panes", "-a", "-F", "#{@sunstack_launch}\t#{@sunstack_label}"); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if f := strings.Split(l, "\t"); len(f) == 2 && f[0] == "" && f[1] != "" {
				taken[f[1]] = true
			}
		}
	}
	label := o.Name
	switch {
	case label != "" && taken[label]:
		return nil, fail(ExitClaim, "label_taken", "a session called %s is already open here; pick another --name", label)
	case label == "":
		for n := 1; label == "" || taken[label]; n++ {
			label = fmt.Sprintf("%s-%d", o.Tool, n)
		}
	}
	var h *homePane
	window := false
	if place == "team" {
		if h, err = openHome(home, scope, cwd, "free"); err != nil {
			return nil, err
		}
	} else {
		pane, win, err := openNear(o.Socket, o.Caller, place, cwd, label)
		if err != nil {
			return nil, err
		}
		h = &homePane{pane: pane, socket: o.Socket}
		window = win
	}
	tm := func(args ...string) error {
		return exec.Command("tmux", append([]string{"-S", h.socket}, args...)...).Run()
	}
	launch := make([]byte, 8)
	rand.Read(launch)
	marks := map[string]string{"@sunstack_launch": hex.EncodeToString(launch), "@sunstack_scope": scope, "@sunstack_label": label, "@sunstack_tool": o.Tool, "@sunstack_root": cwd}
	if o.Request != "" {
		marks["@sunstack_request"] = o.Request
	}
	for k, v := range marks {
		tm("set-option", "-p", "-t", h.pane, k, v)
	}
	tm("select-pane", "-t", h.pane, "-T", label)
	cmd := o.Tool + titleFlag(o.Tool, label) + accessFlags(o.Tool, o.Access) + modelFlags(o.Tool, o.Model, o.Effort)
	if o.Note != "" {
		cmd += " '" + o.Note + "'"
	}
	if err = tm("send-keys", "-t", h.pane, "-l", cmd); err == nil {
		err = tm("send-keys", "-t", h.pane, "Enter")
	}
	if err != nil {
		tm("kill-pane", "-t", h.pane)
		return nil, fail(ExitFail, "tmux", "could not start %s: %v", o.Tool, err)
	}
	unlock()
	unlock = func() {}
	if terr == nil {
		team.LogEvent("spawn", label, "tool="+o.Tool, "pane="+h.pane, "free")
	}
	running := false
	for i := 0; i < 20 && !running; i++ {
		time.Sleep(250 * time.Millisecond)
		running = paneRunsTool(h.socket, h.pane, o.Tool)
	}
	_, notTeam := FindProject(cwd)
	prompt := AnswerTrust(h.socket, h.pane, o.Tool, notTeam == nil, running)
	if running && o.Tool == "codex" && o.Note == "" {
		renameCodex(h.socket, h.pane, label)
	}
	return &FreeResult{Label: label, Tool: o.Tool, Pane: h.pane, Home: h.at, Cwd: cwd, Running: running, Window: window, Prompt: prompt}, nil
}

// freeFolder resolves where a free session opens: beside a session, a
// folder, or the caller's team root.
func freeFolder(o FreeOptions) (string, error) {
	var dir string
	switch {
	case o.Beside != "":
		sessions, _ := ScanSessions()
		var found []*HostSession
		for _, s := range sessions {
			if s.SessionID == o.Beside || (s.Label != "" && strings.SplitN(s.Label, "@", 2)[0] == o.Beside) || (s.Name != "" && s.Name == o.Beside) {
				found = append(found, s)
			}
		}
		switch {
		case len(found) == 0:
			return "", fail(ExitFail, "not_found", "no session %s on this host", o.Beside)
		case len(found) > 1:
			return "", fail(ExitUsage, "ambiguous", "%d sessions are called %s; use its session ID (sunstack team --sessions)", len(found), o.Beside)
		case found[0].Cwd == "":
			return "", fail(ExitFail, "not_found", "could not tell which folder %s runs in", o.Beside)
		}
		dir = found[0].Cwd
	case o.Dir != "" && o.Remote:
		// Another host names only a subfolder of the team (§20.4).
		if o.Root == "" || filepath.IsAbs(o.Dir) || strings.HasPrefix(filepath.Clean(o.Dir), "..") {
			return "", fail(ExitUsage, "usage", "from another host, --dir is a subfolder of the team")
		}
		dir = realPath(filepath.Join(o.Root, o.Dir))
		root := realPath(o.Root)
		if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return "", fail(ExitUsage, "usage", "%s leaves the team's folder", o.Dir)
		}
	case o.Dir != "":
		dir = o.Dir
		if !filepath.IsAbs(dir) {
			base := o.Root
			if base == "" {
				base, _ = os.Getwd()
			}
			dir = filepath.Join(base, dir)
		}
	case o.Root != "":
		dir = o.Root
	default:
		return "", fail(ExitUsage, "usage", "run it in a team, or name a folder: --dir <path> or --beside <session>")
	}
	dir = realPath(dir)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fail(ExitFail, "not_found", "no folder %s", dir)
	}
	return dir, nil
}

// freeCount is the number of live free sessions in a team's scope, for the
// cap, and their labels.
func freeCount(scope string) []string {
	ps, _ := freePanes()
	var out []string
	for _, fp := range ps {
		if fp.scope == scope {
			out = append(out, fp.label+" (free)")
		}
	}
	return out
}

// FindFree finds a free session's pane by label (in scope when given) or by
// session ID.
func FindFree(t, scope string) (pane, socket, label string, err error) {
	ps, sock := freePanes()
	var match []freePane
	for _, fp := range ps {
		if fp.label == t && (scope == "" || fp.scope == scope) {
			match = append(match, fp)
		}
	}
	if len(match) == 0 && IsSessionID(t) {
		sessions, _ := ScanSessions()
		for _, s := range sessions {
			if s.SessionID == t && s.Pane != "" {
				for _, fp := range ps {
					if fp.pane == s.Pane && onDefaultServer(s.socket) {
						match = append(match, fp)
					}
				}
			}
		}
	}
	switch len(match) {
	case 0:
		return "", "", "", fail(ExitFail, "not_found", "no free session %s", t)
	case 1:
		return match[0].pane, sock, match[0].label, nil
	}
	return "", "", "", fail(ExitUsage, "ambiguous", "%d free sessions are called %s; run it in the team, or use the session ID", len(match), t)
}

// KillFree closes a free session's pane.
func KillFree(pane, socket, label string) (string, error) {
	if os.Getenv("TMUX_PANE") == pane {
		return "", fail(ExitUsage, "self", "%s is this pane; close it yourself", label)
	}
	if err := exec.Command("tmux", "-S", socket, "kill-pane", "-t", pane).Run(); err != nil {
		return "", fail(ExitFail, "tmux", "could not close pane %s: %v", pane, err)
	}
	return "closed " + label + " (pane " + pane + ")", nil
}

// FreeByPane is the free session in a pane on the default tmux server.
func FreeByPane(pane string) (label, socket string, ok bool) {
	if pane == "" {
		return "", "", false
	}
	ps, sock := freePanes()
	for _, fp := range ps {
		if fp.pane == pane {
			return fp.label, sock, true
		}
	}
	return "", "", false
}

// accessFlags are the CLI options for what a free session may do without
// asking (§47). read-only: Codex cannot write files (Claude Code already
// asks before each edit). full-auto: Codex writes inside its folder and
// reaches the network, Claude Code accepts edits. Empty: the user's settings.
func accessFlags(tool, access string) string {
	switch {
	case access == "read-only" && tool == "codex":
		return " -s read-only"
	case access == "full-auto" && tool == "codex":
		return " -s workspace-write -c sandbox_workspace_write.network_access=true"
	case access == "full-auto" && tool == "claude":
		return " --permission-mode acceptEdits"
	}
	return ""
}

// modelRe is a model name or alias: it goes on a command line unquoted.
var modelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]{0,63}$`)

// modelFlags are the CLI options for a session's model and reasoning effort.
func modelFlags(tool, model, effort string) string {
	out := ""
	if tool == "codex" {
		if model != "" {
			out += " -m '" + model + "'"
		}
		if effort != "" {
			out += " -c model_reasoning_effort=" + effort
		}
		return out
	}
	if model != "" {
		out += " --model '" + model + "'"
	}
	if effort != "" {
		out += " --effort " + effort
	}
	return out
}

// renameCodex titles a new Codex session with its label (§47): Codex has no
// option for it at launch, and takes /rename before the first prompt. It
// waits for Codex to finish starting, and gives up quietly: the title is a
// convenience, and sunstack next says when one is missing.
func renameCodex(socket, pane, label string) {
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		screen := paneScreen(socket, pane)
		if CLIReady("codex", screen) {
			// typeInto types only into an empty box and presses Enter only
			// on exactly this line.
			typeInto(socket, pane, "codex", "/rename "+label)
			return
		}
		if !ReadyForInput("codex", screen) && (hasDialog(screen) || hasMenu(screen)) {
			return // a screen for the user
		}
	}
}
