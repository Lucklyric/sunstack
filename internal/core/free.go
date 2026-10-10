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
	// Remote: another host asked (§20.4); Dir must then stay inside Root.
	Remote  bool
	Request string // that request's ID, marked on the pane
}

// FreeResult is a started free session.
type FreeResult struct {
	Label, Tool, Pane, Home, Cwd string
	Running                      bool
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
	if !noteOK(o.Note) || strings.HasPrefix(o.Note, "-") {
		return nil, fail(ExitUsage, "usage", "keep single quotes, line breaks and other control characters out of the note, and do not start it with -")
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
	defer unlock()
	taken := map[string]bool{}
	ps, _ := freePanes()
	for _, fp := range ps {
		if fp.scope == scope {
			taken[fp.label] = true
		}
	}
	label := o.Name
	switch {
	case label != "" && taken[label]:
		return nil, fail(ExitClaim, "label_taken", "a free session called %s is already open here; pick another --name", label)
	case label == "":
		for n := 1; label == "" || taken[label]; n++ {
			label = fmt.Sprintf("%s-%d", o.Tool, n)
		}
	}
	h, err := openHome(home, scope, cwd, "free")
	if err != nil {
		return nil, err
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
	cmd := o.Tool
	if o.Note != "" {
		cmd += " '" + o.Note + "'"
	}
	if err := tm("send-keys", "-t", h.pane, "-l", cmd); err == nil {
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
	return &FreeResult{Label: label, Tool: o.Tool, Pane: h.pane, Home: h.at, Cwd: cwd, Running: running}, nil
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
			return "", fail(ExitUsage, "ambiguous", "%d sessions are called %s; use its session ID (sunstack sessions)", len(found), o.Beside)
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
					if fp.pane == s.Pane {
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
