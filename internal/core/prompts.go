package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

// trustScreen is one screen the watch knows, by its exact text.
type trustScreen struct {
	tool, mark, title, answer, marker, setting, flag string
}

var trustScreens = []trustScreen{
	{tool: "codex", title: "Hooks need review", answer: "Trust all and continue", marker: "›", setting: "codex", flag: "--codex-hooks", mark: "Codex's hook review"},
	{tool: "claude", title: "Is this a project you created or one you trust?", answer: "Yes, I trust this folder", marker: "❯", setting: "claude", flag: "--claude-folders", mark: "Claude Code's folder trust check"},
}

// trustWatch is how long the watch looks for a trust screen after launch.
var trustWatch = 15 * time.Second

// AnswerTrust watches a newly started pane for a trust screen and answers it
// when the user turned that choice on, only inside a team for Claude's
// folder check. It moves the selection to the answer, checks that exactly
// that line is selected, then presses Enter. It returns a line for the
// user, or "" when no screen showed.
func AnswerTrust(socket, pane, tool string, inTeam bool) string {
	choice := LoadPrompts()
	deadline := time.Now().Add(trustWatch)
	for time.Now().Before(deadline) {
		screen := paneScreen(socket, pane)
		for _, ts := range trustScreens {
			if ts.tool != tool || !strings.Contains(screen, ts.title) || !strings.Contains(screen, ts.answer) {
				continue
			}
			on := choice.CodexHooks == "on"
			if ts.setting == "claude" {
				on = choice.ClaudeFolders == "on" && inTeam
			}
			if !on {
				why := "answer it there, or let sunstack answer it from now on: sunstack prompts " + ts.flag + " on"
				if ts.setting == "claude" && !inTeam {
					why = "answer it there; sunstack answers it only in a team folder"
				}
				return "waiting at " + ts.mark + " in pane " + pane + ": " + why
			}
			if selectLine(socket, pane, ts) {
				return "answered " + ts.mark + ": " + ts.answer + " (sunstack prompts)"
			}
			return "waiting at " + ts.mark + " in pane " + pane + ": the screen did not match; answer it there"
		}
		if ReadyForInput(tool, screen) {
			return ""
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ""
}

// selectLine moves down to the answer line and presses Enter only once that
// line, and no other, carries the selection marker.
func selectLine(socket, pane string, ts trustScreen) bool {
	for i := 0; i < 5; i++ {
		if sel := selectedLine(paneScreen(socket, pane), ts.marker); strings.Contains(sel, ts.answer) {
			return exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "Enter")...).Run() == nil
		}
		if exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "Down")...).Run() != nil {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// selectedLine is the one screen line starting with the marker, or "" when
// none or several do.
func selectedLine(screen, marker string) string {
	found := ""
	for _, l := range strings.Split(screen, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), marker) {
			if found != "" {
				return ""
			}
			found = l
		}
	}
	return found
}
