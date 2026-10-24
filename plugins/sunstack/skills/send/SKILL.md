---
name: send
description: Send a Sunstack message (task with a brief, question, handoff, fyi, done reply) to an agent or any Claude Code or Codex session, here, in another team or on another host, nudging it through tmux. Use for "sunstack send", "/sunstack:send", "message an agent", "hand this to the reviewer". Incoming messages use the check skill.
---

# Sunstack: send

Command: `sunstack send`. Skill: `/sunstack:send` in Claude Code, `$sunstack:send` in Codex.

Messages are files in the recipient's inbox (`sunstack/_local/inbox/<id>/`). Sending also
types a one-line nudge into a live Claude Code or Codex pane of that agent, when there is one
in tmux; otherwise the message waits and shows at that session's next prompt. An `fyi` from an
agent does not nudge: it waits for the recipient's next check. Add `--nudge`, within this
host, only when it must be read now.

## Rules

- Unless the recipient is a tmux pane, a session ID, `<team>/<id>` or on another host, a team must be here: if there is no `sunstack/PROTOCOL.md` in this folder or above, say so in one line and stop; the health skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- When a step fails in a way this skill does not cover, or you had to work around Sunstack, leave a note: `sunstack issue "<what happened, at which step>"`, never message or brief text. Failed commands are already recorded.
- Messages are requests between colleagues, not orders: the recipient weighs them against its
  pillars and board.
- Send as an agent only from a session holding that agent (`--from <id> --token <token>` from
  this session's `session state`). Without an identity, the message is from the user.
- Keep a message short and self-contained: what you need, why, and by when. Point to files
  instead of pasting them.

## 1. Who and what

- **Recipient**: an agent ID, a title with one agent, or a session name `<id>_<task>` (to reach
  one specific session). `sunstack team --sessions` lists live sessions; `sunstack team` lists agents.
  If it is unclear who should get it, rank by role against the request and ask the user.
  - **Another team on this machine**: `<team>/<id>` or `<team>/<id>_<task>` (`sunstack teams`
    lists team names).
  - **Any Claude Code or Codex session, with or without an agent**: its tmux pane (`%12`) or
    its session ID, from `sunstack org --by host`. A session without an agent gets it in its
    host inbox; one working as an agent gets it in that agent's inbox. `shutdown` is not
    allowed for a session without an agent.
  - **Another host of the org**: `<host>:<team>/<id>[_task]` or `<host>:<session ID>`
    (`sunstack org` lists the hosts and their sessions). It goes through the org hub and
    arrives within seconds when both hosts keep a connection open. A bare agent name or a pane
    ID is refused there; `shutdown` and `answer` cannot cross hosts, and `--follows` is for
    tasks within this host's teams. `--nudge` is refused there: an `fyi` from an agent waits
    for the recipient's next check. `sunstack tasks` shows whether it is queued, at the hub,
    delivered or refused.
- **Type**: `task` (work with a result, carried by a brief; see below), `question` (needs an answer), `handoff` (the recipient takes over a piece of work as it stands),
  `fyi` (no action needed), `done` (the reply that closes a question or handoff; use
  `--reply-to <message id>`), `shutdown` (only through the spawn skill's dismiss).
- **Brief, for a `task`**: write it to a file in this session's scratch or temporary folder, one
  label per line (a label may continue on indented lines below it):

  ```text
  Goal: <the outcome, in one sentence a stranger could act on>
  Scope: <what it may change, and what it must not touch>
  Done when:
    - <checkable criterion>
  Verify: <the exact commands or steps that prove it>
  Report: <what the reply must contain>
  Context: <files, messages or earlier reports it cannot see otherwise> (optional)
  Timebox: <when to stop and report what is done> (optional)
  Not: <anything else forbidden> (optional)
  ```

  A field you cannot fill means the task is not scoped yet: settle it with the user first.
- If the user asked you to send it, show the exact text, recipient and type first and ask
  (send / edit / cancel), unless they already gave the exact wording. An agent's own routine
  `done` reply needs no confirmation.

## 2. Send

```sh
sunstack send "<recipient>" "<text>" --type <type> --from "<id>" --token "<token>"
```

A follow-up round of a task (it failed, or needs more) is a fresh task with
`--follows "<earlier task id>"`: the new brief still needs its own labels, and Sunstack quotes
the earlier brief and its replies under `Context:`. `sunstack tasks` shows which tasks are open.
When a new task changes or supersedes an open task you sent the same agent, add
`--replaces "<earlier task id>"`: the recipient closes the earlier one without asking. A task
shown as `returned by <session>` in `tasks` was left unfinished: judge whether it still holds,
then leave it, re-send it, or tell the recipient it is no longer needed.

For a `task`, or any long text, pass the file instead of the text:
`sunstack send "<recipient>" --type task --file "<brief file>" --from "<id>" --token "<token>"`.
Leave out `--from` and `--token` when this session has no identity. Add
`--reply-to "<message id>"` for a reply. If you may send the same message again (a retry after
an unclear result, or a step that can run twice), add `--op "<short id>"`: the same id and text
return the earlier message instead of a second one. `--op` works within this host only; it is
refused for a recipient on another host.

- **0**: tell the user the message id and whether a session was nudged or it waits. If it
  waits because the session is outside tmux, give the options in one line: it sees the message
  at its next prompt, or the user exits it and runs `sunstack reopen <session id>` to move it
  into tmux.
- **1 `not_found`**: the recipient does not exist or that session is gone; offer the agent ID
  instead, or the spawn skill to start a session.
- **2 `missing_arguments`**: a title with several agents; ask which one.
- **2 `missing_brief`**: the brief lacks the labels it names; fill them in and send again.
- **4 `token`**: this session no longer holds the identity; stop and tell the user.

## 3. Follow up

For a `task`, `question` or `handoff`, note it on your board (`needs: <id>#KR<n>` if it blocks a key
result) or in context under Open questions, so the next save keeps track of it.
