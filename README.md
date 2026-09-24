# Sunstack

Sunstack is a plugin for Claude Code and Codex that manages the agent layer of a project. It lets you hire a team of role agents (builder, reviewer, and others), give the team and each agent project-level constraints called pillars, and align everyone, you included, on dated objectives and key results. Each agent keeps a board of what it is doing and a persistent context of what it has learned.

> **Status:** early. Team, identity, boards, self-improvement, messaging and the dashboard work.

## Principles

- **A CLI does the deterministic work, the model does the judgment.** One `sunstack` binary handles claims, commits, locks and installs. The plugin holds only thin skills that call it.
- **Everything lives in the project.** `sunstack/` holds the team pillars, the team board (`BOARD.md`: your objectives, your own key results, your directives) and one folder per agent (`AGENT.md`, optional `pillars.md`, `board.md`, `context.md`, `threads/`, `archive/`), committed with git.
- **Everything is dated.** Every board, context and archive entry starts with a date, so staleness shows and old entries move to a monthly archive that is never loaded, only consulted.
- **Host-local state stays out of git.** Claims, inboxes, locks, snapshots and the event log live in `sunstack/_local/`.
- **The user approves every self-improvement.** Agents write their own context automatically. A change to an agent's pillars or `AGENT.md` is only a proposal until the user approves it, and both Claude Code and Codex prompt before `sunstack amend` runs.
- **Agents are like people.** Several sessions can work as one agent at once, each on its own key result and named `<id>_<task>`; shared files merge through checked commits, and contradictions go to you.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh
sunstack install
```

The first line puts the `sunstack` binary in `~/.local/bin`. The second adds the plugin to Claude Code and Codex (whichever are on your PATH, or pick one with `--claude` / `--codex`). For Claude Code it also offers permission rules: allow `Bash(sunstack *)`, so you are not asked before every call, and ask before `sunstack amend`, `hire`, `rename`, `fire` and `kill`. For Codex it writes `~/.codex/rules/sunstack.rules` with the same asks.

Keep it current with `sunstack update`. Remove it with `sunstack uninstall`.

## Quick start

```sh
cd your-project
sunstack init                 # sunstack/, the AGENTS.md block, the .gitignore line
sunstack hire builder alice   # or ask an agent to recruit one from a description
sunstack                      # the team dashboard
sunstack health               # checks, then a ranked list of next steps
```

Then, in Claude Code, `/sunstack:as builder.alice` (in Codex, `$sunstack:as builder.alice`).

## Commands and skills

Skills run as `/sunstack:<name>` in Claude Code and `$sunstack:<name>` in Codex. Each core command has a skill that drives it with the right questions:

- **Set up and check:** `init`, `health`, `update` → skill `checkup` (also migrates an older project and suggests which agent fits the session)
- **Work as an agent:** `as` (`--join`, `--task`) → skill `as`; `snapshot`, `commit`, `tidy` → skill `save`; `release` → skill `release`
- **Objectives and alignment:** `board`, `amend --team BOARD.md` → skill `board`; `direct` → skill `direct`
- **Self-improvement:** `amend` → skill `save` (you approve every change)
- **Team members:** `hire`, `library` → skill `recruit`; `fire` → skill `fire`; `rename` → skill `checkup`
- **Messages:** `send` → skill `message`; `check`, `take`, `ack` → skill `check`
- **Sessions:** `sessions`, `spawn`, `dismiss`, `kill` → skill `spawn`
- **Staffing:** `hr` → skill `hr` (suggests hires, splits and retirements from the project and the boards)
- **Watch:** `team`, `log`, `inbox`, `pillar`, `tui` (also a bare `sunstack`)

## How agents reach each other

A message is always a file in the recipient's inbox (`sunstack/_local/inbox/<id>/`), so it works everywhere and is never lost. Getting the recipient to look at it depends on where it runs:

- **A live Claude Code or Codex session in tmux:** `send` types a one-line nudge (`/sunstack:check …` or `$sunstack:check …`) into its pane. It only types into a pane whose foreground program is that CLI, never into a shell.
- **A Claude Code session elsewhere:** the plugin's prompt hook tells it about waiting messages at its next prompt.
- **No live session:** the message waits; `sunstack spawn` starts a session for the agent in a new tmux window, with your approval.

Several sessions of one agent share its inbox; a session takes a message before working on it, so only one handles it.

## What needs your approval

Agents update their own boards and context on their own. Changing a pillar, an agent's `AGENT.md` or the team objectives, and creating, renaming or deleting an agent, always waits for you: `sunstack install` sets Claude Code and Codex to ask before `sunstack amend`, `hire`, `rename`, `fire` and `kill`. If your Codex config sets `approvals_reviewer`, Codex sends those prompts to its automatic reviewer instead, and the skills' own questions are what keep you in the loop.

Every agent is `<title>.<name>`: the title is the role, the name is one agent in it, so a project can have `researcher.macro` and `researcher.equities`. Each has its own `AGENT.md` copy, pillars and context. A new agent's role comes from `~/.sunstack/library/` (your own, saved with `sunstack library save`), then the built-in `builder` and `reviewer`, then another agent of the same role in the project.

## Development

```sh
go test -race ./...
go build -o dist/sunstack ./cmd/sunstack
```

The tests build the binary and run it in separate processes, so the claim and commit races are real. Tagging `v*` publishes binaries for macOS, Linux and Windows through goreleaser.

## License

Apache-2.0
