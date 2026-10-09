---
name: org
description: Show the user's whole Sunstack org on this host and the other hosts of its org, every team, agent and running Claude Code or Codex session (with or without a Sunstack identity), in four views, what needs the user, work by team, people, and hosts, and inspect or summarize any session or team on request. Use when the user says "sunstack org", "/sunstack:org", "what needs me", "what is running on this machine", "show all my sessions", "what is every team doing across teams", "summarize that session", "peek at <session>", "sunstack teams", "work on another host", "should I use SSH", or asks for a team's event log, inbox or effective pillars. To start or close sessions, use the spawn skill.
---

# Sunstack: org

The org view covers this host: every team in its index (`~/.sunstack/teams.json`), their
agents and boards, and every running Claude Code and Codex session, found from the processes
themselves. Sessions that hold no agent are "free": inside a team's project, or grouped by git
remote or folder. It is read only.

When this host is in an org (several hosts connected through one hub), `sunstack org` also
lists the other hosts after this one, each with its state (live, stale, offline) and the age of
its last contact, from their last snapshots. Those show no prompts, replies or pane text, and
their sessions can only be messaged from here. `sunstack org --refresh` fetches them first when
no connector runs (`sunstack hub connect` keeps one open).

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
then `sunstack reopen <session id>` resumes the same conversation in its team's tmux session
(beside this pane for a session in no team; `--place here` or `--place window` to choose). An
agent's claim follows it to the new pane. `sunstack attach <session>` goes to a session's pane.

**Asks.** Agents' questions for the user show in the attention list as
`<id>#Q<n> asks the user: <question> (options: ...) (default: <answer> after <date>)`. Offer
to go through them in one pass. For each answer the user gives, run
`sunstack answer --root "<team root>" "<id>" Q<n> "<answer>"`. Asks "decided by default" stay
listed until archived, so the user can overrule one with an answer message.

**Open tasks.** `sunstack tasks` lists tasks sent and not yet answered, with replies;
`--all` shows closed ones too, `--from <id>` one sender's. Check progress read-only (tasks,
`org --by agent`, `peek`, the recipient's board). Never send "are you done?".

## 2. Report

Lead with what needs the user, then a short line per team (objectives moving, who is working,
anything stale). For free sessions in a team's project, say which agent's role fits the work
in its last prompt, if one does, so the user can run the as skill there. To pick what to do
first, hand over to the next skill.

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
- **A live dashboard**: tell the user to run `sunstack` (or `sunstack tui`) in a terminal. In a
  team folder it opens on that team; elsewhere on a picker of this host's teams. Tabs Team,
  Next and Org (tab or n and o), `t` to switch team, `?` for the keys. The Team tab is the
  same tree for the current team, and the Org tab one of every team, agent and session: select a session for its details and the end of its pane, then
  `enter` to go to it, `m` to message it, `R` to reopen it in tmux, `K` to close an agent's
  session (both ask first), `/` to filter, `f` to show only what needs the user. `sunstack tui --org`
  opens on the host view. It reopens on the last tab and team used. In an org, `h` opens the
  Hosts tab: the hosts drawn around the hub, each with its link state, teams and sessions;
  `enter` shows that host's sessions in the Org tab.

## 4. Work on another host: sunstack first, SSH for host admin and for starting work there

Before acting on another host, read its state from sunstack: `sunstack org --by host` for its
teams and sessions, `sunstack hub hosts` on the hub for its version and machine name, and
`sunstack ssh` for which SSH aliases share a connection that is open now and can be reused
(`ssh <alias>` then logs in without a new sign-in). Then pick the channel from the task:

- **Sunstack** for everything it covers: seeing the host's teams and sessions, and messaging an
  agent or session there. The user also runs peek, spawn and update on that host once it allows
  them (`sunstack org allow` there). These need no SSH, stay within the grants the other host
  set, and leave a record.
- **SSH** only for host administration sunstack does not do, such as installing or updating
  other tools, configuration, checking out a repo, copying files, services or debugging. Use it
  when the user asks for that work or approves it, through the user's own SSH host alias.
- **Starting work there, also while the user is away.** On a host the user set up for SSH
  (`sunstack ssh` shows it mapped, with key login and its last check `ok`), you may SSH there
  through the user's alias and run that host's own `sunstack` from the team's folder there:
  `sunstack spawn` (an agent session with `--brief`, or `--free` with `--note`) or `sunstack send`
  a task to an agent there. Do it only when the user's request or your board calls for that work on that host.
  - Use `ssh -o BatchMode=yes <alias> ...`, so it fails instead of waiting for a password.
  - Record it here first: when you work as an agent, a Now entry in your `board.md` that names
    the host, the session or message, and the goal. Otherwise tell the user in your reply. Report
    the session's label and how to attach to it.
  - The brief or note says the work came from an agent on another host and asks the session to check
    with the user before anything consequential. The session runs as the user there, under that
    host's permission rules.
  - A session started this way does not start more sessions on other hosts.
  - If that host's tmux server was not running, the new session may read as signed out (the
    keychain note below). Check it with `sunstack sessions` there and tell the user.
- **Never route around sunstack otherwise.** Apart from spawn and send above, when sunstack
  refuses an action (`user_only`, a missing grant, a read-only host), tell the user and let them
  run it or grant it. Do not do the same thing over SSH. `sunstack ssh check`, `start` and
  `stop`, `org allow` and `deny`, and `org update` stay the user's to run, here and over SSH.
- Over SSH, run that host's own `sunstack` for its view instead of reading its files directly.
  Never print secrets. Copy secret files only with the user's approval, and never show their
  contents.
- An SSH login cannot read the macOS login keychain, so sign-in checks (Claude, Codex, MCP
  servers) can read as signed out over SSH. Confirm them from a tmux session on that host, or
  peek a session's screen.
- **No SSH to that host yet, or a check fails:** run `sunstack ssh setup <host>` and show the
  user the steps it prints. Setting up SSH, checking it (`sunstack ssh check`) and opening or
  closing a shared connection (`sunstack ssh start` and `stop`) are the user's to run.
