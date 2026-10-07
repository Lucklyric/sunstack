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

The first line puts the `sunstack` binary in `~/.local/bin`. The second adds the plugin to Claude Code and Codex (whichever are on your PATH, or pick one with `--claude` / `--codex`). For Claude Code it also offers permission rules: allow `Bash(sunstack *)`, so you are not asked before every call, and ask before `sunstack amend`, `hire`, `rename`, `fire`, `kill`, `direct`, `answer` and `halt`, and before a host joins or leaves an org (`org join`, `org leave`, `org trust`, `hub init`, `hub invite`, `hub revoke`). For Codex it writes `~/.codex/rules/sunstack.rules` with the same asks.

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

- **Set up and check:** `init`, `health`, `update`, `migrate` → skill `checkup` (also migrates an older project and suggests which agent fits the session)
- **Work as an agent:** `as` (`--join`, `--task`) → skill `as`; `snapshot`, `commit`, `tidy` → skill `save`; `release` → skill `release`
- **What to do next:** `next` (one ranked list: broken, waiting on you, blocked, drift, then your own next step) → skill `next`
- **Objectives and alignment:** `board`, `amend --team BOARD.md` → skill `board`; `direct` → skill `direct`; `answer` (answer an agent's ask) → skills `board` and `org`; `halt` (pause the whole team) → skills `next` and `spawn`
- **Self-improvement:** `amend` → skill `save` (you approve every change)
- **Team members:** `hire`, `library` → skill `recruit`; `fire` → skill `fire`; `rename` → skill `checkup`
- **Messages:** `send` → skill `message` (a `task` carries a brief: Goal, Scope, Done when, Verify, Report; to an agent, `<team>/<id>` in another team, or any Claude Code or Codex session by pane or session ID); `check`, `take`, `ack` → skill `check` (with `--session` for a session without an agent)
- **Sessions:** `sessions`, `spawn` (`--brief` for a first task, the agent's own `tool:`, refused at the team's `max_sessions` and while halted), `dismiss`, `kill` → skill `spawn`
- **Delegation:** `tasks` (tasks sent and not yet answered), `send --follows` (a follow-up round that quotes the earlier one) → skills `org` and `message`
- **Staffing:** `hr` → skill `hr` (suggests hires, splits and retirements from the project and the boards)
- **Your org on this machine:** `org` (what needs you, work by team, people, hosts), `teams`, `peek`, `team`, `log`, `inbox`, `pillar` → skill `org`; `tui` (also a bare `sunstack`): in a team folder it opens on the team, outside one on a picker of this host's teams; tabs Team, Next and Org, each a list on the left with the selected item's details on the right. The Team tab is that tree for the current team (each agent with its sessions; an agent shows its pillars and board), and the Org tab is a tree of teams, agents and sessions with state marks (● busy, ◐ waiting, ⧗ blocked, ○ idle, ✕ gone); a session shows its doing line, last prompt and reply, and the end of its pane, and `enter` goes to its pane, `m` messages it, `R` reopens it in tmux, `K` closes an agent's session (both ask), `/` filters and `f` shows only what needs you, busy or outside tmux; `t` switches team, `?` lists the keys, `--org` opens on the host view; it reopens on the last tab and team (`~/.sunstack/tui.json`), and a team folder always opens its own team
- **Who am I:** `whoami` → skill `whoami`; `doing` (one line on what a session works on) → skills `as` and `save`

## How agents reach each other

A message is always a file in the recipient's inbox (`sunstack/_local/inbox/<id>/`), so it works everywhere and is never lost. Getting the recipient to look at it depends on where it runs:

- **A live Claude Code or Codex session in tmux:** `send` types a one-line nudge (`/sunstack:check …` or `$sunstack:check …`) into its pane. It only types into a pane whose foreground program is that CLI, never into a shell.
- **A Claude Code or Codex session outside tmux:** the message is delivered, and the plugin's prompt hook tells it at its next prompt. `sunstack org` marks such sessions; `sunstack reopen <session-id>` moves a closed one into a tmux pane so it can be woken.
- **No live session:** the message waits; `sunstack spawn` starts a session for the agent in a tmux pane beside yours, with your approval.

Several sessions of one agent share its inbox; a session takes a message before working on it, so only one handles it.

A nudge is typed only while the CLI shows its normal input box, never while it shows a menu, a question or an approval prompt, and it carries no digits or names that could pick an option. Before Enter the input line must hold exactly the nudge: if you were typing a draft there, the nudge is erased and your draft stays unsent. Otherwise the message waits for the hook or the next check.

Codex also loads the plugin's prompt hook and asks you to review it once, the first time a Codex session starts after install.

## Several hosts

Hosts of one org connect through one always-on host, the hub, over Tailscale. The hub stores files and passes traffic; each host stays in charge of its own sessions and inboxes.

- On the hub: `sunstack hub init <org>`, then `sunstack hub connect --install`, which keeps it listening on its Tailscale address only. For each other host, `sunstack hub invite` prints a one-time code (10 minutes). On that host: `sunstack org join <hub's Tailscale name> --code <code>`, then `sunstack hub connect --install`. No accounts and no SSH keys: each host gets its own token, tied to its Tailscale address.
- Message bodies are sealed to the receiving host and signed by the sender, so the hub carries mail it cannot read or change. Each host remembers the keys it first sees; a key that changes stops that host's mail until you compare fingerprints (`sunstack org keys` on both machines) and run `sunstack org trust <host>`.
- `sunstack org` and the TUI's Org and Hosts tabs show every host, with its state and the age of its last contact. Other hosts' prompts, replies and pane text never leave them.
- `sunstack send <host>:<team>/<agent> "..."` reaches an agent on another host within seconds. A message from another host is a request: the receiving session asks you before anything consequential, and user-only commands never run from another host.
- `sunstack hub revoke <host>` cuts a host off at once.

## Keeping work honest

- **Briefs.** A `task` message must say what to achieve, what it may touch, when it is done, how to check it and what to report, or it is refused.
- **Proof of done.** A finished key result says how it was checked (`verified: go test ./... @ f51abe4`, or `verified: none` with a reason); `sunstack board` flags the ones that do not.
- **Asks.** Questions only you can answer go on the agent's board with a default and a date. Work goes on around them, `sunstack org` lists them all in one place, `sunstack answer` replies, and an unanswered ask takes its default.
- **Rules after compaction.** The `SessionStart` hook gives a compacted session its pillars and unaligned directives back, not just its identity.
- **Resumes.** `as` starts with what changed since the agent last saved (commits, other agents' needs, new messages), so a resumed session continues instead of redoing work.
- **A ledger of tasks.** A task stays open until a `done` reply. `sunstack tasks` lists them, `board` flags one open for 3 days, and a task that failed twice becomes a question for you instead of a third try.
- **A halt.** `sunstack halt "<reason>"` pauses the team: work in progress saves and stops, nothing new starts, until `sunstack halt --off`.
- **Review on the other CLI.** `tool: codex` (or `claude`) in an agent's `AGENT.md` sets the CLI `spawn` starts it on, so a reviewer can run on a different model from the builders.
- **A session cap.** `spawn` stops at `max_sessions` live sessions per team (`sunstack/TEAM`, default 6) unless you agree to more.

## What needs your approval

Agents update their own boards and context on their own. Changing a pillar, an agent's `AGENT.md` or the team objectives, and creating, renaming or deleting an agent, always waits for you: `sunstack install` sets Claude Code and Codex to ask before `sunstack amend`, `hire`, `rename`, `fire`, `kill`, `direct`, `answer` and `halt`, and before a host joins or leaves an org (`org join`, `org leave`, `org trust`, `hub init`, `hub invite`, `hub revoke`). If your Codex config sets `approvals_reviewer`, Codex sends those prompts to its automatic reviewer instead, and the skills' own questions are what keep you in the loop.

Every agent is `<title>.<name>`: the title is the role, the name is one agent in it, so a project can have `researcher.macro` and `researcher.equities`. Each has its own `AGENT.md` copy, pillars and context. A new agent's role comes from `~/.sunstack/library/` (your own, saved with `sunstack library save`), then the built-in `builder` and `reviewer`, then another agent of the same role in the project.

## Development

```sh
go test -race ./...
go build -o dist/sunstack ./cmd/sunstack
```

The tests build the binary and run it in separate processes, so the claim and commit races are real. Tagging `v*` publishes binaries for macOS, Linux and Windows through goreleaser.

Release only with the script, after bumping the version in both plugin manifests and committing:

```sh
scripts/release.sh v0.8.14 --update-local
```

It checks the tree and the manifests, runs the tests, pushes `main`, and waits for CI. It tags only when `scripts/ci-green.sh` reports that all three OS jobs passed, then waits for the release build, and updates this machine (`--update-local`) only after that succeeds. `scripts/ci-green.sh <commit>` alone says whether a commit is safe to tag.

## License

Apache-2.0
