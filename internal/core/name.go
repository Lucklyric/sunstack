package core

import (
	"path/filepath"
	"strings"
)

// lockNames is the host lock under which a session name is checked and
// given, by sunstack name and by spawn --free, so two never get the same.
func lockNames() (func(), error) {
	return LockDir(filepath.Join(Home(), "locks", "names"), "session names")
}

// panesByLabel are the panes whose @sunstack_label is label, on the default
// tmux server and the caller's.
func panesByLabel(label string) []paneInfo {
	var out []paneInfo
	for _, p := range tmuxPanes() {
		if p.label == label {
			out = append(out, p)
		}
	}
	return out
}

// Naming a session (§46). Every session in a tmux pane can carry a Sunstack
// name: an agent session's is <id>_<task>, any other session's is the pane's
// @sunstack_label. Only a pane sunstack opened also carries
// @sunstack_launch, so a name alone never makes a session one that clean or
// kill may close.

// NameOptions are the inputs of sunstack name.
type NameOptions struct {
	Target string // another session (ID, label or name); empty: the caller's
	Name   string // the new name: a task label for an agent session, else a label
	Title  string // a free session's own title; empty: <project>/<label> (§48)
	// The caller's pane and CLI session.
	Pane, Socket, Session string
}

// NameResult is a session's changed name.
type NameResult struct {
	Old, Name, Pane string
	Title           string // the title it now shows (§48)
	Agent           string // the agent, when the session works as one
}

// NameSession gives a session a new Sunstack name and puts it on its pane's
// title. p is the team the caller is in, if any.
func NameSession(p *Project, o NameOptions) (*NameResult, error) {
	pane, socket := o.Pane, o.Socket
	if o.Target != "" {
		at, err := FindAttach(p, o.Target)
		if err != nil {
			return nil, err
		}
		pane, socket = at.Pane, at.Socket
	}
	if pane == "" || socket == "" {
		return nil, fail(ExitFail, "no_pane", "this session is not in a tmux pane, so sunstack has nowhere to keep its name")
	}
	own := o.Target == "" || (pane == o.Pane && socket == o.Socket)
	// An agent session: its name is the claim's label.
	if p != nil {
		type held struct {
			id string
			c  *Live
		}
		var mine []held
		for _, id := range p.Agents() {
			claims, _ := p.Claims(id)
			for _, c := range claims {
				if (own && o.Session != "" && c.Session == o.Session) || samePane(c, pane, socket) {
					mine = append(mine, held{id, c})
				}
			}
		}
		switch {
		case len(mine) > 0 && !own:
			return nil, fail(ExitUsage, "agent_session", "%s works as %s; a session changes its own task label: it runs sunstack name <label>", o.Target, mine[0].id)
		case len(mine) > 1:
			return nil, fail(ExitUsage, "ambiguous", "this session works as %d agents; change one with sunstack as <id> --token T --task <label>", len(mine))
		case len(mine) == 1 && o.Title != "":
			return nil, fail(ExitUsage, "usage", "an agent session is titled <project>/<id>:<task>; --title is for a session that works as no agent")
		case len(mine) == 1:
			return p.retask(mine[0].id, mine[0].c.Token, o.Name)
		}
	}
	if !labelRe.MatchString(o.Name) {
		return nil, fail(ExitUsage, "usage", "invalid name %q: lowercase letters, digits and -, 1 to 20 characters", o.Name)
	}
	if o.Title != "" && !ValidTitle(o.Title) {
		return nil, fail(ExitUsage, "usage", "invalid --title %q: one line, at most 80 characters, no single quote", o.Title)
	}
	unlock, err := lockNames()
	if err != nil {
		return nil, err
	}
	defer unlock()
	out, err := quickTmux(socket, "list-panes", "-a", "-F", "#{pane_id}\t#{@sunstack}\t#{@sunstack_scope}\t#{@sunstack_label}\t#{pane_current_path}")
	if err != nil {
		return nil, fail(ExitFail, "tmux", "could not read the tmux panes: %v", err)
	}
	var scope, old, dir string
	found := false
	rows := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for _, l := range rows {
		if f := strings.Split(l, "\t"); len(f) == 5 && f[0] == pane {
			if f[1] != "" {
				// The pane is marked for an agent whose claim is not in this
				// team (or is gone): its name is not ours to change here.
				return nil, fail(ExitUsage, "agent_session", "pane %s belongs to an agent session (%s); run sunstack name in its team", pane, f[1])
			}
			scope, old, dir, found = f[2], f[3], f[4], true
		}
	}
	if !found {
		return nil, fail(ExitFail, "no_pane", "pane %s is gone", pane)
	}
	// Names are unique where one can be mistaken for another: among the
	// sessions of one scope, and against every session that has no scope.
	for _, l := range rows {
		if f := strings.Split(l, "\t"); len(f) == 5 && f[0] != pane && f[3] == o.Name && (f[2] == scope || f[2] == "" || scope == "") {
			return nil, fail(ExitClaim, "name_taken", "the session in pane %s is already called %s; pick another name", f[0], o.Name)
		}
	}
	if _, err := quickTmux(socket, "set-option", "-p", "-t", pane, "@sunstack_label", o.Name); err != nil {
		return nil, fail(ExitFail, "tmux", "could not name pane %s: %v", pane, err)
	}
	// The title says the project (§48), unless the user gave one.
	title := o.Title
	if title == "" {
		title = Title(nameSlug(p, scope, dir), o.Name, "")
	}
	quickTmux(socket, "set-option", "-p", "-t", pane, "@sunstack_title", title)
	quickTmux(socket, "select-pane", "-t", pane, "-T", title)
	return &NameResult{Old: old, Name: o.Name, Pane: pane, Title: title}, nil
}

// nameSlug is the project part of a named session's title: the caller's
// team, else the free session's group, else the pane's folder.
func nameSlug(p *Project, scope, dir string) string {
	switch {
	case p != nil:
		return p.Slug()
	case strings.HasPrefix(scope, "group:"):
		return Slug(strings.TrimSuffix(filepath.Base(strings.TrimPrefix(scope, "group:")), ".git"))
	}
	return Slug(filepath.Base(dir))
}

// retask changes the task label of one claim on id, and so the session's
// name, keeping everything else the claim holds.
func (p *Project) retask(id, token, task string) (*NameResult, error) {
	if !ValidTask(task) {
		return nil, fail(ExitUsage, "usage", "invalid task label %q: an agent session is named %s_<label>, lowercase letters, digits and -, 1 to 20 characters", task, id)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	claims, _ := p.Claims(id)
	mine := findToken(claims, token)
	if mine == nil {
		return nil, fail(ExitClaim, "token", "the claim on %s is gone (taken over or released?)", id)
	}
	old := mine.Label(id)
	res := &NameResult{Old: old, Name: id + "_" + task, Pane: mine.TmuxPane, Agent: id, Title: Title(p.Slug(), id, task)}
	if mine.Task == task {
		return res, nil
	}
	if labelTaken(claims, id, res.Name, mine) {
		return nil, taskTaken(id, res.Name)
	}
	mine.Task = task
	if err := p.writeLive(id, mine); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	if mine.TmuxPane != "" && mine.TmuxSocket != "" {
		quickTmux(mine.TmuxSocket, "select-pane", "-t", mine.TmuxPane, "-T", res.Title)
	}
	p.LogEvent("name", id, "from="+old, "to="+res.Name)
	return res, nil
}
