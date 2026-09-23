# Sunstack

Sunstack is a plugin for Claude Code and Codex that manages the agent layer of a project. It lets you hire a team of role agents (builder, reviewer, and others) and give the team and each agent project-level constraints called pillars. Each agent keeps a persistent context, and agents in the same project can message each other.

> **Status:** build step 1, a spike of `as`, `save` and `release` in both CLIs. Not ready for real projects.

## Principles

- **Plain files plus skills.** There is no runtime binding: no environment variables and no `--agent`. Any Claude Code or Codex session takes on an identity with `as <id>` and writes back with `save`.
- **Everything lives in the project.** `sunstack/` holds the team pillars and one folder per agent (`AGENT.md`, optional `pillars.md`, `context.md`, `threads/`), committed with git.
- **Humans own the constraints.** Agents write their own context directly and only propose changes to their identity or pillars.
- **One session per agent ID.** Parallel work uses named instances such as `builder.alice`.
- **Scripts do the deterministic work, the model does the judgment.** Scripts are POSIX sh. tmux is optional.

## Commands (planned)

- **Team:** `init`, `hire`, `fire`, `team`, `health`
- **Constraints:** `pillar`
- **Identity and context:** `as`, `save`, `release`
- **Collaboration:** `send`, `check`, `spawn`, `dismiss`

Implemented so far: `as`, `save`, `release`.

## Install

One repo serves both CLIs: the root `.claude-plugin/marketplace.json` is read by Claude Code and by Codex.

Claude Code:

```sh
claude plugin marketplace add Lucklyric/sunstack
claude plugin install sunstack@sunstack
```

Codex:

```sh
codex plugin marketplace add Lucklyric/sunstack
codex plugin add sunstack@sunstack
```

## Development

```sh
sh tests/spike-test.sh    # script-level checks: claims, takeover, snapshot/commit, races, guards
```

Scripts live in `plugins/sunstack/scripts/`, skills in `plugins/sunstack/skills/`.

## License

Apache-2.0
