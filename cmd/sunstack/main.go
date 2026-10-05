// Command sunstack does Sunstack's deterministic work: claims, snapshots and
// commits, and installing the plugin into Claude Code and Codex (design §6, §10).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/setup"
	"github.com/Lucklyric/sunstack/internal/tui"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `sunstack — agent-team layer for Claude Code and Codex

Agent identity (used by the plugin's skills):
  sunstack as <id|title> [--task LABEL] [--join] [--token T] [--takeover --expect CLAIM] [--tool claude|codex] [--root DIR]
                                                  several sessions may work as one agent: --join adds this one;
                                                  --task (up to 10 characters) names it <id>_<task>
  sunstack snapshot <id> <context.md|board.md|threads/<topic>.md|archive/<YYYY-MM>.md> --token T [--root DIR]
  sunstack snapshot <id> <pillars.md|AGENT.md> [--root DIR]      rule files, no token needed
  sunstack snapshot --team [BOARD.md] [--root DIR]                team PILLARS.md (default) or BOARD.md
  sunstack commit <id> <target> <candidate> <checksum> --token T [--root DIR]
  sunstack commit <id> <target> --delete <checksum> --token T [--root DIR]
  sunstack release <id> --token T [--root DIR]
  sunstack release <id|id_task> --stale           drop sessions whose tmux pane is confirmed closed (no token needed)

Self-improvement and objectives (the user approves every call; both CLIs prompt for it):
  sunstack amend <id> <pillars.md|AGENT.md> <candidate> <checksum> --summary "..." [--root DIR]
  sunstack amend --team [BOARD.md] <candidate> <checksum> --summary "..." [--root DIR]

Boards:
  sunstack board [id]                             objectives, key results by objective, directives, and what
                                                  needs attention (stale, overdue, blocked, not aligned)
  sunstack direct "<text>" [--to ID,TITLE,...]    add a dated directive from the user to BOARD.md (default: all)
  sunstack answer <id> Q<n> "<answer>"            answer an agent's ask (its board's Asks); the agent records it
  sunstack tidy <id> | --team | --all             archive old finished entries by month; creates a missing board

Sessions and messages:
  sunstack sessions [--json]                      every live session: name, tool, host, tmux place, resume command
  sunstack send <id|title|id_task|team/id|pane-id|session-id> "<text>"|--file F [--type task|question|handoff|fyi|done|shutdown]
                [--reply-to MSG] [--from ID --token T] [--no-nudge] [--op ID]
                                                  a task carries a brief: Goal, Scope, Done when, Verify, Report
                                                  (Context, Timebox, Not optional); a brief missing one is refused
                                                  write a message to the agent's inbox, then type a one-line nudge
                                                  into a live Claude Code or Codex pane of that agent (tmux)
  sunstack check <id> --token T                   messages this session may handle (pending, and taken by it)
  sunstack take <id> <msg> --token T              take a message so no other session works on it
  sunstack ack <id> <msg> --token T               archive a handled message
  sunstack check|take|ack --session [<msg>]       the same for a session that works as no agent (its host inbox)
  sunstack whoami [--root DIR]
  sunstack spawn <id|title> [--tool claude|codex] [--task LABEL] [--note "..."] [--brief FILE] [--window] [--over-cap]
                                                  split this pane (--window: a new window) and start a session as that agent;
                                                  --brief puts a task in its inbox; refused at max_sessions (sunstack/TEAM, default 6)
  sunstack dismiss <id|id_task>                   ask a session to finish (a shutdown message)
  sunstack kill <id_task|id> [--yes]              close that one session's tmux pane now (asks first; unsaved work is lost)
  sunstack hr                                     staffing facts: load, idle agents, uncovered objectives, gaps,
                                                  unused templates, what the project is made of

Team (run these yourself):
  sunstack init [--refresh]                       create sunstack/ here, the AGENTS.md block and .gitignore line;
                                                  --refresh also updates PROTOCOL.md and README.md to this version
  sunstack hire <title> <name> [--file DRAFT | --from ID]
                                                  add agent <title>.<name> from a template, an approved draft,
                                                  or a colleague's role (used when no template exists)
  sunstack rename <id> <title>.<name>             rename an agent nobody holds, keeping its context
  sunstack fire <id> [--discard]                  delete an agent nobody holds
  sunstack library                                list templates (personal, then built-in)
  sunstack library show <title>                   print a template's AGENT.md
  sunstack library save <id> [--as TITLE] [--force]  save an agent's AGENT.md as a personal template
  sunstack team                                   who is on the team, who holds whom, where
  sunstack teams [--scan DIR] [--prune] [--json]  teams indexed on this host (~/.sunstack/teams.json)
  sunstack migrate [--host | --scan DIR] [--apply safe|all] [--json]
                                                  what this team (or every indexed team) needs from an older
                                                  version; safe steps run on update and as, the rest on --apply all

Org (every team and Claude Code or Codex session on this host):
  sunstack org [--by team|agent|host] [--attention] [--json]
                                                  what needs you, then work by team; --by agent: people,
                                                  --by host: every session including free ones
  sunstack peek <id_task|pane-id> [--lines N]     the end of a session's tmux pane, e.g. %12 (read only)
  sunstack doing <id> "<line>" --token T          one line on what this session is doing now ("" clears)
  sunstack reopen <session-id> [--window]         resume a closed session in a tmux pane beside this one, so
                                                  messages wake it at once (exit it where it ran first)
  sunstack log [--id ID] [--follow]               the event log
  sunstack inbox <id>                             pending messages, read only
  sunstack pillar <id> | --team                   effective pillars with their source
  sunstack health                                 read-only checks of the project and the install
  sunstack tui                                    team dashboard (also: sunstack with no arguments)

Setup:
  sunstack install [--claude] [--codex] [--yes]   install the plugin (both CLIs found on PATH by default)
  sunstack update [--claude] [--codex]            update this binary and the plugin
  sunstack uninstall [--claude] [--codex] [--yes] remove the plugin and the permission rule
  sunstack version                                print CLI and protocol versions

Exit codes: 0 ok, 1 error, 2 usage or missing_arguments, 3 snapshot mismatch, 4 claim problem.
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// args splits positional arguments from --flags. valued lists flags that take
// a value; any other known flag is a switch.
type args struct {
	pos   []string
	flags map[string]string
}

func parse(in []string, valued, switches string) (*args, error) {
	a := &args{flags: map[string]string{}}
	isIn := func(list, name string) bool {
		for _, x := range strings.Fields(list) {
			if x == name {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(in); i++ {
		s := in[i]
		if !strings.HasPrefix(s, "--") {
			a.pos = append(a.pos, s)
			continue
		}
		name := strings.TrimPrefix(s, "--")
		switch {
		case isIn(valued, name):
			if i+1 >= len(in) {
				return nil, &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: s + " needs a value"}
			}
			a.flags[name] = in[i+1]
			i++
		case isIn(switches, name):
			a.flags[name] = "1"
		default:
			return nil, &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "unknown option: " + s}
		}
	}
	return a, nil
}

func (a *args) has(name string) bool { _, ok := a.flags[name]; return ok }

// atMost rejects surplus positional arguments, so a mistyped command never
// silently does less than asked.
func (a *args) atMost(n int, cmd string) error {
	if len(a.pos) > n {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: fmt.Sprintf("too many arguments for %s: %s (see sunstack help)", cmd, strings.Join(a.pos[n:], " "))}
	}
	return nil
}

var askKeyRe = regexp.MustCompile(`^Q[0-9]+$`)

// readBody reads a message body or brief from a file.
func readBody(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: fmt.Sprintf("cannot read %s: %v", path, err)}
	}
	return string(b), nil
}

// tmuxSocket is the server socket from $TMUX ("socket,pid,session").
func tmuxSocket() string {
	if t := os.Getenv("TMUX"); t != "" {
		return strings.SplitN(t, ",", 2)[0]
	}
	return ""
}

// tmuxServer is the tmux server's process ID, the second field of $TMUX. It
// tells a restarted server (whose pane IDs start over) from the one a claim
// was made on.
func tmuxServer() string {
	if f := strings.Split(os.Getenv("TMUX"), ","); len(f) >= 2 {
		return f[1]
	}
	return ""
}

func missing(what string) error {
	return &core.Error{Code: core.ExitUsage, Reason: "missing_arguments", Msg: "missing " + what, Stdout: "missing_arguments: " + what + "\n"}
}

// teamFile picks the team target: PILLARS.md by default, or BOARD.md.
func teamFile(pos []string) string {
	if len(pos) > 0 {
		return pos[0]
	}
	return "PILLARS.md"
}

// detectTool names the calling agent CLI from its environment.
func detectTool() (tool, session string) {
	if s := os.Getenv("CLAUDE_CODE_SESSION_ID"); s != "" || os.Getenv("CLAUDECODE") != "" {
		return "claude", s
	}
	// Codex sets CODEX_THREAD_ID for every command; older builds may have
	// used CODEX_SESSION_ID.
	for _, k := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"} {
		if s := os.Getenv(k); s != "" {
			return "codex", s
		}
	}
	return "", ""
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		// A bare `sunstack` in a terminal inside a project opens the dashboard.
		if p, err := core.FindProject(""); err == nil && setup.IsTerminal(os.Stdout) {
			if err := tui.Run(p); err != nil {
				fmt.Fprintf(stderr, "sunstack: error: %v\n", err)
				return core.ExitFail
			}
			return 0
		}
	}
	if len(argv) == 0 || argv[0] == "help" || argv[0] == "--help" || argv[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	err := dispatch(argv[0], argv[1:], stdin, stdout, stderr)
	if err == nil {
		return 0
	}
	var ce *core.Error
	if errors.As(err, &ce) {
		fmt.Fprint(stdout, ce.Stdout)
		fmt.Fprintf(stderr, "sunstack: %s: %s\n", ce.Reason, ce.Msg)
		return ce.Code
	}
	fmt.Fprintf(stderr, "sunstack: error: %v\n", err)
	return core.ExitFail
}

func dispatch(cmd string, rest []string, stdin io.Reader, stdout, stderr io.Writer) error {
	switch cmd {
	case "version", "--version":
		fmt.Fprintf(stdout, "sunstack %s\nprotocol: %d\n", version, core.ProtocolVersion)
		return nil

	case "as":
		a, err := parse(rest, "root token expect tool task", "takeover join")
		if err == nil {
			err = a.atMost(1, "as")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		tool, session := detectTool()
		if a.has("tool") {
			tool = a.flags["tool"]
		}
		arg := ""
		if len(a.pos) > 0 {
			arg = a.pos[0]
		}
		r, err := p.As(core.AsOptions{
			Arg: arg, Tool: tool, Token: a.flags["token"], Takeover: a.has("takeover"),
			Expect: a.flags["expect"], Pane: os.Getenv("TMUX_PANE"), Socket: tmuxSocket(), Server: tmuxServer(), Session: session,
			Join: a.has("join"), Task: a.flags["task"],
		})
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, p.Bundle(r))
		fmt.Fprint(stdout, migrateNote(p))
		return nil

	case "snapshot":
		a, err := parse(rest, "root token", "team")
		if err != nil {
			return err
		}
		if a.has("team") {
			if err := a.atMost(1, "snapshot --team"); err != nil {
				return err
			}
			a.pos = []string{core.TeamID, teamFile(a.pos)}
		}
		if err := a.atMost(2, "snapshot"); err != nil {
			return err
		}
		if len(a.pos) < 2 {
			return missing("id target")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		out, err := p.Snapshot(a.pos[0], a.pos[1], a.flags["token"])
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, out)
		return nil

	case "commit":
		a, err := parse(rest, "root token", "delete")
		if err != nil {
			return err
		}
		o := core.CommitOptions{Token: a.flags["token"], Delete: a.has("delete")}
		need := 4
		if o.Delete {
			need = 3
		}
		if err := a.atMost(need, "commit"); err != nil {
			return err
		}
		if len(a.pos) < need {
			return missing("id target candidate checksum")
		}
		o.ID, o.Rel = a.pos[0], a.pos[1]
		if o.Delete {
			o.Sum = a.pos[2]
		} else {
			o.Candidate, o.Sum = a.pos[2], a.pos[3]
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		if err := p.Commit(o); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: committed %s/%s\n", o.ID, o.Rel)
		return nil

	case "amend":
		a, err := parse(rest, "root summary", "team")
		if err != nil {
			return err
		}
		if a.has("team") {
			// amend --team [BOARD.md|PILLARS.md] <candidate> <checksum>
			file := "PILLARS.md"
			if len(a.pos) == 3 {
				file, a.pos = a.pos[0], a.pos[1:]
			}
			a.pos = append([]string{core.TeamID, file}, a.pos...)
		}
		if err := a.atMost(4, "amend"); err != nil {
			return err
		}
		if len(a.pos) < 4 {
			return missing("target candidate checksum")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		o := core.AmendOptions{ID: a.pos[0], Rel: a.pos[1], Candidate: a.pos[2], Sum: a.pos[3], Summary: a.flags["summary"]}
		if err := p.Amend(o); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: amended %s\n", strings.TrimPrefix(o.ID+"/"+o.Rel, core.TeamID+"/"))
		return nil

	case "release":
		a, err := parse(rest, "root token", "stale")
		if err == nil {
			err = a.atMost(1, "release")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("id")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		if a.has("stale") {
			dropped, err := p.ReleaseStale(a.pos[0])
			if err != nil {
				return err
			}
			if len(dropped) == 0 {
				fmt.Fprintln(stdout, "sunstack: no session with a confirmed closed pane")
			}
			for _, d := range dropped {
				fmt.Fprintf(stdout, "sunstack: released %s (its pane is closed)\n", d)
			}
			return nil
		}
		if err := p.Release(a.pos[0], a.flags["token"]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: released %s\n", a.pos[0])
		return nil

	case "init":
		a, err := parse(rest, "", "refresh")
		if err == nil {
			err = a.atMost(1, "init")
		}
		if err != nil {
			return err
		}
		dir := "."
		if len(a.pos) > 0 {
			dir = a.pos[0]
		}
		done, err := core.Init(dir, a.has("refresh"))
		if err != nil {
			return err
		}
		if p, err := core.FindProject(dir); err == nil {
			_ = p.Register()
		}
		if len(done) == 0 {
			done = []string{"already initialized; nothing to change"}
		}
		for _, d := range done {
			fmt.Fprintln(stdout, d)
		}
		return nil

	case "hire":
		a, err := parse(rest, "root file from", "")
		if err == nil {
			err = a.atMost(2, "hire")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		o := core.HireOptions{File: a.flags["file"], From: a.flags["from"]}
		if len(a.pos) > 0 {
			o.Title = a.pos[0]
		}
		if len(a.pos) > 1 {
			o.Name = a.pos[1]
		}
		id, err := p.Hire(o)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: hired %s (sunstack/%s/)\n", id, id)
		return nil

	case "fire":
		a, err := parse(rest, "root", "discard")
		if err == nil {
			err = a.atMost(1, "fire")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("id")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		if err := p.Fire(a.pos[0], a.has("discard")); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: fired %s\n", a.pos[0])
		return nil

	case "rename":
		a, err := parse(rest, "root", "")
		if err == nil {
			err = a.atMost(2, "rename")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 2 {
			return missing("id and new id")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		if err := p.Rename(a.pos[0], a.pos[1]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: renamed %s to %s (commit the move under sunstack/)\n", a.pos[0], a.pos[1])
		return nil

	case "library":
		a, err := parse(rest, "root as", "force")
		if err == nil {
			err = a.atMost(2, "library")
		}
		if err != nil {
			return err
		}
		if len(a.pos) == 0 {
			for _, e := range core.Library() {
				fmt.Fprintf(stdout, "%-20s %-9s %s\n", e.Title, e.Source, e.Summary)
			}
			return nil
		}
		if a.pos[0] == "show" {
			if len(a.pos) < 2 {
				return missing("title")
			}
			b, src, ok := core.Template(a.pos[1])
			if !ok {
				return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "no template " + a.pos[1]}
			}
			fmt.Fprintf(stdout, "source: %s\n----- AGENT.md -----\n%s", src, b)
			return nil
		}
		if a.pos[0] != "save" {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "sunstack library [show <title> | save <id> [--as TITLE]]"}
		}
		if len(a.pos) < 2 {
			return missing("id")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		dest, err := p.LibrarySave(a.pos[1], a.flags["as"], a.has("force"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: saved %s\n", dest)
		return nil

	case "team", "log", "inbox", "pillar":
		a, err := parse(rest, "root id", "follow team")
		if err != nil {
			return err
		}
		if cmd == "team" || cmd == "log" {
			err = a.atMost(0, cmd)
		} else if cmd == "inbox" {
			err = a.atMost(1, cmd)
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		switch cmd {
		case "team":
			fmt.Fprint(stdout, p.TeamText())
		case "log":
			return followLog(p, a.flags["id"], a.has("follow"), stdout)
		case "inbox":
			if len(a.pos) < 1 {
				return missing("id")
			}
			if !p.HasAgent(a.pos[0]) {
				return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "no agent " + a.pos[0]}
			}
			entries := p.Inbox(a.pos[0])
			if len(entries) == 0 {
				fmt.Fprintf(stdout, "%s has no pending messages\n", a.pos[0])
			}
			for _, e := range entries {
				fmt.Fprintf(stdout, "%s  %-8s from %-16s %s  %s\n", e.At, e.Type, e.From, e.File, e.State)
			}
		case "pillar":
			if n := len(a.pos); n > 1 || (a.has("team") && n > 0) {
				return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "pillar only shows pillars; a change is proposed to the user and written with sunstack amend (see the save skill)"}
			}
			id := core.TeamID
			if !a.has("team") {
				if len(a.pos) < 1 {
					return missing("id")
				}
				id = a.pos[0]
			}
			out, err := p.Pillars(id)
			if err != nil {
				return err
			}
			fmt.Fprint(stdout, out)
		}
		return nil

	case "board":
		a, err := parse(rest, "root", "")
		if err == nil {
			err = a.atMost(1, "board")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		id := ""
		if len(a.pos) > 0 {
			id = a.pos[0]
		}
		out, err := p.BoardText(id)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, out)
		return nil

	case "answer":
		// The user answers an agent's ask (§16.4). The agent writes its own
		// board, so this only sends it the answer.
		a, err := parse(rest, "root", "no-nudge")
		if err == nil {
			err = a.atMost(3, "answer")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 3 {
			return missing("id, ask key (Q<n>) and answer")
		}
		if !askKeyRe.MatchString(a.pos[1]) {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "the ask key looks like Q1 (see the Asks on sunstack board)"}
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		ask, ok := p.OpenAsk(a.pos[0], a.pos[1])
		if !ok {
			return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: fmt.Sprintf("%s has no open ask %s", a.pos[0], a.pos[1])}
		}
		tool, session := detectTool()
		body := fmt.Sprintf("%s: %s\n\nThe ask was: %s\n", ask.Key, a.pos[2], ask.Text)
		r, err := p.Send(core.SendOptions{To: a.pos[0], Body: body, Type: "answer", NoNudge: a.has("no-nudge"), FromSession: session,
			Via: p.CallerLabel(tool, session, os.Getenv("TMUX_PANE"), tmuxSocket())})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: sent %s to %s\n", r.ID, r.To)
		if r.Nudged != "" {
			fmt.Fprintf(stdout, "nudged session %s\n", r.Nudged)
		} else {
			fmt.Fprintf(stdout, "%s\n", r.Note)
		}
		return nil

	case "direct":
		a, err := parse(rest, "root to", "")
		if err == nil {
			err = a.atMost(1, "direct")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("directive text")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		var to []string
		for _, t := range strings.Split(a.flags["to"], ",") {
			if t = strings.TrimSpace(t); t != "" {
				to = append(to, t)
			}
		}
		tool, session := detectTool()
		key, err := p.Direct(a.pos[0], to, p.CallerLabel(tool, session, os.Getenv("TMUX_PANE"), tmuxSocket()))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: added %s to BOARD.md; agents align with it at their next as or save\n", key)
		return nil

	case "tidy":
		a, err := parse(rest, "root", "all team")
		if err == nil {
			err = a.atMost(1, "tidy")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		var owners []string
		switch {
		case a.has("all"):
			owners = append([]string{core.TeamID}, p.Agents()...)
		case a.has("team"):
			owners = []string{core.TeamID}
		case len(a.pos) == 1:
			owners = a.pos
		default:
			return missing("id, --team or --all")
		}
		for _, o := range owners {
			n, err := p.Tidy(o)
			if err != nil {
				return err
			}
			name := o
			if o == core.TeamID {
				name = "team"
			}
			fmt.Fprintf(stdout, "sunstack: tidied %s, %d entr(ies) archived\n", name, n)
		}
		return nil

	case "sessions":
		a, err := parse(rest, "root", "json")
		if err == nil {
			err = a.atMost(0, "sessions")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		ss := p.Sessions()
		if a.has("json") {
			b, _ := json.MarshalIndent(ss, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		if len(ss) == 0 {
			fmt.Fprintln(stdout, "no live sessions")
		}
		for _, s := range ss {
			where := s.Where
			if where == "" {
				where = "not in tmux"
			}
			fmt.Fprintf(stdout, "%-28s %-7s %-16s %-24s last contact %s\n", s.Name, s.Tool, s.Host, where, s.LastContact)
			if s.Resume != "" {
				fmt.Fprintf(stdout, "%-28s resume: %s\n", "", s.Resume)
			}
		}
		return nil

	case "send":
		a, err := parse(rest, "root type reply-to from token op file", "no-nudge")
		if err == nil {
			err = a.atMost(2, "send")
		}
		if err != nil {
			return err
		}
		var body string
		if a.has("file") {
			// The body from a file, so a long brief needs no shell quoting.
			if len(a.pos) > 1 {
				return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "give the message text or --file, not both"}
			}
			if body, err = readBody(a.flags["file"]); err != nil {
				return err
			}
		} else if len(a.pos) == 2 {
			body = a.pos[1]
		}
		if len(a.pos) < 1 || (len(a.pos) < 2 && !a.has("file")) {
			return missing("recipient and message text")
		}
		tool, session := detectTool()
		opts := core.SendOptions{To: a.pos[0], Body: body, Type: a.flags["type"], ReplyTo: a.flags["reply-to"],
			From: a.flags["from"], Token: a.flags["token"], NoNudge: a.has("no-nudge"), Op: a.flags["op"], FromSession: session}
		p, perr := core.FindProject(a.flags["root"])
		if p != nil {
			opts.Via = p.CallerLabel(tool, session, os.Getenv("TMUX_PANE"), tmuxSocket())
		} else if tool != "" {
			opts.Via = tool + " session"
		}
		to := opts.To
		external := strings.HasPrefix(to, "%") || core.IsSessionID(to) || strings.Contains(to, "/")
		if external && opts.From != "" {
			// Sending as an agent beyond its team: check the claim here, then
			// name the sender <team>/<id> to the recipient.
			if p == nil {
				return perr
			}
			if err := p.VerifySender(opts.From, opts.Token); err != nil {
				return err
			}
			opts.FromLabel = p.TeamLabel(opts.From)
		}
		var r *core.SendResult
		var err2 error
		switch {
		case strings.HasPrefix(to, "%") || core.IsSessionID(to):
			s, err := core.FindSession(to, tmuxSocket())
			if err != nil {
				return err
			}
			if s.Agent != "" {
				// It works as an agent: deliver to that agent's session inbox.
				dst, err := core.FindProject(s.TeamRoot)
				if err != nil {
					return err
				}
				opts.To = strings.SplitN(s.Label, "@", 2)[0]
				if p != nil && dst.Root == p.Root {
					opts.FromLabel = "" // same team: the sender is named as usual
				} else {
					opts.From = ""
				}
				r, err2 = dst.Send(opts)
			} else {
				r, err2 = core.SendToSession(s, opts)
			}
		case strings.Contains(to, "/"):
			team, agent, _ := strings.Cut(to, "/")
			dst, err := core.ResolveTeam(team)
			if err != nil {
				return err
			}
			opts.To, opts.From = agent, ""
			r, err2 = dst.Send(opts)
		default:
			if p == nil {
				return perr
			}
			r, err2 = p.Send(opts)
		}
		if err2 != nil {
			return err2
		}
		shown := r.To
		if r.Session != "" {
			shown = r.Session
		}
		fmt.Fprintf(stdout, "sunstack: sent %s to %s\n", r.ID, shown)
		if r.Nudged != "" {
			fmt.Fprintf(stdout, "nudged session %s\n", r.Nudged)
		} else {
			fmt.Fprintf(stdout, "%s\n", r.Note)
		}
		return nil

	case "check", "take", "ack":
		a, err := parse(rest, "root token", "session")
		if err == nil {
			err = a.atMost(map[string]int{"check": 1, "take": 2, "ack": 2}[cmd], cmd)
		}
		if err != nil {
			return err
		}
		if a.has("session") {
			// A session that works as no agent: its host inbox, found by its
			// own session ID.
			_, sid := detectTool()
			switch cmd {
			case "check":
				ms, err := core.HostCheck(sid)
				if err != nil {
					return err
				}
				printMessages(stdout, ms)
			case "take", "ack":
				if len(a.pos) < 1 {
					return missing("message-id")
				}
				if err := core.HostMove(sid, a.pos[len(a.pos)-1], cmd == "ack"); err != nil {
					return err
				}
				fmt.Fprintf(stdout, "sunstack: %s %s\n", map[string]string{"take": "took", "ack": "acked"}[cmd], a.pos[len(a.pos)-1])
			}
			return nil
		}
		need := map[string]int{"check": 1, "take": 2, "ack": 2}[cmd]
		if len(a.pos) < need {
			return missing(map[string]string{"check": "id", "take": "id message-id", "ack": "id message-id"}[cmd])
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		switch cmd {
		case "check":
			ms, err := p.Check(a.pos[0], a.flags["token"])
			if err != nil {
				return err
			}
			printMessages(stdout, ms)
		case "take":
			if err := p.Take(a.pos[0], a.flags["token"], a.pos[1]); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "sunstack: took %s\n", a.pos[1])
		case "ack":
			if err := p.Ack(a.pos[0], a.flags["token"], a.pos[1]); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "sunstack: acked %s\n", a.pos[1])
		}
		return nil

	case "spawn":
		a, err := parse(rest, "root tool task note brief", "window over-cap")
		if err == nil {
			err = a.atMost(1, "spawn")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("id")
		}
		brief := ""
		if a.has("brief") {
			// Checked before anything opens: a bad brief costs no session.
			if brief, err = readBody(a.flags["brief"]); err != nil {
				return err
			}
			if err := core.CheckBrief(brief); err != nil {
				return err
			}
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		tool := a.flags["tool"]
		if tool == "" {
			if tool, _ = detectTool(); tool == "" {
				tool = "claude"
			}
		}
		r, err := p.Spawn(core.SpawnOptions{Arg: a.pos[0], Tool: tool, Task: a.flags["task"], Socket: tmuxSocket(), Server: tmuxServer(), Note: a.flags["note"],
			Window: a.has("window"), Caller: os.Getenv("TMUX_PANE"), Brief: brief, OverCap: a.has("over-cap")})
		if err != nil {
			return err
		}
		_ = p.Register()
		where := "a pane beside this one"
		if r.Window {
			where = "a new tmux window"
		}
		fmt.Fprintf(stdout, "sunstack: started %s (%s) in %s, pane %s\n", r.Name, tool, where, r.Pane)
		if !r.Running {
			fmt.Fprintf(stdout, "warning: %s is not running in pane %s yet; it may be at a login or trust prompt, or have exited. Look at the pane (tmux select-pane -t %s); if the session is gone, run sunstack kill %s\n", tool, r.Pane, r.Pane, r.Name)
		}
		return nil

	case "dismiss":
		a, err := parse(rest, "root", "")
		if err == nil {
			err = a.atMost(1, "dismiss")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("id or session name")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		msg, err := p.Dismiss(a.pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s\n", msg)
		return nil

	case "kill":
		a, err := parse(rest, "root", "yes")
		if err == nil {
			err = a.atMost(1, "kill")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("session name (sunstack sessions lists them)")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		plan, err := p.KillPlan(a.pos[0])
		if err != nil {
			return err
		}
		if !a.has("yes") {
			if !setup.IsTerminal(os.Stdin) {
				return &core.Error{Code: core.ExitUsage, Reason: "confirm", Msg: "this will " + plan + "; rerun with --yes once the user has confirmed"}
			}
			if !setup.Confirm(stdin, stdout, "This will "+plan+". Unsaved work in that session is lost. Continue?", false) {
				fmt.Fprintln(stdout, "sunstack: nothing closed")
				return nil
			}
		}
		msg, err := p.Kill(a.pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s\n", msg)
		return nil

	case "hr":
		a, err := parse(rest, "root", "")
		if err == nil {
			err = a.atMost(0, "hr")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, p.HR(time.Now()).Text())
		return nil

	case "whoami":
		a, err := parse(rest, "root", "")
		if err == nil {
			err = a.atMost(0, "whoami")
		}
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		_, session := detectTool()
		held := p.HeldBy(session, os.Getenv("TMUX_PANE"), tmuxSocket())
		if len(held) == 0 {
			return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "this session holds no Sunstack identity here; run the as skill"}
		}
		fmt.Fprint(stdout, whoamiText(p, held))
		return nil

	case "org":
		a, err := parse(rest, "by", "attention json")
		if err == nil {
			err = a.atMost(0, "org")
		}
		if err != nil {
			return err
		}
		view := a.flags["by"]
		switch view {
		case "", "team", "agent", "host":
		default:
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--by must be team, agent or host"}
		}
		if a.has("attention") {
			view = "attention"
		}
		o := core.BuildOrg()
		if a.has("json") {
			b, _ := json.MarshalIndent(o, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		fmt.Fprint(stdout, o.Text(view))
		return nil

	case "doing":
		a, err := parse(rest, "root token", "")
		if err == nil {
			err = a.atMost(2, "doing")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 2 {
			return missing("id and a one-line description (\"\" clears it)")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		if err := p.SetDoing(a.pos[0], a.flags["token"], a.pos[1]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: doing set for %s\n", a.pos[0])
		return nil

	case "peek":
		a, err := parse(rest, "root lines", "")
		if err == nil {
			err = a.atMost(1, "peek")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("session name or tmux pane (%N)")
		}
		n := 30
		if v := a.flags["lines"]; v != "" {
			if n, err = strconv.Atoi(v); err != nil || n < 1 || n > 500 {
				return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--lines must be 1 to 500"}
			}
		}
		p, _ := core.FindProject(a.flags["root"])
		out, err := p.Peek(a.pos[0], tmuxSocket(), n)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, out)
		return nil

	case "migrate":
		a, err := parse(rest, "root scan apply", "host json")
		if err == nil {
			err = a.atMost(0, "migrate")
		}
		if err != nil {
			return err
		}
		apply := a.flags["apply"]
		if apply != "" && apply != "safe" && apply != "all" {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--apply must be safe or all"}
		}
		var teams []*core.Project
		hostWide := a.has("host") || a.flags["scan"] != ""
		if hostWide && a.flags["root"] != "" {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "use --root for one team, or --host / --scan for many, not both"}
		}
		if dir := a.flags["scan"]; dir != "" {
			if _, err := core.ScanTeams(dir); err != nil {
				return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
			}
		}
		if hostWide {
			teams = core.IndexedProjects()
		} else {
			p, err := core.FindProject(a.flags["root"])
			if err != nil {
				return err // never widen a failed lookup to every team
			}
			teams = []*core.Project{p}
		}
		core.ThisHost()
		type row struct {
			Root  string             `json:"root"`
			Steps []core.MigrateStep `json:"steps"`
			Done  []string           `json:"done,omitempty"`
		}
		var rows []row
		for _, p := range teams {
			r := row{Root: p.Root, Steps: p.MigratePlan()}
			if r.Steps == nil {
				r.Steps = []core.MigrateStep{}
			}
			if apply != "" && len(r.Steps) > 0 {
				d, err := p.Migrate(apply == "all")
				if err != nil {
					return err
				}
				r.Done = d
			}
			rows = append(rows, r)
		}
		if a.has("json") {
			b, _ := json.MarshalIndent(map[string]any{"schema": 1, "teams": rows}, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "no teams found (run inside a team, or sunstack migrate --scan <dir>)")
		}
		for _, r := range rows {
			fmt.Fprintf(stdout, "%s\n", r.Root)
			if len(r.Steps) == 0 {
				fmt.Fprintln(stdout, "  up to date")
			}
			for _, s := range r.Steps {
				kind := "ask "
				if s.Safe {
					kind = "auto"
				}
				fmt.Fprintf(stdout, "  [%s] %s\n", kind, s.What)
			}
			for _, d := range r.Done {
				fmt.Fprintf(stdout, "  done: %s\n", d)
			}
		}
		if apply == "" {
			fmt.Fprintln(stdout, "\n[auto] steps only add files; --apply safe runs them. [ask] steps rewrite files agents read; --apply all runs everything after the user agrees. Commit the changed files afterwards.")
		}
		return nil

	case "reopen":
		a, err := parse(rest, "", "window")
		if err == nil {
			err = a.atMost(1, "reopen")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("session ID")
		}
		r, err := core.Reopen(core.ReopenOptions{SessionID: a.pos[0], Socket: tmuxSocket(), Server: tmuxServer(), Caller: os.Getenv("TMUX_PANE"), Window: a.has("window")})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: reopened %s session %s in tmux pane %s (%s)\n", r.Tool, a.pos[0], r.Pane, r.Cwd)
		return nil

	case "teams":
		a, err := parse(rest, "scan", "prune json")
		if err == nil {
			err = a.atMost(0, "teams")
		}
		if err != nil {
			return err
		}
		if dir := a.flags["scan"]; dir != "" {
			found, err := core.ScanTeams(dir)
			if err != nil {
				return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
			}
			fmt.Fprintf(stdout, "sunstack: found %d team(s) under %s\n", len(found), dir)
		}
		if a.has("prune") {
			gone, err := core.PruneTeams()
			if err != nil {
				return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
			}
			for _, g := range gone {
				fmt.Fprintf(stdout, "dropped %s (no team there any more)\n", g)
			}
		}
		list := core.Teams()
		if a.has("json") {
			b, _ := json.MarshalIndent(map[string]any{"schema": 1, "host": core.ThisHost(), "teams": list}, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "no teams indexed on this host yet (they are added by init, as and spawn; or run sunstack teams --scan <dir>)")
		}
		for _, e := range list {
			id := e.ID
			if id == "" {
				id = "(no TEAM file; run sunstack init there)"
			}
			fmt.Fprintf(stdout, "%-20s %s  %s  last seen %s\n", e.Name, e.Root, id, e.LastSeen)
		}
		return nil

	case "hook":
		// Called by the plugin's UserPromptSubmit and SessionStart hooks;
		// never fails the prompt.
		var in struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
			Event     string `json:"hook_event_name"`
			Source    string `json:"source"`
		}
		_ = json.NewDecoder(stdin).Decode(&in)
		p, err := core.FindProject(in.Cwd)
		if err != nil {
			// Outside a team, only the host inbox can hold something.
			if in.Event != "SessionStart" {
				if n := core.HostPending(in.SessionID); n > 0 {
					hookContext(stdout, fmt.Sprintf("[sunstack] %d message(s) are waiting for this session. Use the Sunstack check skill (sunstack check --session) when it fits the current work.", n))
				}
			}
			return nil
		}
		if in.Event == "SessionStart" {
			// After compaction, /clear or a resume the model has lost the
			// session state; give it back so it never has to guess.
			held := p.HeldBy(in.SessionID, os.Getenv("TMUX_PANE"), tmuxSocket())
			if len(held) == 0 {
				return nil
			}
			ctx := "[sunstack] " + whoamiText(p, held) + rulesText(p, held) + "Before more Sunstack work, reload the identity files: run the as skill with this id and --token (it resumes the claim, no new one)."
			b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart", "additionalContext": ctx}})
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		lines := p.PendingForSession(in.SessionID, os.Getenv("TMUX_PANE"), tmuxSocket())
		if n := core.HostPending(in.SessionID); n > 0 {
			lines = append(lines, fmt.Sprintf("%d message(s) for this session itself (sunstack check --session)", n))
		}
		if len(lines) == 0 {
			return nil
		}
		hookContext(stdout, "[sunstack] "+strings.Join(lines, "; ")+". Use the Sunstack check skill to handle them when it fits the current work.")
		return nil

	case "tui":
		a, err := parse(rest, "root", "")
		if err != nil {
			return err
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		return tui.Run(p)

	case "health":
		a, err := parse(rest, "root", "")
		if err != nil {
			return err
		}
		return health(a.flags["root"], stdout)

	case "install", "update", "uninstall":
		a, err := parse(rest, "", "claude codex yes skip-binary")
		if err != nil {
			return err
		}
		t := setup.Detect(setup.Targets{Claude: a.has("claude"), Codex: a.has("codex")})
		if !t.Claude && !t.Codex {
			return &core.Error{Code: core.ExitFail, Reason: "no_cli", Msg: "neither claude nor codex found on PATH"}
		}
		return lifecycle(cmd, t, a.has("yes"), a.has("skip-binary"), rest, stdin, stdout)
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "unknown command: " + cmd + " (see sunstack help)"}
}

func lifecycle(cmd string, t setup.Targets, yes, skipBinary bool, args []string, stdin io.Reader, out io.Writer) error {
	var failed []string
	step := func(name string, err error) {
		if err != nil {
			failed = append(failed, name)
			fmt.Fprintf(out, "! %s failed: %v\n", name, err)
		}
	}
	switch cmd {
	case "install":
		if t.Claude {
			step("claude plugin", setup.InstallClaude(out))
			if setup.HasClaudeRules() {
				fmt.Fprintf(out, "Claude Code already has the sunstack permission rules\n")
			} else if yes || setup.Confirm(stdin, out, fmt.Sprintf("Update %s: allow %q (no prompt before each call), and ask before sunstack amend, hire, rename, fire, kill, direct and answer (you approve every rule change and every new or deleted agent)?", setup.ClaudeSettingsPath(), setup.AllowRule), false) {
				_, err := setup.SetClaudeRules(true)
				step("permission rules", err)
				if err == nil {
					fmt.Fprintf(out, "added allow %s and ask %s (previous file kept as settings.json.bak)\n", setup.AllowRule, strings.Join(setup.AskRules, ", "))
				}
			} else {
				fmt.Fprintf(out, "skipped the permission rules; rerun with --yes, or add allow %q and ask %s yourself\n", setup.AllowRule, strings.Join(setup.AskRules, ", "))
			}
		}
		if t.Codex {
			step("codex plugin", setup.InstallCodex(out))
			err := setup.SetCodexRules(true)
			step("codex rule", err)
			if err == nil {
				fmt.Fprintf(out, "wrote %s: Codex asks before sunstack amend, hire, rename, fire, kill, direct and answer\n", setup.CodexRulesPath())
			}
			if setup.CodexAutoReviewsApprovals() {
				fmt.Fprintln(out, "note: your Codex config sets approvals_reviewer, so Codex approval prompts go to an automatic reviewer, not to you. The rule then cannot guarantee you see each amend; the skill still asks you before every rule change.")
			}
		}
	case "update":
		switch {
		case skipBinary:
		case version == "dev":
			fmt.Fprintln(out, "development build: skipping the binary update")
		default:
			if latest, err := setup.LatestVersion(); err != nil {
				step("check latest release", err)
			} else if latest != version {
				fmt.Fprintf(out, "updating sunstack %s -> %s\n", version, latest)
				if err := setup.SelfUpdate(latest); err != nil {
					step("binary update", err)
				} else if exe, err := os.Executable(); err == nil {
					// Let the new binary update the plugin and apply its own rules.
					c := exec.Command(exe, append([]string{"update", "--skip-binary"}, args...)...)
					c.Stdin, c.Stdout, c.Stderr = stdin, out, out
					if err := c.Run(); err != nil {
						return &core.Error{Code: core.ExitFail, Reason: "update_failed", Msg: "the new binary could not finish the update: " + err.Error()}
					}
					return nil
				}
			} else {
				fmt.Fprintf(out, "sunstack %s is the latest release\n", version)
			}
		}
		if t.Claude {
			step("claude plugin", setup.UpdateClaude(out))
			// The user allowed sunstack before; add any ask rules a newer version needs.
			if setup.HasAllowRule() && !setup.HasClaudeRules() {
				_, err := setup.SetClaudeRules(true)
				step("permission rules", err)
				if err == nil {
					fmt.Fprintf(out, "added the newer ask rules: %s\n", strings.Join(setup.AskRules, ", "))
				}
			}
		}
		if t.Codex {
			step("codex plugin", setup.UpdateCodex(out))
			if setup.CodexRulesState() == "outdated" {
				step("codex rule", setup.SetCodexRules(true))
			}
		}
		// Bring the teams on this host up to this version: safe steps now,
		// the rest is offered by the as and checkup skills.
		core.ThisHost()
		asks := 0
		for _, p := range core.IndexedProjects() {
			done, err := p.Migrate(false)
			if err != nil {
				step("migrate "+p.Root, err)
				continue
			}
			for _, d := range done {
				fmt.Fprintf(out, "migrated %s: %s\n", p.Root, d)
			}
			asks += len(p.PendingAsks())
		}
		if asks > 0 {
			fmt.Fprintf(out, "%d team change(s) wait for your OK (protocol, README, AGENTS.md): run sunstack migrate --host, or /sunstack:checkup\n", asks)
		}
	case "uninstall":
		if t.Claude {
			step("claude plugin", setup.UninstallClaude(out))
			if yes || setup.Confirm(stdin, out, fmt.Sprintf("Remove the sunstack permission rules from %s?", setup.ClaudeSettingsPath()), false) {
				_, err := setup.SetClaudeRules(false)
				step("permission rules", err)
			}
		}
		if t.Codex {
			step("codex plugin", setup.UninstallCodex(out))
			step("codex rule", setup.SetCodexRules(false))
		}
	}
	if len(failed) > 0 {
		return &core.Error{Code: core.ExitFail, Reason: cmd + "_failed", Msg: strings.Join(failed, ", ")}
	}
	fmt.Fprintf(out, "sunstack: %s done\n", cmd)
	return nil
}

// followLog prints the event log, then keeps printing new lines with --follow.
func followLog(p *core.Project, id string, follow bool, out io.Writer) error {
	seen := 0
	for {
		lines := p.Events(id)
		for _, l := range lines[min(seen, len(lines)):] {
			fmt.Fprintln(out, l)
		}
		seen = len(lines)
		if !follow {
			return nil
		}
		time.Sleep(time.Second)
	}
}

// health prints project checks (when inside a project) and install checks.
func health(root string, out io.Writer) error {
	var cs []core.Check
	if p, err := core.FindProject(root); err == nil {
		cs = p.Health()
	} else {
		cs = append(cs, core.Check{Level: "fail", Area: "project", Msg: "no sunstack/ here or above", Fix: "sunstack init (sets up sunstack/ in the current folder)"})
	}
	cs = append(cs, core.Check{Level: "ok", Area: "install", Msg: "sunstack " + version})
	if _, err := exec.LookPath("claude"); err == nil {
		if setup.HasClaudeRules() {
			cs = append(cs, core.Check{Level: "ok", Area: "install", Msg: "Claude Code allows sunstack and asks before amend, hire, rename, fire, kill, direct and answer"})
		} else {
			if setup.HasAllowRule() {
				cs = append(cs, core.Check{Level: "warn", Area: "migrate", Msg: "Claude Code permission rules are from an older sunstack", Fix: "sunstack update"})
			} else {
				cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: "Claude Code permission rules are missing", Fix: "sunstack install --claude"})
			}
		}
	}
	if _, err := exec.LookPath("codex"); err == nil {
		switch setup.CodexRulesState() {
		case "current":
			cs = append(cs, core.Check{Level: "ok", Area: "install", Msg: "Codex asks before amend, hire, rename, fire, kill, direct and answer (" + setup.CodexRulesPath() + ")"})
		case "outdated":
			cs = append(cs, core.Check{Level: "warn", Area: "migrate", Msg: "Codex rules are from an older sunstack", Fix: "sunstack update"})
		case "foreign":
			cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: setup.CodexRulesPath() + " was not written by sunstack", Fix: "merge the sunstack rules by hand"})
		default:
			cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: "Codex rules for amend, hire, rename, fire, kill, direct and answer are missing", Fix: "sunstack install --codex"})
		}
		if setup.CodexAutoReviewsApprovals() {
			cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: "Codex approvals_reviewer sends approval prompts to an automatic reviewer; rule changes rely on the skill asking you"})
		}
	}
	failedN := 0
	for _, c := range cs {
		fmt.Fprintf(out, "%-4s %-8s %s\n", c.Level, c.Area, c.Msg)
		if c.Fix != "" {
			fmt.Fprintf(out, "              fix: %s\n", c.Fix)
		}
		if c.Level == "fail" {
			failedN++
		}
	}
	// Ranked summary: failures first, then warnings, then waiting work.
	var steps []string
	seen := map[string]bool{}
	for _, level := range []string{"fail", "warn", "next"} {
		for _, c := range cs {
			if c.Level == level && c.Fix != "" && !seen[c.Msg+c.Fix] {
				seen[c.Msg+c.Fix] = true
				steps = append(steps, fmt.Sprintf("[%s] %s: %s", level, c.Msg, c.Fix))
			}
		}
	}
	if len(steps) == 0 {
		fmt.Fprintf(out, "\nnext steps: none, all good\n")
	} else {
		fmt.Fprintf(out, "\nnext steps:\n")
		for i, s := range steps {
			fmt.Fprintf(out, "%d. %s\n", i+1, s)
		}
	}
	if failedN > 0 {
		return &core.Error{Code: core.ExitFail, Reason: "health", Msg: fmt.Sprintf("%d check(s) failed", failedN)}
	}
	return nil
}

// whoamiText prints the session state of each claim this session holds.
func whoamiText(p *core.Project, held []core.Held) string {
	var b strings.Builder
	for _, h := range held {
		if h.ByPane {
			fmt.Fprintf(&b, "This tmux pane holds %s, claimed from a different CLI session ID (for example before /clear). If that was this session, it is yours; otherwise ask the user.\n", h.L.Label(h.ID))
		} else {
			fmt.Fprintf(&b, "This session holds %s.\n", h.L.Label(h.ID))
		}
		b.WriteString(strings.TrimPrefix(p.StateText(h.ID, h.L.Token, h.L.Task), "\n"))
	}
	return b.String()
}

// pillarCap bounds each agent's pillar text in the SessionStart hook.
const pillarCap = 4096

// rulesText gives back each held agent's effective pillars and unaligned
// directives after compaction (§16.3), so an agent that acts before it
// reloads its identity still has its rules.
func rulesText(p *core.Project, held []core.Held) string {
	var b strings.Builder
	boards := p.LoadBoards()
	seen := map[string]bool{}
	for _, h := range held {
		if seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		pillars, err := p.Pillars(h.ID)
		if err != nil {
			continue
		}
		if len(pillars) > pillarCap {
			cut := strings.LastIndex(pillars[:pillarCap], "\n")
			pillars = pillars[:cut+1] + fmt.Sprintf("... run sunstack pillar %s for the rest\n", h.ID)
		}
		fmt.Fprintf(&b, "===== pillars of %s (follow them now, before the reload) =====\n%s", h.ID, pillars)
		for _, d := range boards.Unaligned(h.ID) {
			fmt.Fprintf(&b, "not aligned: %s %s %s\n", d.Key, d.Date, d.Text)
		}
	}
	return b.String()
}

// migrateNote runs a team's safe migration steps at as, and tells the session
// what it changed and which steps wait for the user.
func migrateNote(p *core.Project) string {
	done, _ := p.Migrate(false)
	asks := p.PendingAsks()
	if len(done) == 0 && len(asks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n===== migration (this team was made by an older sunstack) =====\n")
	for _, d := range done {
		fmt.Fprintf(&b, "done: %s\n", d)
	}
	for _, a := range asks {
		fmt.Fprintf(&b, "needs the user's OK: %s\n", a)
	}
	if len(asks) > 0 {
		b.WriteString("Ask the user once; on yes run: sunstack migrate --apply all --root \"" + p.Root + "\"\n")
	}
	if len(done) > 0 || len(asks) > 0 {
		b.WriteString("Then tell the user which files changed, to commit them.\n")
	}
	return b.String()
}

func printMessages(stdout io.Writer, ms []*core.Message) {
	if len(ms) == 0 {
		fmt.Fprintln(stdout, "no messages")
	}
	for _, m := range ms {
		from := m.From
		if m.Via != "" {
			from += " (via " + m.Via + ")"
		}
		fmt.Fprintf(stdout, "===== %s (%s) =====\nfrom: %s\ntype: %s\nat: %s\n", m.ID, m.State, from, m.Type, m.At)
		if m.FromSession != "" {
			fmt.Fprintf(stdout, "from_session: %s\n", m.FromSession)
		}
		if m.ReplyTo != "" {
			fmt.Fprintf(stdout, "reply_to: %s\n", m.ReplyTo)
		}
		fmt.Fprintf(stdout, "\n%s\n", strings.TrimRight(m.Body, "\n"))
	}
}

func hookContext(stdout io.Writer, ctx string) {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "UserPromptSubmit", "additionalContext": ctx}})
	fmt.Fprintln(stdout, string(b))
}
