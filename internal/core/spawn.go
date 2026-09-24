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

// target resolves an ID or a session name to exactly one live session.
func (p *Project) target(t string) (string, *Live, error) {
	id, session, err := p.resolveRecipient(t)
	if err != nil {
		return "", nil, err
	}
	claims, err := p.Claims(id)
	if err != nil {
		return "", nil, err
	}
	var c *Live
	for _, x := range claims {
		if session == "" || x.Label(id) == session {
			if c != nil {
				e := fail(ExitUsage, "missing_arguments", "%s has several sessions; name one", id)
				e.Stdout = "missing_arguments: session\n"
				for _, y := range claims {
					e.Stdout += y.Label(id) + "\n"
				}
				return "", nil, e
			}
			c = x
		}
	}
	if c == nil {
		return "", nil, fail(ExitFail, "not_found", "%s has no live session", t)
	}
	return id, c, nil
}

// Dismiss asks a session to finish: a shutdown message to it, with a nudge.
func (p *Project) Dismiss(t string) (string, error) {
	id, c, err := p.target(t)
	if err != nil {
		return "", err
	}
	name := c.Label(id)
	to := id
	if c.Task != "" {
		to = name
	} else if claims, _ := p.Claims(id); len(claims) > 1 {
		return "", fail(ExitUsage, "usage", "that session has no task label, so a message cannot reach it alone; ask it in its pane, or use sunstack kill")
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

// KillPlan describes what kill would close, for the confirmation.
func (p *Project) KillPlan(t string) (string, error) {
	id, c, err := p.target(t)
	if err != nil {
		return "", err
	}
	if c.TmuxPane == "" || c.TmuxSocket == "" {
		return "", fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows; close it yourself", c.Label(id))
	}
	return fmt.Sprintf("close tmux pane %s (%s) running %s as %s, drop its claim, and put its taken messages back", c.TmuxPane, TmuxWhere(c.TmuxSocket, c.TmuxPane), c.Tool, c.Label(id)), nil
}

// Kill closes one session's tmux pane at once, whoever started it, then
// drops its claim and requeues its taken messages. Unsaved work in that
// session is lost. It refuses a pane that no longer runs the recorded CLI,
// unless sunstack spawned it and the pane still carries its mark.
func (p *Project) Kill(t string) (string, error) {
	id, c, err := p.target(t)
	if err != nil {
		return "", err
	}
	name := c.Label(id)
	if c.TmuxPane == "" || c.TmuxSocket == "" {
		return "", fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows; close it yourself", name)
	}
	mark, _ := exec.Command("tmux", TmuxArgs(c.TmuxSocket, "show-options", "-p", "-v", "-t", c.TmuxPane, "@sunstack")...).Output()
	ours := c.Spawned && strings.TrimSpace(string(mark)) == p.Root+"|"+id
	if !ours && !paneRunsTool(c.TmuxSocket, c.TmuxPane, c.Tool) {
		return "", fail(ExitFail, "not_running", "pane %s no longer runs %s for %s, so it is left alone; if that session is gone, delete %s to drop its claim", c.TmuxPane, c.Tool, name, c.path)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := exec.Command("tmux", TmuxArgs(c.TmuxSocket, "kill-pane", "-t", c.TmuxPane)...).Run(); err != nil {
		return "", fail(ExitFail, "tmux", "could not close pane %s: %v", c.TmuxPane, err)
	}
	p.removeLive(c)
	os.RemoveAll(p.sessionTmp(id, c.Token))
	rest, _ := p.Claims(id)
	p.requeue(id, rest)
	p.LogEvent("kill", name, "pane="+c.TmuxPane)
	return "closed " + name + " (pane " + c.TmuxPane + ")", nil
}
