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

// home opens a pane for agent id in the team's home: a split of the agent's
// window, a new window when it has none, or the next overflow window
// (<id>-2, …) when the window is too small to split.
func (p *Project) home(id string) (*homePane, error) {
	sock, override := DefaultSocket()
	tm := func(args ...string) ([]byte, error) {
		cmd := exec.Command("tmux", append([]string{"-S", sock}, args...)...)
		cmd.Env = withoutTMUX(os.Environ())
		return cmd.Output()
	}
	tf, ok := p.Team()
	tid := tf.ID
	if !ok || tid == "" {
		tid = p.Root
	}
	tname := tf.Name
	if tname == "" {
		tname = filepath.Base(p.Root)
	}
	name := HomeName(tname, tid, p.Root)
	newPane := func(args ...string) (string, error) {
		out, err := tm(append(args, "-P", "-F", "#{pane_id}", "-c", p.Root, "-e", "PATH="+os.Getenv("PATH"))...)
		pane := strings.TrimSpace(string(out))
		if err == nil && pane == "" {
			err = fmt.Errorf("no pane")
		}
		return pane, err
	}
	var pane, win string
	if _, err := tm("has-session", "-t", "="+name); err != nil {
		if override {
			if _, err := tm("list-sessions"); err != nil {
				return nil, fail(ExitFail, "no_tmux", "no tmux server at %s (SUNSTACK_TMUX_SOCKET)", sock)
			}
		}
		if pane, err = newPane("new-session", "-d", "-s", name, "-n", id); err != nil {
			return nil, fail(ExitFail, "tmux", "could not open the team's tmux session %s: %v", name, err)
		}
		win = id
		tm("set-option", "-t", "="+name+":", "@sunstack_home", tid)
	} else {
		owner, _ := tm("show-options", "-v", "-t", "="+name+":", "@sunstack_home")
		if strings.TrimSpace(string(owner)) != tid {
			return nil, fail(ExitFail, "home_taken", "the tmux session %s was not made by sunstack for this team; rename or close it, or spawn with --place here or window", name)
		}
		out, _ := tm("list-windows", "-t", "="+name, "-F", "#{window_id}\t#{window_name}")
		windows := map[string]string{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if wid, wname, ok := strings.Cut(l, "\t"); ok {
				windows[wname] = wid
			}
		}
		for n := 1; pane == ""; n++ {
			win = id
			if n > 1 {
				win = fmt.Sprintf("%s-%d", id, n)
			}
			wid, exists := windows[win]
			if !exists {
				if pane, err = newPane("new-window", "-d", "-t", "="+name+":", "-n", win); err != nil {
					return nil, fail(ExitFail, "tmux", "could not open window %s in %s: %v", win, name, err)
				}
				break
			}
			pane, _ = newPane("split-window", "-d", "-t", wid)
			if pane != "" {
				tm("select-layout", "-t", wid, "tiled")
			}
		}
	}
	info, err := tm("display-message", "-p", "-t", pane, "#{socket_path}\t#{pid}")
	f := strings.Split(strings.TrimSpace(string(info)), "\t")
	if err != nil || len(f) != 2 {
		return nil, fail(ExitFail, "tmux", "could not read the tmux server of %s: %v", name, err)
	}
	return &homePane{pane: pane, socket: f[0], server: f[1], at: name + ":" + win}, nil
}
