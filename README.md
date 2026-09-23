# Sunstack

Sunstack is a plugin for Claude Code and Codex that manages the agent layer of a project. It lets you hire a team of role agents (builder, reviewer, and others) and give the team and each agent project-level constraints called pillars. Each agent keeps a persistent context, and agents in the same project can message each other.

> **Status:** early. The team, identity, self-improvement and dashboard commands work; agent-to-agent messaging (`send`, `check`, `spawn`) is next.

## Principles

- **A CLI does the deterministic work, the model does the judgment.** One `sunstack` binary handles claims, commits, locks and installs. The plugin holds only thin skills that call it.
- **Everything lives in the project.** `sunstack/` holds the team pillars and one folder per agent (`AGENT.md`, optional `pillars.md`, `context.md`, `threads/`), committed with git.
- **Host-local state stays out of git.** Claims, inboxes, locks, snapshots and the event log live in `sunstack/_local/`.
- **The user approves every self-improvement.** Agents write their own context automatically. A change to an agent's pillars or `AGENT.md` is only a proposal until the user approves it, and both Claude Code and Codex prompt before `sunstack amend` runs.
- **One session per agent ID.** Parallel work uses named instances such as `builder.alice`.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh
sunstack install
```

The first line puts the `sunstack` binary in `~/.local/bin`. The second adds the plugin to Claude Code and Codex (whichever are on your PATH, or pick one with `--claude` / `--codex`). For Claude Code it also offers permission rules: allow `Bash(sunstack *)`, so you are not asked before every call, and ask before `sunstack amend`, `hire` and `fire`. For Codex it writes `~/.codex/rules/sunstack.rules` with the same asks.

Keep it current with `sunstack update`. Remove it with `sunstack uninstall`.

## Quick start

```sh
cd your-project
sunstack init                 # sunstack/, the AGENTS.md block, the .gitignore line
sunstack hire builder alice   # or ask an agent to recruit one from a description
sunstack                      # the team dashboard
```

Then, in Claude Code, `/sunstack:as builder.alice` (in Codex, `$sunstack:as builder.alice`).

## Commands

- **Team:** `init`, `hire`, `fire`, `library`, `library save`, `team`, `log`, `inbox`, `pillar`, `health`, `tui`
- **Identity (used by the skills):** `as`, `snapshot`, `commit`, `release`
- **Self-improvement:** `amend`, always approved by the user
- **Setup:** `install`, `update`, `uninstall`, `version`
- **Planned:** `send`, `check`, `ack`, `spawn`, `dismiss`

Skills, as `/sunstack:<name>` in Claude Code and `$sunstack:<name>` in Codex:

- `as`: take on an agent
- `save`: save what it learned, and ask you to approve any rule change
- `release`: hand the agent back
- `recruit`: describe a role in a sentence; it drafts the agent for your approval

## What needs your approval

Agents update their own context on their own. Changing a pillar or an agent's `AGENT.md`, and creating or deleting an agent, always waits for you: `sunstack install` sets Claude Code and Codex to ask before `sunstack amend`, `hire` and `fire`. If your Codex config sets `approvals_reviewer`, Codex sends those prompts to its automatic reviewer instead, and the skills' own questions are what keep you in the loop.

Role templates come from `~/.sunstack/library/` (your own, saved with `sunstack library save`) and then from the built-in `builder` and `reviewer`.

## Development

```sh
go test -race ./...
go build -o dist/sunstack ./cmd/sunstack
```

The tests build the binary and run it in separate processes, so the claim and commit races are real. Tagging `v*` publishes binaries for macOS, Linux and Windows through goreleaser.

## License

Apache-2.0
