package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The delegation ledger (§17.2), derived from messages, never stored: a task
// is open until a done message replies to it. A task from the user or
// another team, whose reply cannot land in this team, is also closed once
// its recipient acks it.

// openDays is how long a task may stay open before the board flags it.
const openDays = 3

// TaskInfo is one task message and what came of it.
type TaskInfo struct {
	ID, From, To, Session, At, Goal, Follows string
	State                                    string // pending, taken, acked
	Replies                                  []TaskReply
	Open                                     bool
}

// TaskReply is a done message answering a task.
type TaskReply struct {
	ID, At   string
	Verified bool // it carries a verified: line
	mod      time.Time
}

// allMessages reads every message of the team: pending, taken and archived.
func (p *Project) allMessages() []*Message {
	var out []*Message
	read := func(dir, state string) {
		files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, f := range files {
			if m, err := parseMessage(f); err == nil {
				m.State = state
				if st, err := os.Stat(f); err == nil {
					m.mod = st.ModTime()
				}
				out = append(out, m)
			}
		}
	}
	ids, _ := os.ReadDir(p.local("inbox"))
	for _, d := range ids {
		if !d.IsDir() {
			continue
		}
		read(p.inboxDir(d.Name()), "pending")
		tokens, _ := os.ReadDir(p.local("inbox", d.Name(), ".taken"))
		for _, tk := range tokens {
			read(p.local("inbox", d.Name(), ".taken", tk.Name()), "taken")
		}
	}
	read(p.messageArchive(), "acked")
	return out
}

// findMessage finds one message of the team by ID, wherever it is.
func (p *Project) findMessage(id string) *Message {
	for _, m := range p.allMessages() {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// Tasks lists every task message with its replies, oldest first.
func (p *Project) Tasks() []*TaskInfo {
	msgs := p.allMessages()
	// A reply counts only when it comes from the agent the task went to.
	to := map[string]string{}
	for _, m := range msgs {
		if m.Type == "task" {
			to[m.ID] = m.To
		}
	}
	replies := map[string][]TaskReply{}
	for _, m := range msgs {
		if m.Type == "done" && m.ReplyTo != "" && to[m.ReplyTo] != "" && m.From == to[m.ReplyTo] {
			replies[m.ReplyTo] = append(replies[m.ReplyTo], TaskReply{ID: m.ID, At: m.At, Verified: hasVerifiedLine(m.Body), mod: m.mod})
		}
	}
	for id := range replies {
		// Oldest first, wherever stored: by the second in the ID, then by the
		// file's time, which a move to the archive keeps.
		rs := replies[id]
		sort.Slice(rs, func(i, j int) bool {
			if a, b := rs[i].ID[:16], rs[j].ID[:16]; a != b {
				return a < b
			}
			return rs[i].mod.Before(rs[j].mod)
		})
	}
	var out []*TaskInfo
	for _, m := range msgs {
		if m.Type != "task" {
			continue
		}
		t := &TaskInfo{ID: m.ID, From: m.From, To: m.To, Session: m.Session, At: m.At, Follows: m.Follows, State: m.State, Replies: replies[m.ID]}
		for _, l := range strings.Split(m.Body, "\n") {
			if strings.HasPrefix(strings.ToLower(l), "goal:") {
				t.Goal = strings.TrimSpace(l)
				break
			}
		}
		ownSender := p.HasAgent(m.From)
		t.Open = len(t.Replies) == 0 && (ownSender || m.State != "acked")
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// taskFindings flags tasks open too long, and chains that failed twice.
func (p *Project) taskFindings(today time.Time) []Issue {
	var out []Issue
	tasks := p.Tasks()
	byID := map[string]*TaskInfo{}
	followed := map[string]bool{}
	for _, t := range tasks {
		byID[t.ID] = t
		if t.Follows != "" {
			followed[t.Follows] = true
		}
	}
	for _, t := range tasks {
		owner := t.From
		if !p.HasAgent(owner) {
			owner = "user"
		}
		if at, err := time.Parse("2006-01-02T15:04:05Z", t.At); err == nil && t.Open && today.Sub(at) > openDays*24*time.Hour {
			out = append(out, Issue{"blocked", owner, fmt.Sprintf("task %s from %s to %s has been open since %s", t.ID, t.From, t.To, at.Format("2006-01-02")),
				"sunstack tasks; check progress read-only (org --by agent, peek), never ask \"are you done?\""})
		}
		if followed[t.ID] {
			continue // judged at the end of its chain
		}
		// A round failed when its latest reply carries no verified: line.
		failed := 0
		for c := t; c != nil; c = byID[c.Follows] {
			if n := len(c.Replies); n > 0 && !c.Replies[n-1].Verified {
				failed++
			}
			if c.Follows == "" {
				break
			}
		}
		lastOK := len(t.Replies) > 0 && t.Replies[len(t.Replies)-1].Verified
		if failed >= 2 && !lastOK {
			out = append(out, Issue{"user", owner, fmt.Sprintf("task %s from %s to %s failed twice (replies without verified:); send no third round", t.ID, t.From, t.To),
				owner + " makes it an ask to the user (save skill)"})
		}
	}
	return out
}

// TasksText renders sunstack tasks.
func (p *Project) TasksText(all bool, from string) string {
	var b strings.Builder
	n := 0
	for _, t := range p.Tasks() {
		if (!all && !t.Open) || (from != "" && t.From != from) {
			continue
		}
		n++
		to := t.To
		if t.Session != "" {
			to = t.Session
		}
		state := t.State
		if !t.Open {
			state = "closed"
		}
		fmt.Fprintf(&b, "%s  %s -> %s  %s  since %s\n", t.ID, t.From, to, state, strings.SplitN(t.At, "T", 2)[0])
		if t.Goal != "" {
			fmt.Fprintf(&b, "    %s\n", t.Goal)
		}
		if t.Follows != "" {
			fmt.Fprintf(&b, "    follows %s\n", t.Follows)
		}
		for _, r := range t.Replies {
			v := "done with verified:"
			if !r.Verified {
				v = "done without verified:"
			}
			fmt.Fprintf(&b, "    reply %s: %s\n", r.ID, v)
		}
	}
	if n == 0 {
		if all {
			return "no tasks\n"
		}
		return "no open tasks\n"
	}
	return b.String()
}

// followContext quotes an earlier task and its replies for a follow-up
// (§17.4), so the new round carries the whole scope.
func (p *Project) followContext(id string) (string, error) {
	if !msgIDRe.MatchString(id) {
		return "", fail(ExitUsage, "usage", "invalid --follows message id: %s", id)
	}
	prev := p.findMessage(id)
	if prev == nil {
		return "", fail(ExitFail, "not_found", "no message %s in this team", id)
	}
	if prev.Type != "task" {
		return "", fail(ExitUsage, "usage", "--follows names a task; %s is a %s", id, prev.Type)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nContext: earlier round %s, quoted below\n", id)
	quote := func(text string) {
		for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			fmt.Fprintf(&b, "  > %s\n", l)
		}
	}
	quote(prev.Body)
	for _, m := range p.allMessages() {
		if m.Type == "done" && m.ReplyTo == id {
			fmt.Fprintf(&b, "  > reply %s from %s:\n", m.ID, m.From)
			quote(m.Body)
		}
	}
	return b.String(), nil
}

// hasVerifiedLine says whether a reply carries its own verified: line with
// something after it. Quoted lines and "not verified:" do not count.
func hasVerifiedLine(body string) bool {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, ">") {
			continue
		}
		l = strings.TrimPrefix(strings.ToLower(l), "(")
		if v, ok := strings.CutPrefix(l, "verified:"); ok && strings.TrimSpace(strings.TrimSuffix(v, ")")) != "" {
			return true
		}
	}
	return false
}
