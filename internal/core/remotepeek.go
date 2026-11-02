package core

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// PeekLimit and PeekBytes bound a peek another host asks for (§20.5).
const (
	PeekLimit = 50
	PeekBytes = 16 << 10
)

var (
	peekMu   sync.Mutex
	peekLast = map[string]time.Time{}
)

// PeekFor answers another host's peek: addr is <team>/<agent>[_task] or a
// session ID. It resolves exactly one live CLI session from this host's own
// files and scan, checks just before the capture that its pane still runs
// that CLI, and returns at most n lines and PeekBytes. A session is peeked
// at most once every 2 seconds, whatever name it was asked by.
func PeekFor(addr string, n int) (label, text string, err error) {
	if n < 1 || n > PeekLimit {
		n = PeekLimit
	}
	var socket, pane, tool string
	var since time.Time // the claim's last contact: a CLI started after it is not that session
	cliPID := 0         // the claim's CLI process, when it recorded one
	if IsSessionID(addr) {
		sessions, _ := ScanSessions()
		var found []*HostSession
		for _, s := range sessions {
			if s.SessionID == addr {
				found = append(found, s)
			}
		}
		switch {
		case len(found) != 1:
			return "", "", fail(ExitFail, "not_found", "no single session %s on this host", addr)
		case found[0].Pane == "":
			return "", "", fail(ExitFail, "no_pane", "session %s is not in a tmux pane", addr)
		}
		s := found[0]
		socket, pane, tool, label = s.Socket(), s.Pane, s.Tool, sessionName(s)
	} else {
		team, agent, ok := strings.Cut(addr, "/")
		if !ok {
			return "", "", fail(ExitUsage, "usage", "peek <team>/<agent>[_task] or a session ID")
		}
		p, err := ResolveTeam(team)
		if err != nil {
			return "", "", err
		}
		id, c, err := p.target(agent)
		var ce *Error
		switch {
		case err == nil:
			if c.TmuxPane == "" || c.TmuxSocket == "" || !sameServer(c) {
				return "", "", fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows", c.Label(id))
			}
			socket, pane, tool, label = c.TmuxSocket, c.TmuxPane, c.Tool, c.Label(id)
			cliPID = c.CLIPID
			if why := claimPaneHeld(c, &scanCache{}); why != "" {
				return "", "", fail(ExitFail, "not_running", "%s: %s", c.Label(id), why)
			}
			for _, at := range []string{c.Claimed, c.LastContact} {
				if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(since) {
					since = t
				}
			}
		case errors.As(err, &ce) && ce.Reason == "not_found":
			// A free session in the team, by its label: a new one has no
			// session ID until its first prompt.
			_, scope := p.homeOf()
			ps, sock := freePanes()
			for _, fp := range ps {
				if fp.label == agent && fp.scope == scope {
					socket, pane, tool, label = sock, fp.pane, fp.tool, fp.label
				}
			}
			if pane == "" {
				return "", "", fail(ExitFail, "not_found", "no agent or free session %s in %s", agent, team)
			}
		default:
			return "", "", err
		}
	}
	key := socket + "|" + pane
	peekMu.Lock()
	if t, ok := peekLast[key]; ok && time.Since(t) < 2*time.Second {
		peekMu.Unlock()
		return "", "", fail(ExitFail, "too_soon", "%s was peeked less than 2 seconds ago", label)
	}
	peekLast[key] = time.Now()
	peekMu.Unlock()
	// The pane must still run that CLI: a pane ID is reused once its
	// session ends.
	if !paneRunsTool(socket, pane, tool) {
		return "", "", fail(ExitFail, "not_running", "the pane of %s no longer runs %s", label, tool)
	}
	// The same CLI in the same pane may be a new session that never took
	// this claim (§34.10).
	if !since.IsZero() { // an agent's claim: its own CLI, or nothing
		// Only the claim's own CLI process. A claim from before v0.17.2 has
		// none recorded; start times cannot stand in for it, since Linux
		// reports them a second off (§34.10).
		pid, _, ok := paneToolProc(socket, pane, tool)
		switch {
		case cliPID == 0:
			return "", "", fail(ExitFail, "not_running", "%s has no recorded CLI process (claimed before v0.17.2); it can be peeked after it runs as --token again", label)
		case !ok:
			return "", "", fail(ExitFail, "not_running", "could not confirm the pane of %s still runs the session that claimed it", label)
		case pid != cliPID:
			return "", "", fail(ExitFail, "not_running", "the pane of %s now runs another %s session, not the one that claimed it", label, tool)
		}
	}
	screen := paneScreen(socket, pane)
	if screen == "" {
		return "", "", fail(ExitFail, "no_pane", "could not read the pane of %s", label)
	}
	text = maskTokens(strings.Join(screenTail(screen, n), "\n")) + "\n"
	if len(text) > PeekBytes {
		text = text[len(text)-PeekBytes:]
	}
	return label, text, nil
}
