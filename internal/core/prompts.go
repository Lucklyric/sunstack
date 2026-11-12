package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Trust screens (§37): after the user's one-time consent, a session Sunstack
// starts gets past Codex's hook review and Claude Code's folder trust check.

// Prompts is the user's choice per screen: ask (the default), on or off.
type Prompts struct {
	CodexHooks    string `json:"codex_hooks"`
	ClaudeFolders string `json:"claude_folders"`
}

func promptsPath() string { return filepath.Join(Home(), "prompts.json") }

// LoadPrompts reads the choices; anything unset or unknown is ask.
func LoadPrompts() Prompts {
	var p Prompts
	if b, err := os.ReadFile(promptsPath()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	norm := func(v string) string {
		if v == "on" || v == "off" {
			return v
		}
		return "ask"
	}
	return Prompts{norm(p.CodexHooks), norm(p.ClaudeFolders)}
}

// SavePrompts records the user's choices.
func SavePrompts(p Prompts) error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := writeAtomic(promptsPath(), append(b, '\n')); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	return nil
}

// trustScreen is one screen the watch knows: its title line, its options in
// order, which of them is the answer, and the selection marker.
type trustScreen struct {
	tool, mark, title, marker, setting, flag, footer string
	body                                             *regexp.Regexp // the text from the title to the options, lines joined by one space
	options                                          []string
	answer                                           int
}

var trustScreens = []trustScreen{
	{tool: "codex", title: "Hooks need review", marker: "›", setting: "codex", flag: "--codex-hooks", mark: "Codex's hook review",
		body:    regexp.MustCompile(`^Hooks need review [1-9][0-9]? hooks? (is|are) new or changed\. Hooks can run outside the sandbox after you trust them\.$`),
		options: []string{"Review hooks", "Trust all and continue", "Continue without trusting (hooks won't run)"}, answer: 1,
		footer: "enter confirm · esc skip"},
	{tool: "claude", title: "Quick safety check: Is this a project you created or one you trust?", marker: "❯", setting: "claude", flag: "--claude-folders", mark: "Claude Code's folder trust check",
		body:    regexp.MustCompile(`^` + regexp.QuoteMeta("Quick safety check: Is this a project you created or one you trust? (Like your own code, a well-known open source project, or work from your team). If not, take a moment to review what's in this folder first. Claude Code'll be able to read, edit, and execute files here. Security guide") + `$`),
		options: []string{"No, exit", "Yes, I trust this folder"}, answer: 1,
		footer: "Enter to confirm · Esc to cancel"},
}

// trustWatch is how long the watch looks for a trust screen after launch.
var trustWatch = 15 * time.Second

// parseMenu reads a trust screen exactly: never with the CLI's input box
// showing, the title as its own line, then every option in order as a full
// line (numbered for Codex), and exactly one line on the whole screen
// carrying the selection marker, on an option. It returns the selected
// option.
func parseMenu(screen string, ts trustScreen) (int, bool) {
	if ReadyForInput(ts.tool, screen) {
		return 0, false
	}
	lines := strings.Split(screen, "\n")
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == ts.title || (ts.tool == "claude" && strings.HasPrefix(t, ts.title)) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, false
	}
	marked := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), ts.marker) {
			marked++
		}
	}
	if marked != 1 {
		return 0, false
	}
	// The text from the title to the first option is the known text, however
	// the screen wraps it; then the options are one block: every non-blank
	// line is the next known option in full; then the CLI's own footer.
	first := func(t string) bool {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), ts.marker))
		if ts.tool == "codex" {
			return t == "1. "+ts.options[0]
		}
		return t == ts.options[0]
	}
	opt := -1
	var body []string
	for i := start; i < len(lines); i++ {
		if first(lines[i]) {
			opt = i
			break
		}
		if t := strings.TrimSpace(lines[i]); t != "" {
			body = append(body, strings.Join(strings.Fields(t), " "))
		}
	}
	if opt < 0 || !ts.body.MatchString(strings.Join(body, " ")) {
		return 0, false
	}
	next, sel, footer := 0, -1, false
	for _, l := range lines[opt:] {
		t := strings.TrimSpace(l)
		if footer {
			if t != "" {
				return 0, false // nothing may follow the footer
			}
			continue
		}
		if next == len(ts.options) {
			if t == "" {
				continue
			}
			if strings.Join(strings.Fields(t), " ") != ts.footer {
				return 0, false
			}
			footer = true
			continue
		}
		on := strings.HasPrefix(t, ts.marker)
		t = strings.TrimSpace(strings.TrimPrefix(t, ts.marker))
		want := ts.options[next]
		if ts.tool == "codex" {
			want = string(rune('1'+next)) + ". " + want
		}
		if t != want {
			return 0, false // the block is broken, or an option changed
		}
		if on {
			sel = next
		}
		next++
	}
	if next != len(ts.options) || sel < 0 || !footer {
		return 0, false
	}
	return sel, true
}

// AnswerTrust watches a newly started pane for a trust screen and answers it
// when the user turned that choice on, only inside a team for Claude's
// folder check. It does nothing when the CLI is not running, and stops when
// the CLI exits, tmux cannot be read, or the CLI shows its input box. It
// returns a line for the user, or "" when no screen showed.
func AnswerTrust(socket, pane, tool string, inTeam, running bool) string {
	if !running {
		return ""
	}
	choice := LoadPrompts()
	begin := time.Now()
	deadline := begin.Add(trustWatch)
	looks := "" // a trust title seen on a screen that did not match yet
	for time.Now().Before(deadline) {
		screen := paneScreen(socket, pane)
		if screen == "" || !paneRunsTool(socket, pane, tool) {
			return "" // gone, or tmux cannot say: nothing to answer
		}
		if CLIReady(tool, screen) {
			return ""
		}
		for _, ts := range trustScreens {
			if ts.tool != tool || !strings.Contains(screen, ts.title) {
				continue
			}
			if _, ok := parseMenu(screen, ts); !ok {
				// It may still be drawing: read again until the watch ends.
				looks = "waiting at what looks like " + ts.mark + " in pane " + pane + ", but the screen is not the known one; answer it there"
				continue
			}
			on := choice.CodexHooks == "on"
			if ts.setting == "claude" {
				on = choice.ClaudeFolders == "on" && inTeam
			}
			if !on {
				setting := choice.CodexHooks
				if ts.setting == "claude" {
					setting = choice.ClaudeFolders
				}
				why := "answer it there"
				if setting == "ask" {
					why += ", or let sunstack answer it from now on: sunstack prompts " + ts.flag + " on"
				}
				if ts.setting == "claude" && !inTeam {
					why = "answer it there; sunstack answers it only in a team folder"
				}
				return "waiting at " + ts.mark + " in pane " + pane + ": " + why
			}
			if selectAnswer(socket, pane, ts) {
				return "answered " + ts.mark + ": " + ts.options[ts.answer] + " (sunstack prompts)"
			}
			return "waiting at " + ts.mark + " in pane " + pane + ": the screen changed while answering; answer it there"
		}
		if looks == "" && time.Since(begin) > trustAppear {
			return "" // no trust screen showed at startup
		}
		time.Sleep(300 * time.Millisecond)
	}
	return looks
}

// trustAppear is how long the watch waits for a trust screen to show at all.
var trustAppear = 6 * time.Second

// selectAnswer moves down one line at a time, reading the whole menu again
// after each key, and presses Enter only when a fresh read shows the exact
// menu with the answer, and only it, selected.
func selectAnswer(socket, pane string, ts trustScreen) bool {
	for i := 0; i <= len(ts.options); i++ {
		sel, ok := parseMenu(paneScreen(socket, pane), ts)
		if !ok || sel > ts.answer {
			return false
		}
		if sel == ts.answer {
			time.Sleep(150 * time.Millisecond)
			if again, ok := parseMenu(paneScreen(socket, pane), ts); !ok || again != ts.answer {
				return false
			}
			// Bounded: a stuck tmux must not hold the sweep's lock (§46.4).
			_, err := quickTmux(socket, "send-keys", "-t", pane, "Enter")
			return err == nil
		}
		if _, err := quickTmux(socket, "send-keys", "-t", pane, "Down"); err != nil {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// SweepHookReviews looks at every tmux pane on the default server and the
// caller's for Codex's hook review, whoever started the session and however
// long ago (§46.4), and answers it when the user chose on. It reads the
// screen exactly as the watch at launch does, and returns one line per pane
// it answered or left waiting. Only one sweep runs at a time on a host.
func SweepHookReviews() []string {
	var ts trustScreen
	for _, t := range trustScreens {
		if t.setting == "codex" {
			ts = t
		}
	}
	var panes []paneInfo
	for _, p := range tmuxPanes() {
		// Codex runs as node or under its own name; every other pane is
		// skipped without reading its screen.
		if c := strings.ToLower(p.cmd); c == "node" || strings.Contains(c, "codex") {
			panes = append(panes, p)
		}
	}
	if len(panes) == 0 {
		return nil
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].id < panes[j].id })
	unlock, err := LockDir(filepath.Join(Home(), "locks", "prompts-sweep"), "the hook review sweep")
	if err != nil {
		return nil
	}
	defer unlock()
	on := LoadPrompts().CodexHooks == "on"
	var lines []string
	for _, p := range panes {
		screen := paneScreen(p.socket, p.id)
		if !strings.Contains(screen, ts.title) || ReadyForInput(ts.tool, screen) || !paneRunsTool(p.socket, p.id, ts.tool) {
			continue
		}
		where := fmt.Sprintf("pane %s (%s)", p.id, p.where)
		switch _, known := parseMenu(screen, ts); {
		case !known:
			lines = append(lines, where+" waits at what looks like "+ts.mark+", but the screen is not the known one; answer it there")
		case !on:
			lines = append(lines, where+" waits at "+ts.mark+": answer it there, or let sunstack answer it: sunstack prompts "+ts.flag+" on")
		case selectAnswer(p.socket, p.id, ts):
			lines = append(lines, where+": answered "+ts.mark+": "+ts.options[ts.answer])
		default:
			lines = append(lines, where+" waits at "+ts.mark+": the screen changed while answering; answer it there")
		}
	}
	return lines
}
