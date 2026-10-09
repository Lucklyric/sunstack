package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A team's home is one tmux session on the user's default tmux server,
// with one window per agent (§20.1), so a session is found in the same
// place whoever started it.

// DefaultSocket is the default tmux server's socket. SUNSTACK_TMUX_SOCKET
// names another one (tests); spawn then never starts a server there.
func DefaultSocket() (path string, override bool) {
	if s := os.Getenv("SUNSTACK_TMUX_SOCKET"); s != "" {
		return s, true
	}
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), "default"), false
}

// HomeName is a team's home: ss-<name>-<first 4 of its ID>, or of a hash of
// its root for a team without a TEAM file.
func HomeName(name, id, root string) string {
	if id == "" || id == root {
		sum := sha256.Sum256([]byte(root))
		id = hex.EncodeToString(sum[:])
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	n := strings.Trim(b.String(), "-")
	if n == "" {
		n = "team"
	}
	return "ss-" + n + "-" + id[:4]
}

type homePane struct {
	pane, socket, server string
	at                   string // home:window, for the user
}

// homeOf is the team's home name and its owner mark (the team ID, or the
// root for a team without a TEAM file).
func (p *Project) homeOf() (name, owner string) {
	tf, ok := p.Team()
	owner = tf.ID
	if !ok || owner == "" {
		owner = p.Root
	}
	tname := tf.Name
	if tname == "" {
		tname = filepath.Base(p.Root)
	}
	return HomeName(tname, owner, p.Root), owner
}

// HomeScope is the mark the team's home and its free sessions carry.
func (p *Project) HomeScope() string {
	_, owner := p.homeOf()
	return owner
}

// home opens a pane for agent id in the team's home.
func (p *Project) home(id string) (*homePane, error) {
	name, owner := p.homeOf()
	return openHome(name, owner, p.Root, id)
}

// openHome opens a pane in folder cwd in the home called name, in window
// win: a split of that window, a new window when there is none, or the next
// overflow window (<win>-2, …) when it is too small to split. It creates the
// home, marked with owner, and refuses one that another owner made.
func openHome(name, owner, cwd, win string) (*homePane, error) {
	sock, override := DefaultSocket()
	tm := func(args ...string) ([]byte, error) {
		cmd := exec.Command("tmux", TmuxArgs(sock, args...)...)
		cmd.Env = withoutTMUX(os.Environ())
		return cmd.Output()
	}
	newPane := func(args ...string) (string, error) {
		out, err := tm(append(args, "-P", "-F", "#{pane_id}", "-c", cwd, "-e", "PATH="+os.Getenv("PATH"))...)
		pane := strings.TrimSpace(string(out))
		if err == nil && pane == "" {
			err = fmt.Errorf("no pane")
		}
		return pane, err
	}
	var pane, at string
	if _, err := tm("has-session", "-t", "="+name); err != nil {
		if override {
			if _, err := tm("list-sessions"); err != nil {
				return nil, fail(ExitFail, "no_tmux", "no tmux server at %s (SUNSTACK_TMUX_SOCKET)", sock)
			}
		}
		if pane, err = newPane("new-session", "-d", "-s", name, "-n", win); err != nil {
			return nil, fail(ExitFail, "tmux", "could not open the tmux session %s: %v", name, err)
		}
		at = win
		tm("set-option", "-t", "="+name+":", "@sunstack_home", owner)
	} else {
		mark, _ := tm("show-options", "-v", "-t", "="+name+":", "@sunstack_home")
		if strings.TrimSpace(string(mark)) != owner {
			return nil, fail(ExitFail, "home_taken", "the tmux session %s was not made by sunstack for this team; rename or close it, or use --place here or window", name)
		}
		out, _ := tm("list-windows", "-t", "="+name, "-F", "#{window_id}\t#{window_name}")
		windows := map[string]string{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if wid, wname, ok := strings.Cut(l, "\t"); ok {
				windows[wname] = wid
			}
		}
		for n := 1; pane == ""; n++ {
			at = win
			if n > 1 {
				at = fmt.Sprintf("%s-%d", win, n)
			}
			wid, exists := windows[at]
			if !exists {
				if pane, err = newPane("new-window", "-d", "-t", "="+name+":", "-n", at); err != nil {
					return nil, fail(ExitFail, "tmux", "could not open window %s in %s: %v", at, name, err)
				}
				break
			}
			if pane, _ = newPane("split-window", "-d", "-t", wid); pane != "" {
				tm("select-layout", "-t", wid, "tiled")
			}
		}
	}
	info, err := tm("display-message", "-p", "-t", pane, "#{socket_path}\t#{pid}")
	f := strings.Split(strings.TrimSpace(string(info)), "\t")
	if err != nil || len(f) != 2 {
		return nil, fail(ExitFail, "tmux", "could not read the tmux server of %s: %v", name, err)
	}
	return &homePane{pane: pane, socket: f[0], server: f[1], at: name + ":" + at}, nil
}

// AttachPlace is where a session's pane is, for sunstack attach.
type AttachPlace struct {
	Socket, Pane, Session string // tmux server, pane, and the tmux session holding it
	Label                 string
}

// FindAttach resolves a session for attach: an agent session's name in
// project p (<id> or <id>_<task>, refused when several match), or any
// session's ID or label from the scan.
func FindAttach(p *Project, t string) (*AttachPlace, error) {
	at := &AttachPlace{Label: t}
	scope := ""
	if p != nil {
		scope = p.HomeScope()
	}
	// A free session by its label (§20.2): it holds no claim, and before its
	// first prompt it has no session ID either.
	pane, sock, label, ferr := FindFree(t, scope)
	switch {
	case ferr == nil:
		at.Socket, at.Pane, at.Label = sock, pane, label
	case strings.Contains(ferr.Error(), "free sessions are called"):
		return nil, ferr
	case p != nil && !IsSessionID(t) && !strings.HasPrefix(t, "%"):
		id, c, err := p.target(t)
		if err != nil {
			return nil, err
		}
		if c.TmuxPane == "" || c.TmuxSocket == "" || paneGone(c) {
			return nil, fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows", c.Label(id))
		}
		at.Socket, at.Pane, at.Label = c.TmuxSocket, c.TmuxPane, c.Label(id)
	default:
		sessions, _ := ScanSessions()
		var found []*HostSession
		for _, s := range sessions {
			if s.SessionID == t || (s.Label != "" && strings.SplitN(s.Label, "@", 2)[0] == t) || (s.Name != "" && s.Name == t) {
				found = append(found, s)
			}
		}
		switch {
		case len(found) == 0:
			return nil, fail(ExitFail, "not_found", "no session %s on this host", t)
		case len(found) > 1:
			return nil, fail(ExitFail, "ambiguous", "%d sessions are called %s; use a session ID (sunstack sessions)", len(found), t)
		case found[0].Pane == "":
			return nil, fail(ExitFail, "no_pane", "session %s runs outside tmux (%s); sunstack reopen moves it into tmux", t, placeOf(found[0]))
		}
		at.Socket, at.Pane = found[0].Socket(), found[0].Pane
	}
	out, err := exec.Command("tmux", "-u", "-S", at.Socket, "display-message", "-p", "-t", at.Pane, "#{session_name}").Output()
	if err != nil {
		return nil, fail(ExitFail, "no_pane", "could not reach pane %s: %v", at.Pane, err)
	}
	at.Session = strings.TrimSpace(string(out))
	return at, nil
}
