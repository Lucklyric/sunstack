// Command sunstack does Sunstack's deterministic work: claims, snapshots and
// commits, and installing the plugin into Claude Code and Codex (design §6, §10).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/setup"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `sunstack — agent-team layer for Claude Code and Codex

Agent identity (used by the plugin's skills):
  sunstack as <id|title> [--token T] [--takeover --expect CLAIM] [--tool claude|codex] [--root DIR]
  sunstack snapshot <id> <context.md|threads/<topic>.md> --token T [--root DIR]
  sunstack snapshot <id> <pillars.md|AGENT.md> [--root DIR]      rule files, no token needed
  sunstack snapshot --team [--root DIR]                           team PILLARS.md
  sunstack commit <id> <target> <candidate> <checksum> --token T [--root DIR]
  sunstack commit <id> <target> --delete <checksum> --token T [--root DIR]
  sunstack release <id> --token T [--root DIR]

Self-improvement (the user approves every call; both CLIs prompt for it):
  sunstack amend <id> <pillars.md|AGENT.md> <candidate> <checksum> --summary "..." [--root DIR]
  sunstack amend --team <candidate> <checksum> --summary "..." [--root DIR]

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

func missing(what string) error {
	return &core.Error{Code: core.ExitUsage, Reason: "missing_arguments", Msg: "missing " + what, Stdout: "missing_arguments: " + what + "\n"}
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
		a, err := parse(rest, "root token expect tool", "takeover")
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
			Expect: a.flags["expect"], Pane: os.Getenv("TMUX_PANE"), Session: session,
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
			a.pos = []string{core.TeamID, "PILLARS.md"}
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
			a.pos = append([]string{core.TeamID, "PILLARS.md"}, a.pos...)
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
			} else if yes || setup.Confirm(stdin, out, fmt.Sprintf("Add two rules to %s: allow %q (no prompt before each call) and ask %q (you approve every pillar or AGENT.md change)?", setup.ClaudeSettingsPath(), setup.AllowRule, setup.AskRule), false) {
				_, err := setup.SetClaudeRules(true)
				step("permission rules", err)
				if err == nil {
					fmt.Fprintf(out, "added allow %s and ask %s (previous file kept as settings.json.bak)\n", setup.AllowRule, setup.AskRule)
				}
			} else {
				fmt.Fprintf(out, "skipped the permission rules; rerun with --yes, or add allow %q and ask %q yourself\n", setup.AllowRule, setup.AskRule)
			}
		}
		if t.Codex {
			step("codex plugin", setup.InstallCodex(out))
			err := setup.SetCodexRules(true)
			step("codex rule", err)
			if err == nil {
				fmt.Fprintf(out, "wrote %s: Codex asks before every sunstack amend\n", setup.CodexRulesPath())
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
		}
		if t.Codex {
			step("codex plugin", setup.UpdateCodex(out))
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
