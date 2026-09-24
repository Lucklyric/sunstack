package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SpawnOptions are the inputs of `sunstack spawn`.
type SpawnOptions struct {
	Arg    string // agent ID or title
	Tool   string // claude or codex
	Task   string // session label
	Socket string // tmux server of the caller, from $TMUX
	Note   string // what the new session should do first, in words
}

// SpawnResult is a started session.
type SpawnResult struct{ ID, Name, Pane, Token string }

// Spawn opens a tmux window in the project root, claims the agent for the
// new session (marked spawned, so dismiss --force may close it), and starts
// Claude Code or Codex there with a first prompt that takes on the identity
// with that claim's token and checks the inbox.
func (p *Project) Spawn(o SpawnOptions) (*SpawnResult, error) {
	if o.Tool != "claude" && o.Tool != "codex" {
		return nil, fail(ExitUsage, "usage", "--tool must be claude or codex")
	}
	if o.Task != "" && !ValidTask(o.Task) {
		return nil, fail(ExitUsage, "usage", "invalid task %q: lowercase letters, digits and -, at most 10 characters", o.Task)
	}
	if strings.ContainsAny(o.Note, "'\n") {
		return nil, fail(ExitUsage, "usage", "keep single quotes and line breaks out of the note")
	}
	if o.Socket == "" {
		return nil, fail(ExitFail, "no_tmux", "spawn opens a tmux window, so run it inside tmux; otherwise start %s yourself and take on the agent there", o.Tool)
	}
	if _, err := exec.LookPath(o.Tool); err != nil {
		return nil, fail(ExitFail, "no_cli", "%s is not on PATH", o.Tool)
	}
	id, err := p.resolve(o.Arg)
	if err != nil {
		return nil, err
	}
	if err := p.checkIdentityFiles(id); err != nil {
		return nil, err
	}
	name := (&Live{Task: o.Task}).Label(id)
	out, err := exec.Command("tmux", TmuxArgs(o.Socket, "new-window", "-d", "-P", "-F", "#{pane_id}", "-n", name, "-c", p.Root, "-e", "PATH="+os.Getenv("PATH"))...).Output()
	pane := strings.TrimSpace(string(out))
	if err != nil || pane == "" {
		return nil, fail(ExitFail, "tmux", "could not open a tmux window: %v", err)
	}
	closePane := func() { exec.Command("tmux", TmuxArgs(o.Socket, "kill-pane", "-t", pane)...).Run() }
	// Mark the pane, so dismiss --force can tell it from the user's own panes.
	exec.Command("tmux", TmuxArgs(o.Socket, "set-option", "-p", "-t", pane, "@sunstack", p.Root+"|"+id)...).Run()

	unlock, err := p.lock(id)
	if err != nil {
		closePane()
		return nil, err
	}
	host, _ := os.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	l := &Live{Tool: o.Tool, Host: host, Token: NewToken(), Claimed: now(), LastContact: now(),
		TmuxPane: pane, TmuxSocket: o.Socket, Task: o.Task, Spawned: true}
	werr := p.writeLive(id, l)
	unlock()
	if werr != nil {
		closePane()
		return nil, fail(ExitFail, "fs", "%v", werr)
	}
	exec.Command("tmux", TmuxArgs(o.Socket, "select-pane", "-t", pane, "-T", name)...).Run()

	note := o.Note
	if note == "" {
		note = "then check the inbox and handle what is there"
	}
	skill := "/sunstack:as"
	if o.Tool == "codex" {
		skill = "$sunstack:as"
	}
	first := fmt.Sprintf("%s %s --token %s --task %s (you were started by sunstack spawn; this claim is yours), %s", skill, id, l.Token, orDash(o.Task), note)
	// Single quotes: the shell must not expand $sunstack in the Codex prompt.
	cmd := fmt.Sprintf("%s '%s'", o.Tool, first)
	if err := exec.Command("tmux", TmuxArgs(o.Socket, "send-keys", "-t", pane, "-l", cmd)...).Run(); err == nil {
		err = exec.Command("tmux", TmuxArgs(o.Socket, "send-keys", "-t", pane, "Enter")...).Run()
	}
	if err != nil {
		p.removeLive(l)
		closePane()
		return nil, fail(ExitFail, "tmux", "could not start %s: %v", o.Tool, err)
	}
	p.LogEvent("spawn", name, "tool="+o.Tool, "pane="+pane)
	return &SpawnResult{ID: id, Name: name, Pane: pane, Token: l.Token}, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Dismiss asks a session to finish (a shutdown message to it, with a nudge).
// With force it closes the pane instead, only for a session sunstack spawned
// in a pane still marked as its own; the claim is dropped and its taken
// messages go back to the inbox.
func (p *Project) Dismiss(target string, force bool) (string, error) {
	id, session, err := p.resolveRecipient(target)
	if err != nil {
		return "", err
	}
	claims, err := p.Claims(id)
	if err != nil {
		return "", err
	}
	var c *Live
	for _, x := range claims {
		if session == "" || x.Label(id) == session {
			if c != nil {
				return "", fail(ExitUsage, "missing_arguments", "%s has several sessions; name one (<id>_<task>, see sunstack sessions)", id)
			}
			c = x
		}
	}
	if c == nil {
		return "", fail(ExitFail, "not_found", "%s has no live session", target)
	}
	name := c.Label(id)
	if !force {
		if c.Task == "" && len(claims) > 1 {
			return "", fail(ExitUsage, "usage", "that session has no task label, so it cannot be addressed alone; use --force for a spawned one, or ask it in its pane")
		}
		to := id
		if c.Task != "" {
			to = name
		}
		res, err := p.Send(SendOptions{To: to, Type: "shutdown", Body: "Please finish: save, reply done, release, then stop."})
		if err != nil {
			return "", err
		}
		p.LogEvent("dismiss", name, res.ID)
		if res.Nudged != "" {
			return fmt.Sprintf("asked %s to finish (message %s); it replies done when it has", name, res.ID), nil
		}
		return fmt.Sprintf("left a shutdown message %s for %s; %s", res.ID, name, res.Note), nil
	}
	if !c.Spawned {
		return "", fail(ExitFail, "not_spawned", "%s was not started by sunstack spawn; close it yourself", name)
	}
	mark, err := exec.Command("tmux", TmuxArgs(c.TmuxSocket, "show-options", "-p", "-v", "-t", c.TmuxPane, "@sunstack")...).Output()
	if err != nil || strings.TrimSpace(string(mark)) != p.Root+"|"+id {
		return "", fail(ExitFail, "not_spawned", "pane %s is no longer the one sunstack opened for %s; close it yourself", c.TmuxPane, name)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	exec.Command("tmux", TmuxArgs(c.TmuxSocket, "kill-pane", "-t", c.TmuxPane)...).Run()
	p.removeLive(c)
	os.RemoveAll(p.sessionTmp(id, c.Token))
	rest, _ := p.Claims(id)
	p.requeue(id, rest)
	p.LogEvent("dismiss", name, "force")
	return "closed " + name + " (pane " + c.TmuxPane + ")", nil
}
