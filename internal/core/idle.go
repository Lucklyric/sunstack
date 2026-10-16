package core

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// IdleAfter is how long an agent session may sit at its prompt, with nothing
// unfinished, before sunstack next offers to close it.
const IdleAfter = 24 * time.Hour

// IdleSession is an agent session that can be closed: kill --idle's list.
type IdleSession struct {
	Label string        // <id>_<task>, what kill takes
	Pane  string        // tmux pane
	For   time.Duration // since its last activity
}

// IdleSessions lists the team's agent sessions that run in a tmux pane, are
// at their prompt, have done nothing for IdleAfter (no claim, save, doing
// line or transcript change), hold no taken message and no staged save. The
// calling session's own pane is never listed. sessions is ScanSessions'.
func (p *Project) IdleSessions(sessions []*HostSession, now time.Time) []IdleSession {
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
			last := ""
			for _, at := range []string{c.Claimed, c.LastContact, c.DoingAt, s.Activity.LastActive} {
				if at > last {
					last = at
				}
			}
			t, err := time.Parse("2006-01-02T15:04:05Z", last)
			if err != nil || now.Sub(t) < IdleAfter {
				continue
			}
			out = append(out, IdleSession{Label: name, Pane: c.TmuxPane, For: now.Sub(t)})
		}
	}
	return out
}

// IdleIssue is next's one line for the idle sessions, if any.
func IdleIssue(idle []IdleSession) (Issue, bool) {
	if len(idle) == 0 {
		return Issue{}, false
	}
	var names []string
	for _, s := range idle {
		names = append(names, fmt.Sprintf("%s (%dd)", s.Label, int(s.For.Hours())/24))
	}
	return Issue{"user", "user", fmt.Sprintf("%d agent session(s) idle over 24h: %s", len(idle), strings.Join(names, ", ")),
		"sunstack kill --idle closes them after one confirmation (spawn skill)"}, true
}
