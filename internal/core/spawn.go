package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SpawnOptions are the inputs of `sunstack spawn`.
type SpawnOptions struct {
	Arg         string // agent ID or title
	Tool        string // claude or codex; empty: the agent's tool:, else DefaultTool, else claude
	DefaultTool string // the caller's own CLI
	Task        string // session label
	Socket      string // tmux server of the caller, from $TMUX
	Server      string // that server's PID
	Note        string // what the new session should do first, in words
	Place       string // team (default), here or window (§20.1)
	Window      bool   // open a new tmux window instead of a pane beside the caller (--window: place window)
	Caller      string // the caller's pane, from $TMUX_PANE
	Brief       string // a task brief, put in the new session's inbox (checked by the caller)
	OverCap     bool   // start it even when the team is at max_sessions
	Via         string // the agent session that spawned it, for the brief
	FromSession string // that session's CLI session ID, so a reply can reach it
	// FromHost names the host that asked for this spawn (§20.4); the brief
	// then comes from <host>:user and its reply goes back there.
	FromHost string
	Request  string // that request's ID, marked on the pane
}

// SpawnResult is a started session. Running is false when the CLI was not
// seen running in the pane a few seconds after launch.
type SpawnResult struct {
	ID, Name, Pane, Token, Tool string
	Place                       string // team, here or window
	Home                        string // the team's home and window, for place team: ss-alpha-a1b2:pm.lead
	Running, Window             bool
}

// Spawn opens a pane beside the caller's (or, with Window or outside a known
// pane, a new tmux window) in the project root, claims the agent for the
// new session (marked spawned, so kill may close it), and starts
// Claude Code or Codex there with a first prompt that takes on the identity
// with that claim's token and checks the inbox.
func (p *Project) Spawn(o SpawnOptions) (*SpawnResult, error) {
	if o.Tool != "" && o.Tool != "claude" && o.Tool != "codex" {
		return nil, fail(ExitUsage, "usage", "--tool must be claude or codex")
	}
	if date, reason, ok := p.Halted(); ok {
		return nil, fail(ExitFail, "halted", "the team is halted since %s: %s; nothing new starts until the user runs sunstack halt --off", date, reason)
	}
	if o.Task != "" && !ValidTask(o.Task) {
		return nil, fail(ExitUsage, "usage", "invalid task %q: lowercase letters, digits and -, at most 10 characters", o.Task)
	}
	if strings.ContainsAny(o.Note, "'\n") {
		return nil, fail(ExitUsage, "usage", "keep single quotes and line breaks out of the note")
	}
	// One spawn at a time per team, from the cap check until the new claim
	// is written, so two spawns cannot both pass a cap of one.
	unlockSpawn, err := p.lock("_spawn_")
	if err != nil {
		return nil, err
	}
	defer func() { unlockSpawn() }()
	if !o.OverCap {
		if err := p.checkCap(); err != nil {
			return nil, err
		}
	}
	id, err := p.resolve(o.Arg)
	if err != nil {
		return nil, err
	}
	if err := p.checkIdentityFiles(id); err != nil {
		return nil, err
	}
	if o.Tool == "" {
		// The agent's own tool (§17.5), then the caller's CLI.
		agentTool, _ := frontmatterValue(filepath.Join(p.AgentDir(id), "AGENT.md"), "tool")
		switch {
		case agentTool == "claude" || agentTool == "codex":
			o.Tool = agentTool
		case agentTool != "":
			return nil, fail(ExitUsage, "usage", "%s/AGENT.md says tool: %s; it must be claude or codex", id, agentTool)
		case o.DefaultTool == "claude" || o.DefaultTool == "codex":
			o.Tool = o.DefaultTool
		default:
			o.Tool = "claude"
		}
	}
	place := o.Place
	switch {
	case o.Window:
		place = "window"
	case place == "":
		place = "team"
	case place != "team" && place != "here" && place != "window":
		return nil, fail(ExitUsage, "usage", "--place must be team, here or window")
	}
	if o.Socket == "" {
		// Outside tmux there is no pane to split or window to join.
		place = "team"
	}
	if place == "here" && o.Caller == "" {
		place = "window"
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fail(ExitFail, "no_tmux", "spawn opens a tmux pane, and tmux is not on PATH; start %s yourself and take on the agent there", o.Tool)
	}
	if _, err := exec.LookPath(o.Tool); err != nil {
		return nil, fail(ExitFail, "no_cli", "%s is not on PATH", o.Tool)
	}
	name := (&Live{Task: o.Task}).Label(id)
	if claims, _ := p.Claims(id); labelTaken(claims, id, name, nil) {
		return nil, taskTaken(id, name)
	}
	var pane, homeAt string
	switch place {
	case "team":
		h, err := p.home(id)
		if err != nil {
			return nil, err
		}
		pane, homeAt = h.pane, h.at
		o.Socket, o.Server = h.socket, h.server
	case "window":
		out, err := exec.Command("tmux", TmuxArgs(o.Socket, "new-window", "-d", "-P", "-F", "#{pane_id}", "-n", name, "-c", p.Root, "-e", "PATH="+os.Getenv("PATH"))...).Output()
		if pane = strings.TrimSpace(string(out)); err != nil || pane == "" {
			return nil, fail(ExitFail, "tmux", "could not open a tmux window: %v", err)
		}
	default:
		// Side by side when the caller's pane is wide enough, else stacked.
		split := "-v"
		if w, _ := exec.Command("tmux", TmuxArgs(o.Socket, "display-message", "-p", "-t", o.Caller, "#{pane_width}")...).Output(); atoiOr(strings.TrimSpace(string(w)), 0) >= 160 {
			split = "-h"
		}
		out, err := exec.Command("tmux", TmuxArgs(o.Socket, "split-window", "-d", split, "-t", o.Caller, "-P", "-F", "#{pane_id}", "-c", p.Root, "-e", "PATH="+os.Getenv("PATH"))...).Output()
		if pane = strings.TrimSpace(string(out)); err != nil || pane == "" {
			return nil, fail(ExitFail, "no_space", "could not split this pane (%v); it may be too small, so rerun with --place window", err)
		}
	}
	window := place == "window"
	closePane := func() { exec.Command("tmux", TmuxArgs(o.Socket, "kill-pane", "-t", pane)...).Run() }
	// Mark the pane, so kill can tell it from the user's own panes.
	exec.Command("tmux", TmuxArgs(o.Socket, "set-option", "-p", "-t", pane, "@sunstack", p.Root+"|"+id)...).Run()
	if o.Request != "" {
		exec.Command("tmux", TmuxArgs(o.Socket, "set-option", "-p", "-t", pane, "@sunstack_request", o.Request)...).Run()
	}

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
		TmuxPane: pane, TmuxSocket: o.Socket, TmuxServer: o.Server, Task: o.Task, Spawned: true}
	claims, _ := p.Claims(id)
	claims, _ = p.pruneStale(id, claims, "")
	if labelTaken(claims, id, name, nil) {
		unlock()
		closePane()
		return nil, taskTaken(id, name)
	}
	werr := p.writeLive(id, l)
	unlock()
	unlockSpawn()
	unlockSpawn = func() {}
	if werr != nil {
		closePane()
		return nil, fail(ExitFail, "fs", "%v", werr)
	}
	if o.Brief != "" {
		// Before the CLI starts, so its first inbox check finds it; no
		// nudge, since the first prompt checks the inbox. It keeps the
		// caller's session, so the reply has a way back.
		so := SendOptions{To: name, Body: o.Brief, Type: "task", NoNudge: true, Via: o.Via, FromSession: o.FromSession}
		if o.FromHost != "" {
			so.FromLabel, so.FromHost = o.FromHost+":user", o.FromHost
		}
		if _, err := p.Send(so); err != nil {
			p.removeLive(l)
			closePane()
			return nil, fail(ExitFail, "brief", "could not deliver the brief to %s: %v", name, err)
		}
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
	task := ""
	if o.Task != "" {
		task = " --task " + o.Task
	}
	first := fmt.Sprintf("%s %s --token %s%s (you were started by sunstack spawn; this claim is yours), %s", skill, id, l.Token, task, note)
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
	running := false
	for i := 0; i < 20 && !running; i++ {
		time.Sleep(250 * time.Millisecond)
		running = paneRunsTool(o.Socket, pane, o.Tool)
	}
	return &SpawnResult{ID: id, Name: name, Pane: pane, Token: l.Token, Tool: o.Tool, Place: place, Home: homeAt, Running: running, Window: window}, nil
}

func atoiOr(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// labelTaken reports whether a claim other than skip already uses the label.
func labelTaken(claims []*Live, id, label string, skip *Live) bool {
	for _, c := range claims {
		if c != skip && c.Label(id) == label {
			return true
		}
	}
	return false
}

func taskTaken(id, label string) error {
	return fail(ExitClaim, "task_taken", "another session of %s is already named %s; pick a different --task", id, label)
}

// Peek returns the last n lines of a session's tmux pane: a session name
// (<id> or <id>_<task>) in this project, or a pane ID like %12 on the
// caller's or the default tmux server. Read only.
func (p *Project) Peek(t, socket string, n int) (string, error) {
	pane := t
	if !strings.HasPrefix(t, "%") {
		if p == nil {
			return "", fail(ExitFail, "no_root", "name a pane (%%N), or run inside a Sunstack project to name a session")
		}
		id, c, err := p.target(t)
		if err != nil {
			return "", err
		}
		if c.TmuxPane == "" || !sameServer(c) {
			return "", fail(ExitFail, "no_pane", "%s is not in a tmux pane sunstack knows", c.Label(id))
		}
		pane, socket = c.TmuxPane, c.TmuxSocket
	}
	screen := paneScreen(socket, pane)
	if screen == "" {
		return "", fail(ExitFail, "no_pane", "could not read tmux pane %s", pane)
	}
	return maskTokens(strings.Join(screenTail(screen, n), "\n")) + "\n", nil
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
		return fmt.Sprintf("asked %s to finish (message %s); it saves and releases itself, and sunstack sessions no longer lists it once done", name, res.ID), nil
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
	if os.Getenv("TMUX_PANE") == c.TmuxPane && sameServer(c) {
		return "", fail(ExitUsage, "self", "%s is this pane; kill closes another session, so ask the user to close this one", name)
	}
	if !sameServer(c) {
		return "", fail(ExitFail, "not_running", "tmux has restarted since %s was claimed, so pane %s is not that session; run sunstack release %s --stale", name, c.TmuxPane, name)
	}
	mark, _ := exec.Command("tmux", TmuxArgs(c.TmuxSocket, "show-options", "-p", "-v", "-t", c.TmuxPane, "@sunstack")...).Output()
	ours := c.Spawned && strings.TrimSpace(string(mark)) == p.Root+"|"+id
	if !ours && !paneRunsTool(c.TmuxSocket, c.TmuxPane, c.Tool) {
		return "", fail(ExitFail, "not_running", "pane %s no longer runs %s for %s, so it is left alone; if that session is gone, close the pane, then sunstack release %s --stale", c.TmuxPane, c.Tool, name, name)
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

// checkCap refuses a spawn when the team already has max_sessions live
// sessions (§16.7), listing them so the user can choose one to finish.
func (p *Project) checkCap() error {
	tf, _ := p.Team()
	if tf.MaxSessions <= 0 {
		tf.MaxSessions = DefaultMaxSessions
	}
	var live []string
	for _, id := range p.Agents() {
		// Read only: a claim whose pane is gone is not counted, and is left
		// for release --stale, which takes the agent's lock.
		claims, _ := p.Claims(id)
		for _, c := range claims {
			if paneGone(c) {
				continue
			}
			line := c.Label(id)
			if c.Doing != "" {
				line += " (doing: " + c.Doing + ")"
			}
			live = append(live, line)
		}
	}
	// Free sessions count too (§20.2).
	_, owner := p.homeOf()
	live = append(live, freeCount(owner)...)
	if len(live) < tf.MaxSessions {
		return nil
	}
	return fail(ExitClaim, "team_full", "the team has %d live session(s), at its max_sessions: %d (sunstack/TEAM): %s; finish one first (sunstack dismiss), or pass --over-cap after the user agrees", len(live), tf.MaxSessions, strings.Join(live, ", "))
}

// frontmatterValue reads one key of a file's leading --- block.
func frontmatterValue(path, key string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", false
	}
	for _, l := range strings.Split(s[4:4+end], "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}
