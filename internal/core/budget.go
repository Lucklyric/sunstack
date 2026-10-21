package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// What a session loads stays small (§27): tidy keeps the newest Done
// entries and archives old processed messages; an agent over budget is
// flagged by tidy, health and next.

const (
	keepDone      = 10       // Done entries kept on an agent's board
	keepProcessed = 10       // processed-message entries kept in context
	contextBudget = 25 << 10 // context.md without processed messages
	boardBudget   = 40       // board.md lines after tidy
	processedSec  = "Processed messages"
)

// doneOverflow returns the line numbers of the Done entries past the
// keepDone newest (by date, then by position).
func doneOverflow(owner string, lines []string) map[int]bool {
	type entry struct {
		i    int
		date string
	}
	var done []entry
	sec := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			sec = strings.TrimPrefix(t, "## ")
		}
		if sec != "Done" {
			continue
		}
		if items, _ := parseBoard(owner, []byte("## Done\n"+t)); len(items) == 1 {
			done = append(done, entry{i, items[0].Date})
		}
	}
	sort.SliceStable(done, func(a, b int) bool { return done[a].date > done[b].date })
	out := map[int]bool{}
	for k := keepDone; k < len(done); k++ {
		out[done[k].i] = true
	}
	return out
}

// tidyContext moves processed-message entries that are old to the archive,
// except those whose message is still pending or taken in the inbox, where
// the lost-ack check needs them. The caller holds the agent's lock.
func (p *Project) tidyContext(id string, old func(date string) bool) (int, error) {
	path := filepath.Join(p.AgentDir(id), "context.md")
	if err := p.noSymlink(path); err != nil {
		return 0, err
	}
	doc, ok, err := readMaybe(path)
	if err != nil {
		return 0, fail(ExitFail, "fs", "%v", err)
	}
	if !ok || HasConflictMarkers(doc) {
		return 0, nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n"), "\n")
	// The keepProcessed newest entries stay, like Done entries on the board.
	type entry struct {
		i    int
		date string
	}
	var all []entry
	sec := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			sec = strings.TrimPrefix(t, "## ")
		}
		if f := strings.Fields(t); sec == processedSec && len(f) >= 3 && f[0] == "-" && dateOK(f[1]) {
			all = append(all, entry{i, f[1]})
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].date > all[b].date })
	extra := map[int]bool{}
	for k := keepProcessed; k < len(all); k++ {
		extra[all[k].i] = true
	}
	moved := map[string][]string{}
	var keep []string
	sec, n := "", 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			sec = strings.TrimPrefix(t, "## ")
		}
		if f := strings.Fields(t); sec == processedSec && len(f) >= 3 && f[0] == "-" && dateOK(f[1]) && (old(f[1]) || extra[i]) && !p.inInbox(id, f[2]) {
			moved[f[1][:7]] = append(moved[f[1][:7]], t)
			n++
			continue
		}
		keep = append(keep, l)
	}
	if n == 0 {
		return 0, nil
	}
	// Archive first: a crash in between leaves an entry in both, never in neither.
	for month, lines := range moved {
		ap := filepath.Join(p.archiveDir(id), month+".md")
		if err := p.noSymlink(ap); err != nil {
			return 0, err
		}
		cur, _, err := readMaybe(ap)
		if err != nil {
			return 0, fail(ExitFail, "fs", "%v", err)
		}
		for _, l := range lines {
			cur = appendToSection(cur, processedSec, l)
		}
		if err := writeAtomic(ap, cur); err != nil {
			return 0, fail(ExitFail, "fs", "%v", err)
		}
	}
	if err := writeAtomic(path, []byte(strings.Join(keep, "\n")+"\n")); err != nil {
		return 0, fail(ExitFail, "fs", "%v", err)
	}
	p.LogEvent("tidy", id, fmt.Sprintf("archived=%d processed messages", n))
	return n, nil
}

// inInbox reports whether a message is still pending or taken for id.
func (p *Project) inInbox(id, msgID string) bool {
	if !IsMessageID(msgID) {
		return false
	}
	if _, err := os.Stat(p.msgPath(p.inboxDir(id), msgID)); err == nil {
		return true
	}
	ds, _ := os.ReadDir(filepath.Join(p.inboxDir(id), ".taken"))
	for _, d := range ds {
		if _, err := os.Stat(p.msgPath(p.takenDir(id, d.Name()), msgID)); err == nil {
			return true
		}
	}
	return false
}

// OverBudget says what makes an agent's loaded files too large, or "".
func (p *Project) OverBudget(id string) string {
	var out []string
	if doc, err := os.ReadFile(filepath.Join(p.AgentDir(id), "context.md")); err == nil {
		size, sec := 0, ""
		for _, l := range strings.SplitAfter(string(doc), "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "## ") {
				sec = strings.TrimPrefix(t, "## ")
			}
			if sec != processedSec {
				size += len(l)
			}
		}
		if size > contextBudget {
			out = append(out, fmt.Sprintf("%s/context.md is %d KB, over %d KB without processed messages: archive superseded decisions and fixed pitfalls", id, size>>10, contextBudget>>10))
		}
	}
	if doc, err := os.ReadFile(p.boardPath(id)); err == nil {
		if n := strings.Count(strings.TrimRight(string(doc), "\n"), "\n") + 1; n > boardBudget {
			out = append(out, fmt.Sprintf("%s/board.md has %d lines, over %d: shorten Next", id, n, boardBudget))
		}
	}
	return strings.Join(out, "; ")
}

func dateOK(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
