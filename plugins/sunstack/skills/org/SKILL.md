---
name: org
description: Show the user's whole Sunstack org on this host, every team, agent and running Claude Code or Codex session (with or without a Sunstack identity), in four views, what needs the user, work by team, people, and hosts, and inspect or summarize any session or team on request. Use when the user says "sunstack org", "/sunstack:org", "what needs me", "what is running on this machine", "show all my sessions", "what is every team doing across teams", "summarize that session", "peek at <session>", "sunstack teams", or asks for a team's event log, inbox or effective pillars. To start or close sessions, use the spawn skill.
---

# Sunstack: org

The org view covers this host: every team in its index (`~/.sunstack/teams.json`), their
agents and boards, and every running Claude Code and Codex session, found from the processes
themselves. Sessions that hold no agent are "free": inside a team's project, or grouped by git
remote or folder. It is read only.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- Nothing here types into a session. To give a free session an identity, the user runs the as
  skill in that session. To ask a free session something, use the message skill with its pane
  or session ID.
- Summaries are on request only: reading a transcript costs time and tokens.

## 1. Look

Pick the view from the request:

```sh
sunstack org
```

shows what needs the user, then work by team. Others:

- `sunstack org --attention`: only what waits on the user (sessions at a prompt or blocked,
  key results waiting on the user, broken or overdue board entries, unaligned directives).
- `sunstack org --by agent`: each agent's Now entries, `doing:` lines and sessions.
- `sunstack org --by host`: every session on this host, with its team or group.
- `--json` gives the same snapshot for further processing.

If a team the user expects is missing, run `sunstack teams --scan <folder>` (for example the
vault or code root), then look again. `sunstack teams --prune` drops teams that are gone.

**Reach.** Each session shows how a message gets to it:

- a tmux pane: a message wakes it at once (a nudge typed into its idle input box)
- `outside tmux (messages wait for its next prompt)`: the message is delivered, but the session
  sees it only when the user next types in it
- `background`: a Claude background session

For a session outside tmux that should be reachable, offer the options: leave it (it sees
messages at its next prompt), or move it into tmux: the user exits it where it runs (`/exit`),
then `sunstack reopen <session id>` resumes the same conversation in a pane beside this one
(`--window` for a new window). An agent's claim follows it to the new pane.

**Asks.** Agents' questions for the user show in the attention list as
`<id>#Q<n> asks the user: <question> (options: ...) (default: <answer> after <date>)`. Offer
to go through them in one pass. For each answer the user gives, run
`sunstack answer --root "<team root>" "<id>" Q<n> "<answer>"`. Asks "decided by default" stay
listed until archived, so the user can overrule one with an answer message.

## 2. Report

Lead with what needs the user, then a short line per team (objectives moving, who is working,
anything stale). For free sessions in a team's project, say which agent's role fits the work
in its last prompt, if one does, so the user can run the as skill there.

## 3. Drill in, on request

- **One session's screen**: `sunstack peek <id_task>` or `sunstack peek %<pane>`
  (`--lines N`, default 30).
- **Summarize a session**: `sunstack org --json` gives its transcript-free facts (title, last
  prompt, last reply, last active). For a fuller summary, read the session's transcript: Claude
  Code keeps it at `~/.claude/projects/*/<session_id>.jsonl`, Codex at the rollout file under
  `~/.codex/sessions/` whose name ends in its thread ID. Read only the end of the file, and
  report in a few lines: the goal, what was done, where it stands, and what it waits on.
- **One team**, from inside its project (or with `--root <dir>`):
  - `sunstack team`: who is on it and their sessions
  - `sunstack board`: objectives and key results (the board skill handles changes)
  - `sunstack inbox <id>`: messages waiting for an agent (read only)
  - `sunstack log [--id <id>]`: the event log
  - `sunstack pillar <id>` or `sunstack pillar --team`: the pillars in effect, with their source
- **A live dashboard**: tell the user to run `sunstack tui` in a terminal, from inside a team's
  project, and press `o`.
