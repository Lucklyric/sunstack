package core

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/assets"
)

// Halt (§17.3): one line in BOARD.md that pauses the whole team. Work in
// progress finishes, is saved and stops; nothing new starts. Only the user
// sets or clears it.

var haltRe = regexp.MustCompile(`(?m)^halt:[ \t]*(\S+)[ \t]+(.+?)[ \t]*\n?$`)

// Halted returns the halt line's date and reason, if the team is halted.
func (p *Project) Halted() (date, reason string, ok bool) {
	doc, _, err := readMaybe(p.teamBoardPath())
	if err != nil {
		// A halt that cannot be read must not end quietly.
		return today(), fmt.Sprintf("BOARD.md cannot be read (%v); treat the team as halted until it can", err), true
	}
	if m := haltRe.FindSubmatch(doc); m != nil {
		return string(m[1]), string(m[2]), true
	}
	return "", "", false
}

// HaltLine is the banner every session sees while the team is halted.
func (p *Project) HaltLine() string {
	if date, reason, ok := p.Halted(); ok {
		return fmt.Sprintf("TEAM HALTED since %s: %s. Finish and save the work in progress, start nothing new, take no new task or handoff.", date, reason)
	}
	return ""
}

// Halt sets the halt line, or with reason "" clears it.
func (p *Project) Halt(reason string) error {
	reason = strings.Join(strings.Fields(reason), " ")
	unlock, err := p.lock(TeamID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := p.noSymlink(p.teamBoardPath()); err != nil {
		return err
	}
	doc, ok, err := readMaybe(p.teamBoardPath())
	if err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if !ok {
		doc = []byte(assets.TeamBoardTemplate)
	}
	if HasConflictMarkers(doc) {
		return fail(ExitFail, "conflict", "merge conflict markers in BOARD.md; resolve them first")
	}
	doc = haltRe.ReplaceAll(doc, nil)
	if reason != "" {
		doc = append([]byte(fmt.Sprintf("halt: %s %s\n", today(), reason)), doc...)
	}
	if err := writeAtomic(p.teamBoardPath(), doc); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if reason == "" {
		p.LogEvent("halt", "team", "off")
	} else {
		p.LogEvent("halt", "team", "on")
	}
	return nil
}

// Findings is everything on the team that needs attention: the halt, the
// boards and the task ledger.
func (p *Project) Findings(today time.Time) []Issue {
	var out []Issue
	if date, reason, ok := p.Halted(); ok {
		out = append(out, Issue{"broken", "user", fmt.Sprintf("the team is halted since %s: %s", date, reason), "fix the cause, then sunstack halt --off"})
	}
	out = append(out, p.LoadBoards().Findings(today)...)
	return append(out, p.taskFindings(today)...)
}

// IssueTexts is the text of Findings.
func (p *Project) IssueTexts(today time.Time) []string {
	var out []string
	for _, is := range p.Findings(today) {
		out = append(out, is.Text)
	}
	return out
}
