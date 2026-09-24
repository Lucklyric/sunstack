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

Self-improvement and objectives (the user approves every call; both CLIs prompt for it):
  sunstack amend <id> <pillars.md|AGENT.md> <candidate> <checksum> --summary "..." [--root DIR]
  sunstack amend --team [BOARD.md] <candidate> <checksum> --summary "..." [--root DIR]

Boards:
  sunstack board [id]                             objectives, key results by objective, directives, and what
                                                  needs attention (stale, overdue, blocked, not aligned)
  sunstack direct "<text>" [--to ID,TITLE,...]    add a dated directive from the user to BOARD.md (default: all)
  sunstack tidy <id> | --team | --all             archive old finished entries by month; creates a missing board

Sessions and messages:
  sunstack sessions [--json]                      every live session: name, tool, host, tmux place, resume command
  sunstack send <id|title|id_task> "<text>" [--type question|handoff|fyi|done|shutdown] [--reply-to MSG]
                [--from ID --token T] [--no-nudge]
                                                  write a message to the agent's inbox, then type a one-line nudge
                                                  into a live Claude Code or Codex pane of that agent (tmux)
  sunstack check <id> --token T                   messages this session may handle (pending, and taken by it)
  sunstack take <id> <msg> --token T              take a message so no other session works on it
  sunstack ack <id> <msg> --token T               archive a handled message
  sunstack spawn <id|title> [--tool claude|codex] [--task LABEL] [--note "..."]
                                                  open a tmux window and start a session as that agent
  sunstack dismiss <id|id_task> [--force]         ask a session to finish; --force closes a spawned pane

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

// tmuxSocket is the server socket from $TMUX ("socket,pid,session").
func tmuxSocket() string {
	if t := os.Getenv("TMUX"); t != "" {
		return strings.SplitN(t, ",", 2)[0]
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
	if s := os.Getenv("CODEX_SESSION_ID"); s != "" {
		return "codex", s
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
			Expect: a.flags["expect"], Pane: os.Getenv("TMUX_PANE"), Socket: tmuxSocket(), Session: session,
			Join: a.has("join"), Task: a.flags["task"],
		})
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, p.Bundle(r))
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
		a, err := parse(rest, "root token", "")
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
		key, err := p.Direct(a.pos[0], to)
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
		a, err := parse(rest, "root type reply-to from token", "no-nudge")
		if err == nil {
			err = a.atMost(2, "send")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 2 {
			return missing("recipient and message text")
		}
		p, err := core.FindProject(a.flags["root"])
		if err != nil {
			return err
		}
		r, err := p.Send(core.SendOptions{To: a.pos[0], Body: a.pos[1], Type: a.flags["type"], ReplyTo: a.flags["reply-to"],
			From: a.flags["from"], Token: a.flags["token"], NoNudge: a.has("no-nudge")})
		if err != nil {
			return err
		}
		to := r.To
		if r.Session != "" {
			to = r.Session
		}
		fmt.Fprintf(stdout, "sunstack: sent %s to %s\n", r.ID, to)
		if r.Nudged != "" {
			fmt.Fprintf(stdout, "nudged session %s\n", r.Nudged)
		} else {
			fmt.Fprintf(stdout, "%s\n", r.Note)
		}
		return nil

	case "check", "take", "ack":
		a, err := parse(rest, "root token", "")
		if err == nil {
			err = a.atMost(map[string]int{"check": 1, "take": 2, "ack": 2}[cmd], cmd)
		}
		if err != nil {
			return err
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
			if len(ms) == 0 {
				fmt.Fprintln(stdout, "no messages")
			}
			for _, m := range ms {
				fmt.Fprintf(stdout, "===== %s (%s) =====\nfrom: %s\ntype: %s\nat: %s\n", m.ID, m.State, m.From, m.Type, m.At)
				if m.ReplyTo != "" {
					fmt.Fprintf(stdout, "reply_to: %s\n", m.ReplyTo)
				}
				fmt.Fprintf(stdout, "\n%s\n", strings.TrimRight(m.Body, "\n"))
			}
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
		a, err := parse(rest, "root tool task note", "")
		if err == nil {
			err = a.atMost(1, "spawn")
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
		tool := a.flags["tool"]
		if tool == "" {
			if tool, _ = detectTool(); tool == "" {
				tool = "claude"
			}
		}
		r, err := p.Spawn(core.SpawnOptions{Arg: a.pos[0], Tool: tool, Task: a.flags["task"], Socket: tmuxSocket(), Note: a.flags["note"]})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: started %s (%s) in a new tmux window, pane %s\n", r.Name, tool, r.Pane)
		return nil

	case "dismiss":
		a, err := parse(rest, "root", "force")
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
		msg, err := p.Dismiss(a.pos[0], a.has("force"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s\n", msg)
		return nil

	case "hook":
		// Called by the plugin's UserPromptSubmit hook; never fails the prompt.
		var in struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
		}
		_ = json.NewDecoder(stdin).Decode(&in)
		p, err := core.FindProject(in.Cwd)
		if err != nil {
			return nil
		}
		lines := p.PendingForSession(in.SessionID, os.Getenv("TMUX_PANE"), tmuxSocket())
		if len(lines) == 0 {
			return nil
		}
		ctx := "[sunstack] " + strings.Join(lines, "; ") + ". Use the Sunstack check skill to handle them when it fits the current work."
		b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "UserPromptSubmit", "additionalContext": ctx}})
		fmt.Fprintln(stdout, string(b))
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
		a, err := parse(rest, "", "claude codex yes")
		if err != nil {
			return err
		}
		t := setup.Detect(setup.Targets{Claude: a.has("claude"), Codex: a.has("codex")})
		if !t.Claude && !t.Codex {
			return &core.Error{Code: core.ExitFail, Reason: "no_cli", Msg: "neither claude nor codex found on PATH"}
		}
		return lifecycle(cmd, t, a.has("yes"), stdin, stdout)
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "unknown command: " + cmd + " (see sunstack help)"}
}

func lifecycle(cmd string, t setup.Targets, yes bool, stdin io.Reader, out io.Writer) error {
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
			} else if yes || setup.Confirm(stdin, out, fmt.Sprintf("Update %s: allow %q (no prompt before each call), and ask before sunstack amend, hire, rename and fire (you approve every rule change and every new or deleted agent)?", setup.ClaudeSettingsPath(), setup.AllowRule), false) {
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
				fmt.Fprintf(out, "wrote %s: Codex asks before sunstack amend, hire, rename and fire\n", setup.CodexRulesPath())
			}
			if setup.CodexAutoReviewsApprovals() {
				fmt.Fprintln(out, "note: your Codex config sets approvals_reviewer, so Codex approval prompts go to an automatic reviewer, not to you. The rule then cannot guarantee you see each amend; the skill still asks you before every rule change.")
			}
		}
	case "update":
		if version == "dev" {
			fmt.Fprintln(out, "development build: skipping the binary update")
		} else if latest, err := setup.LatestVersion(); err != nil {
			step("check latest release", err)
		} else if latest != version {
			fmt.Fprintf(out, "updating sunstack %s -> %s\n", version, latest)
			step("binary update", setup.SelfUpdate(latest))
		} else {
			fmt.Fprintf(out, "sunstack %s is the latest release\n", version)
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
			cs = append(cs, core.Check{Level: "ok", Area: "install", Msg: "Claude Code allows sunstack and asks before amend, hire, rename and fire"})
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
			cs = append(cs, core.Check{Level: "ok", Area: "install", Msg: "Codex asks before amend, hire, rename and fire (" + setup.CodexRulesPath() + ")"})
		case "outdated":
			cs = append(cs, core.Check{Level: "warn", Area: "migrate", Msg: "Codex rules are from an older sunstack", Fix: "sunstack update"})
		case "foreign":
			cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: setup.CodexRulesPath() + " was not written by sunstack", Fix: "merge the sunstack rules by hand"})
		default:
			cs = append(cs, core.Check{Level: "warn", Area: "install", Msg: "Codex rules for amend, hire, rename and fire are missing", Fix: "sunstack install --codex"})
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
