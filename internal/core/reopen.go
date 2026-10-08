package core

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Reopen (v0.8.2) moves a Claude Code or Codex session that ran outside tmux
// into a tmux pane, so messages can wake it at once. The session must be
// closed first: the same conversation is never open in two places.

// ReopenOptions are the inputs of `sunstack reopen`.
type ReopenOptions struct {
	SessionID string
	Socket    string // the caller's tmux server
	Server    string // its PID
	Caller    string // the caller's pane
	Window    bool   // a new window instead of a pane beside the caller
	Place     string // team, here or window; default team for a session in a team (§20.1)
}

// ReopenResult says where the session was reopened.
type ReopenResult struct{ Tool, Cwd, Pane, Command string }

// sessionOrigin finds a session's tool and folder from its transcript.
func sessionOrigin(sid string) (tool, cwd string, ok bool) {
	home, _ := os.UserHomeDir()
	if m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sid+".jsonl")); len(m) > 0 {
		f, err := os.Open(m[0])
		if err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 8<<20)
			for i := 0; i < 200 && sc.Scan(); i++ {
				var e struct {
					Cwd string `json:"cwd"`
				}
				if json.Unmarshal(sc.Bytes(), &e) == nil && e.Cwd != "" {
					return "claude", e.Cwd, true
				}
			}
		}
		return "claude", "", true
	}
	if m, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+sid+".jsonl")); len(m) > 0 {
		return "codex", rolloutCwd(m[0]), true
	}
	return "", "", false
}

// Reopen opens a tmux pane in the session's folder and resumes it there. A
// claim that recorded this session moves to the new pane.
func Reopen(o ReopenOptions) (*ReopenResult, error) {
	if !IsSessionID(o.SessionID) {
		return nil, fail(ExitUsage, "usage", "reopen takes a session ID (sunstack org --by host shows them)")
	}
	tool, cwd, ok := sessionOrigin(o.SessionID)
	if !ok {
		return nil, fail(ExitFail, "not_found", "no Claude Code or Codex transcript for session %s on this host", o.SessionID)
	}
	if cwd == "" {
		return nil, fail(ExitFail, "not_found", "could not tell which folder session %s ran in", o.SessionID)
	}
	cmd := "claude --resume " + o.SessionID
	if tool == "codex" {
		cmd = "codex resume " + o.SessionID
	}
	sessions, notes := ScanSessions()
	if len(notes) > 0 {
		return nil, fail(ExitFail, "scan_failed", "could not check whether session %s is still open (%s); try again", o.SessionID, strings.Join(notes, "; "))
	}
	for _, s := range sessions {
		if s.SessionID == o.SessionID {
			where := placeOf(s)
			if s.Pane != "" {
				return nil, fail(ExitFail, "in_tmux", "session %s is already in tmux (%s)", o.SessionID, where)
			}
			return nil, fail(ExitFail, "running", "session %s is still open (%s); exit it there first (/exit), then rerun", o.SessionID, where)
		}
	}
	res := &ReopenResult{Tool: tool, Cwd: cwd, Command: cmd}
	team, terr := FindProject(realPath(cwd))
	place := o.Place
	switch {
	case o.Window:
		place = "window"
	case place == "" && terr == nil:
		place = "team"
	case place == "":
		place = "here"
	case place != "team" && place != "here" && place != "window":
		return nil, fail(ExitUsage, "usage", "--place must be team, here or window")
	}
	if place == "team" && terr != nil {
		return nil, fail(ExitUsage, "usage", "%s is in no team, so it has no home; use --place here or window", cwd)
	}
	if place != "team" && o.Socket == "" {
		return res, fail(ExitFail, "no_tmux", "reopen opens a tmux pane, so run it inside tmux; or start tmux and run: cd %q && %s", cwd, cmd)
	}
	if _, err := exec.LookPath(tool); err != nil {
		return nil, fail(ExitFail, "no_cli", "%s is not on PATH", tool)
	}
	if place == "team" {
		// The agent's window when the session holds a claim, else free.
		win := "free"
		for _, id := range team.Agents() {
			claims, _ := team.Claims(id)
			for _, c := range claims {
				if c.Session == o.SessionID {
					win = id
				}
			}
		}
		name, owner := team.homeOf()
		h, err := openHome(name, owner, cwd, win)
		if err != nil {
			return nil, err
		}
		o.Socket, o.Server = h.socket, h.server
		return finishReopen(o, res, tool, cmd, h.pane)
	}
	var args []string
	if place == "window" || o.Caller == "" {
		args = []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-c", cwd, "-e", "PATH=" + os.Getenv("PATH")}
	} else {
		split := "-v"
		if w, _ := exec.Command("tmux", TmuxArgs(o.Socket, "display-message", "-p", "-t", o.Caller, "#{pane_width}")...).Output(); atoiOr(strings.TrimSpace(string(w)), 0) >= 160 {
			split = "-h"
		}
		args = []string{"split-window", "-d", split, "-t", o.Caller, "-P", "-F", "#{pane_id}", "-c", cwd, "-e", "PATH=" + os.Getenv("PATH")}
	}
	out, err := exec.Command("tmux", TmuxArgs(o.Socket, args...)...).Output()
	pane := strings.TrimSpace(string(out))
	if err != nil || pane == "" {
		return nil, fail(ExitFail, "tmux", "could not open a tmux pane (%v); rerun with --window", err)
	}
	return finishReopen(o, res, tool, cmd, pane)
}

// finishReopen resumes the session in its new pane and moves a claim that
// recorded it there.
func finishReopen(o ReopenOptions, res *ReopenResult, tool, cmd, pane string) (*ReopenResult, error) {
	err := exec.Command("tmux", TmuxArgs(o.Socket, "send-keys", "-t", pane, "-l", cmd)...).Run()
	if err == nil {
		err = exec.Command("tmux", TmuxArgs(o.Socket, "send-keys", "-t", pane, "Enter")...).Run()
	}
	if err != nil {
		exec.Command("tmux", TmuxArgs(o.Socket, "kill-pane", "-t", pane)...).Run()
		return nil, fail(ExitFail, "tmux", "could not start %s in the new pane: %v", tool, err)
	}
	res.Pane = pane
	// An agent session keeps its claim: point it at the new pane.
	for _, p := range IndexedProjects() {
		for _, id := range p.Agents() {
			unlock, err := p.lock(id)
			if err != nil {
				continue
			}
			// Read the claims under the lock, so a concurrent change is kept.
			claims, _ := p.Claims(id)
			for _, c := range claims {
				if c.Session != o.SessionID {
					continue
				}
				c.TmuxPane, c.TmuxSocket, c.TmuxServer = pane, o.Socket, o.Server
				_ = p.writeLive(id, c)
				exec.Command("tmux", TmuxArgs(o.Socket, "select-pane", "-t", pane, "-T", c.Label(id))...).Run()
				p.LogEvent("reopen", c.Label(id), "pane="+pane)
			}
			unlock()
		}
	}
	return res, nil
}
