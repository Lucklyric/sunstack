# Sunstack

Sunstack is a plugin for Claude Code and Codex that manages the agent layer of a project. It lets you hire a team of role agents (builder, reviewer, and others) and give the team and each agent project-level constraints called pillars. Each agent keeps a persistent context, and agents in the same project can message each other.

> **Status:** build step 2. The `sunstack` CLI does `as`, `snapshot`, `commit` and `release`, and installs the plugin. Not ready for real projects.

## Principles

- **A CLI does the deterministic work, the model does the judgment.** One `sunstack` binary handles claims, commits, locks and installs. The plugin holds only thin skills that call it.
- **Everything lives in the project.** `sunstack/` holds the team pillars and one folder per agent (`AGENT.md`, optional `pillars.md`, `context.md`, `threads/`), committed with git.
- **Host-local state stays out of git.** Claims, inboxes, locks, snapshots and the event log live in `sunstack/_local/`.
- **Humans own the constraints.** Agents write their own context and only propose changes to their identity or pillars.
- **One session per agent ID.** Parallel work uses named instances such as `builder.alice`.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh
sunstack install
```

The first line puts the `sunstack` binary in `~/.local/bin`. The second adds the plugin to Claude Code and Codex (whichever are on your PATH, or pick one with `--claude` / `--codex`). For Claude Code it also offers to add one permission rule, `Bash(sunstack *)`, so you are not asked before every call.

Keep it current with `sunstack update`. Remove it with `sunstack uninstall`.

## Commands

- **Identity (used by the skills):** `as`, `snapshot`, `commit`, `release`
- **Setup:** `install`, `update`, `uninstall`, `version`
- **Planned:** `init`, `hire`, `fire`, `team`, `log`, `inbox`, `health`, `pillar`, `send`, `check`, `ack`, `spawn`, `dismiss`

In Claude Code the skills are `/sunstack:as`, `/sunstack:save` and `/sunstack:release`. In Codex they are `$sunstack:as`, `$sunstack:save` and `$sunstack:release`.

## Development

```sh
go test -race ./...
go build -o dist/sunstack ./cmd/sunstack
```

The tests build the binary and run it in separate processes, so the claim and commit races are real. Tagging `v*` publishes binaries for macOS, Linux and Windows through goreleaser.

## License

Apache-2.0
