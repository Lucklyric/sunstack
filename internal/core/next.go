package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Next (§17.1) merges what health, org and board each report into one
// ranked list of actions, each with the command or skill that acts on it.
// It only reads: the health checks, the findings and the caller's board.

// NextTiers rank actions, most urgent first.
var NextTiers = []string{"broken", "user", "blocked", "drift", "yours"}

func tierRank(t string) int {
	for i, x := range NextTiers {
		if x == t {
			return i
		}
	}
	return len(NextTiers)
}

// Next ranks what to do now. Items of the caller's own agent come first
// within each tier, and its own next board entry closes the list.
func (p *Project) Next(caller string, today time.Time) []Issue {
	var out []Issue
	for _, c := range p.Health() {
		if is, ok := CheckIssue(c); ok {
			out = append(out, is)
		}
	}
	out = append(out, p.waiting()...)
	out = append(out, p.Findings(today)...)
	Rank(out, caller)
	if caller != "" {
		if it, ok := p.ownNext(caller); ok {
			out = append(out, Issue{"yours", caller, fmt.Sprintf("%s: %s %s", caller, it.Key, it.Text), "work on it as " + caller + ", then save"})
		}
	}
	return out
}

// SessionNext is what waits on one session that works as no agent: the
// messages in its host inbox, and the tasks it sent that are still open in
// any team on this host.
func SessionNext(sid string) []Issue {
	if !IsSessionID(sid) {
		return nil
	}
	var out []Issue
	ms, _ := HostCheck(sid)
	for _, m := range ms {
		out = append(out, Issue{"yours", "user", fmt.Sprintf("%s %s from %s waits for this session (%s)", m.Type, m.ID, m.From, m.State), "sunstack check --session"})
	}
	for _, p := range IndexedProjects() {
		name := filepath.Base(p.Root)
		if tf, ok := p.Team(); ok && tf.Name != "" {
			name = tf.Name
		}
		for _, t := range p.Tasks() {
			if t.Open && t.FromSession == sid {
				out = append(out, Issue{"blocked", "user", fmt.Sprintf("%s %s from this session to %s/%s has no reply yet (%s since %s)", t.Type, t.ID, name, t.To, t.State, strings.SplitN(t.At, "T", 2)[0]),
					"wait for the done reply; sunstack tasks --root " + p.Root + " shows it"})
			}
		}
	}
	return out
}

// CheckIssue turns a failed or warning health check into an action; a
// warning with nothing to do is only health's.
func CheckIssue(c Check) (Issue, bool) {
	switch {
	case c.Fix == "":
		return Issue{}, false
	case c.Level == "fail":
		return Issue{"broken", "user", c.Msg, c.Fix}, true
	case c.Level == "warn" && c.Area == "migrate":
		return Issue{"user", "user", c.Msg, c.Fix}, true
	case c.Level == "warn" && c.Area == "install":
		return Issue{"broken", "user", c.Msg, c.Fix}, true
	case c.Level == "warn":
		return Issue{"drift", "user", c.Msg, c.Fix}, true
	}
	return Issue{}, false
}

// Rank sorts actions by tier, the caller's own first within a tier.
func Rank(items []Issue, caller string) {
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := tierRank(items[i].Kind), tierRank(items[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return caller != "" && items[i].Owner == caller && items[j].Owner != caller
	})
}

// waiting is the team's work that waits on the user or on a session.
func (p *Project) waiting() []Issue {
	var out []Issue
	status := p.Status()
	if len(status) == 0 {
		out = append(out, Issue{"user", "user", "no agents hired yet", "hire skill, or sunstack hire <title> <name> (sunstack library lists titles)"})
	}
	for _, s := range status {
		if n := len(s.Proposals); n > 0 {
			out = append(out, Issue{"user", s.ID, fmt.Sprintf("%s has %d rule change(s) waiting for your approval", s.ID, n),
				"sunstack as " + s.ID + ", then the save skill asks you about each"})
		}
		if s.Inbox > 0 && len(s.Claims) == 0 {
			out = append(out, Issue{"blocked", s.ID, fmt.Sprintf("%s has %d unread message(s) and no session", s.ID, s.Inbox),
				"sunstack spawn " + s.ID})
		}
	}
	return out
}

// ownNext is the caller's first open Now entry, else its first Next entry.
func (p *Project) ownNext(id string) (Item, bool) {
	doc, _, _ := readMaybe(p.boardPath(id))
	items, _ := parseBoard(id, doc)
	for _, sec := range []string{"Now", "Next"} {
		for _, it := range items {
			if it.Section == sec && !it.Done {
				return it, true
			}
		}
	}
	return Item{}, false
}

// NextText renders the list, the first max items unless max is 0.
// all is the command that lists every item; empty means a team's own.
func NextText(items []Issue, max int, all string) string {
	if all == "" {
		all = "sunstack next --all"
	}
	if len(items) == 0 {
		return "Nothing needs doing: no problems, nothing waiting on the user, no open work on the boards.\n"
	}
	var b strings.Builder
	if max > 0 {
		items = collapseDrift(items)
	}
	shown := items
	if max > 0 && len(items) > max {
		shown = items[:max]
	}
	for i, it := range shown {
		fmt.Fprintf(&b, "%d. [%s] %s\n   do: %s\n", i+1, it.Kind, it.Text, it.Do)
	}
	if len(shown) < len(items) {
		fmt.Fprintf(&b, "(%d more: %s)\n", len(items)-len(shown), all)
	}
	return b.String()
}

// collapseDrift folds the drift tier into one line, so housekeeping the
// agents do at their next save never pushes real actions off the list.
func collapseDrift(items []Issue) []Issue {
	var out []Issue
	n, at := 0, -1
	for _, it := range items {
		if it.Kind != "drift" {
			out = append(out, it)
			continue
		}
		if n++; at < 0 {
			at = len(out)
			out = append(out, it)
		}
	}
	if n > 1 {
		out[at] = Issue{"drift", "user", fmt.Sprintf("%d board or setup item(s) have drifted; the agents fix theirs at their next save", n), "sunstack next --all lists them"}
	}
	return out
}
