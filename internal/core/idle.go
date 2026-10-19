package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IdleAfter is how long a session may sit at its prompt, with nothing
// unfinished, before sunstack next offers to close it.
const IdleAfter = 24 * time.Hour

// Idle session kinds: sunstack clean closes each kind its own way.
const (
	IdleAgent  = "agent"  // an agent session at its prompt over IdleAfter
	IdleFree   = "free"   // a free session (§20.2) at its prompt over IdleAfter
	IdleExited = "exited" // a pane sunstack opened whose Claude or Codex has exited
)

// IdleSession is a session or pane that can be closed: sunstack clean's list.
type IdleSession struct {
	Kind  string
	Label string        // <id>_<task> or the free label, what kill takes; empty for an exited pane with no claim
	Name  string        // what the user reads
	Pane  string        // tmux pane
	For   time.Duration // since its last activity; 0 for an exited pane
	free  bool          // a free session's pane
	agent string        // an exited pane with no claim: the agent its mark names
}

// IdleSessions lists what sunstack clean closes for the team: its agent
// sessions and free sessions that run in a tmux pane, are at their prompt,
// have done nothing for IdleAfter and hold no taken message or staged save,
// then the panes sunstack opened for it whose CLI has exited. The calling
// session's own pane is never listed. sessions is ScanSessions'.
func (p *Project) IdleSessions(sessions []*HostSession, now time.Time) []IdleSession {
	out := p.idleAgents(sessions, now)
	out = append(out, p.idleFree(sessions, now)...)
	return append(out, p.exitedPanes()...)
}

func (p *Project) idleAgents(sessions []*HostSession, now time.Time) []IdleSession {
	host := ThisHost().Name
	byLabel := map[string]*HostSession{}
	for _, list := range classify(sessions, []*Project{p})[p.Root] {
		for _, s := range list {
			byLabel[s.Label] = s
		}
	}
	var out []IdleSession
	for _, id := range p.Agents() {
		claims, _ := p.Claims(id)
		for _, c := range claims {
			name := c.Label(id)
			s := byLabel[name+"@"+host]
			if s == nil || s.Status != "idle" || c.TmuxPane == "" || !sameServer(c) || os.Getenv("TMUX_PANE") == c.TmuxPane {
				continue
			}
			if len(listNames(p.local("inbox", id, ".taken", c.Token), ".md")) > 0 {
				continue
			}
			if staged, _ := os.ReadDir(p.sessionTmp(id, c.Token)); len(staged) > 0 {
				continue
			}
			if d, ok := idleFor(now, c.Claimed, c.LastContact, c.DoingAt, s.Activity.LastActive); ok {
				out = append(out, IdleSession{Kind: IdleAgent, Label: name, Name: name, Pane: c.TmuxPane, For: d})
			}
		}
	}
	return out
}

// idleFree lists the team's free sessions at their prompt over IdleAfter,
// with no taken message in their host inbox.
func (p *Project) idleFree(sessions []*HostSession, now time.Time) []IdleSession {
	ps, _ := freePanes()
	scope := p.HomeScope()
	var out []IdleSession
	for _, fp := range ps {
		if fp.scope != scope || os.Getenv("TMUX_PANE") == fp.pane {
			continue
		}
		for _, s := range sessions {
			if s.Pane != fp.pane || s.Status != "idle" || s.SessionID == "" {
				continue
			}
			if len(listNames(hostTakenDir(s.SessionID), ".md")) > 0 {
				continue
			}
			if d, ok := idleFor(now, s.Activity.LastActive); ok {
				out = append(out, IdleSession{Kind: IdleFree, Label: fp.label, Name: fp.label + " (free)", Pane: fp.pane, For: d, free: true})
			}
		}
	}
	return out
}

// exitedPanes lists the panes sunstack opened for the team (an agent's spawn
// or a free session) whose Claude or Codex has exited: the pane is dead, or
// back at its shell with nothing else running. Panes the user opened, and
// panes running anything but a shell, are never listed.
func (p *Project) exitedPanes() []IdleSession {
	var out []IdleSession
	seen := map[string]bool{}
	for _, id := range p.Agents() {
		claims, _ := p.Claims(id)
		for _, c := range claims {
			if !c.Spawned || c.TmuxPane == "" || c.TmuxSocket == "" || !sameServer(c) || os.Getenv("TMUX_PANE") == c.TmuxPane {
				continue
			}
			mark, _ := exec.Command("tmux", TmuxArgs(c.TmuxSocket, "show-options", "-p", "-v", "-t", c.TmuxPane, "@sunstack")...).Output()
			if strings.TrimSpace(string(mark)) != p.Root+"|"+id || !paneExited(c.TmuxSocket, c.TmuxPane) {
				continue
			}
			if onDefaultServer(c.TmuxSocket) {
				seen[c.TmuxPane] = true
			}
			name := c.Label(id)
			out = append(out, IdleSession{Kind: IdleExited, Label: name, Name: name + " (" + c.Tool + " exited)", Pane: c.TmuxPane})
		}
	}
	// Agent panes left with no claim (released, or dropped as stale), and
	// free sessions, on the default tmux server.
	sock, _ := DefaultSocket()
	cmd := exec.Command("tmux", TmuxArgs(sock, "list-panes", "-a", "-F", "#{pane_id}\t#{@sunstack}\t#{@sunstack_launch}\t#{@sunstack_scope}\t#{@sunstack_label}\t#{@sunstack_tool}")...)
	cmd.Env = withoutTMUX(os.Environ())
	list, _ := cmd.Output()
	scope := p.HomeScope()
	for _, l := range strings.Split(strings.TrimSpace(string(list)), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 6 || seen[f[0]] || os.Getenv("TMUX_PANE") == f[0] {
			continue
		}
		pane, mark, launch, fscope, label, tool := f[0], f[1], f[2], f[3], f[4], f[5]
		switch {
		case strings.HasPrefix(mark, p.Root+"|"):
			if id := strings.TrimPrefix(mark, p.Root+"|"); paneExited(sock, pane) {
				out = append(out, IdleSession{Kind: IdleExited, Name: id + " (pane with no claim, CLI exited)", Pane: pane, agent: id})
			}
		case launch != "" && fscope == scope:
			if paneExited(sock, pane) {
				out = append(out, IdleSession{Kind: IdleExited, Label: label, Name: label + " (free, " + tool + " exited)", Pane: pane, free: true})
			}
		}
	}
	return out
}

// paneExited reports whether a pane is dead or runs only its shell.
func paneExited(socket, pane string) bool {
	cmd := exec.Command("tmux", TmuxArgs(socket, "display-message", "-p", "-t", pane, "#{pane_dead}\t#{pane_current_command}")...)
	cmd.Env = withoutTMUX(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	dead, command, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if dead == "1" {
		return true
	}
	switch strings.TrimPrefix(filepath.Base(command), "-") {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "tcsh", "csh", "nu", "pwsh", "powershell":
		return true
	}
	return false
}

// idleFor is how long ago the latest of the times was, if over IdleAfter.
func idleFor(now time.Time, times ...string) (time.Duration, bool) {
	last := ""
	for _, at := range times {
		if at > last {
			last = at
		}
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", last)
	if err != nil || now.Sub(t) < IdleAfter {
		return 0, false
	}
	return now.Sub(t), true
}

// CloseIdle closes one entry of IdleSessions' list.
func (p *Project) CloseIdle(s IdleSession) (string, error) {
	sock, _ := DefaultSocket()
	switch {
	case s.free:
		if label, _, ok := FreeByPane(s.Pane); !ok || label != s.Label {
			return "", fail(ExitFail, "not_found", "pane %s no longer runs the free session %s; left alone", s.Pane, s.Label)
		}
		return KillFree(s.Pane, sock, s.Label)
	case s.Label != "":
		return p.Kill(s.Label)
	}
	// An exited agent pane with no claim: re-check, then close it.
	mark, _ := exec.Command("tmux", TmuxArgs(sock, "show-options", "-p", "-v", "-t", s.Pane, "@sunstack")...).Output()
	if strings.TrimSpace(string(mark)) != p.Root+"|"+s.agent || !paneExited(sock, s.Pane) {
		return "", fail(ExitFail, "not_exited", "pane %s is no longer an exited %s pane; left alone", s.Pane, s.agent)
	}
	if err := exec.Command("tmux", TmuxArgs(sock, "kill-pane", "-t", s.Pane)...).Run(); err != nil {
		return "", fail(ExitFail, "tmux", "could not close pane %s: %v", s.Pane, err)
	}
	p.LogEvent("kill", s.agent, "pane="+s.Pane+" exited")
	return "closed " + s.Name + " (pane " + s.Pane + ")", nil
}

// IdleIssue is next's one line for the idle sessions and exited panes, if any.
func IdleIssue(idle []IdleSession) (Issue, bool) {
	if len(idle) == 0 {
		return Issue{}, false
	}
	var parts []string
	for _, k := range []struct{ kind, what string }{
		{IdleAgent, "agent session(s) idle over 24h"},
		{IdleFree, "free session(s) idle over 24h"},
		{IdleExited, "pane(s) whose CLI exited"},
	} {
		var names []string
		for _, s := range idle {
			if s.Kind != k.kind {
				continue
			}
			if s.For > 0 {
				names = append(names, fmt.Sprintf("%s (%dd)", s.Name, int(s.For.Hours())/24))
			} else {
				names = append(names, s.Name)
			}
		}
		if len(names) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s: %s", len(names), k.what, strings.Join(names, ", ")))
		}
	}
	return Issue{"user", "user", strings.Join(parts, "; "),
		"sunstack clean closes them after one confirmation (clean skill)"}, true
}
