package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Next (§17.1) merges what checkup, org and board each report into one
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
		switch {
		case c.Area == "board":
			// The same findings come typed below.
		case c.Level == "fail":
			out = append(out, Issue{"broken", "user", c.Msg, c.Fix})
		case c.Level == "warn" && c.Area == "migrate":
			out = append(out, Issue{"user", "user", c.Msg, "checkup skill (" + c.Fix + ")"})
		case c.Level == "warn":
			out = append(out, Issue{"drift", "user", c.Msg, c.Fix})
		case c.Level == "next" && strings.Contains(c.Msg, "unread message"):
			out = append(out, Issue{"blocked", agentOf(c.Msg), c.Msg, c.Fix})
		case c.Level == "next":
			out = append(out, Issue{"user", agentOf(c.Msg), c.Msg, c.Fix})
		}
	}
	out = append(out, p.Findings(today)...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := tierRank(out[i].Kind), tierRank(out[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return caller != "" && out[i].Owner == caller && out[j].Owner != caller
	})
	if caller != "" {
		if it, ok := p.ownNext(caller); ok {
			out = append(out, Issue{"yours", caller, fmt.Sprintf("%s: %s %s", caller, it.Key, it.Text), "work on it as " + caller + ", then save"})
		}
	}
	return out
}

// agentOf takes the agent ID that starts a health message, if any.
func agentOf(msg string) string {
	if f := strings.Fields(msg); len(f) > 0 && strings.Contains(f[0], ".") {
		return f[0]
	}
	return "user"
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
func NextText(items []Issue, max int) string {
	if len(items) == 0 {
		return "Nothing needs doing: no problems, nothing waiting on the user, no open work on the boards.\n"
	}
	var b strings.Builder
	shown := items
	if max > 0 && len(items) > max {
		shown = items[:max]
	}
	for i, it := range shown {
		fmt.Fprintf(&b, "%d. [%s] %s\n   do: %s\n", i+1, it.Kind, it.Text, it.Do)
	}
	if len(shown) < len(items) {
		fmt.Fprintf(&b, "(%d more: sunstack next --all)\n", len(items)-len(shown))
	}
	return b.String()
}
