package core

import (
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
		if err != nil {
			return "", "", err
		}
		if c.TmuxPane == "" || c.TmuxSocket == "" || !sameServer(c) {
			return "", "", fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows", c.Label(id))
		}
		socket, pane, tool, label = c.TmuxSocket, c.TmuxPane, c.Tool, c.Label(id)
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
	screen := paneScreen(socket, pane)
	if screen == "" {
		return "", "", fail(ExitFail, "no_pane", "could not read the pane of %s", label)
	}
	text = strings.Join(screenTail(screen, n), "\n") + "\n"
	if len(text) > PeekBytes {
		text = text[len(text)-PeekBytes:]
	}
	return label, text, nil
}
