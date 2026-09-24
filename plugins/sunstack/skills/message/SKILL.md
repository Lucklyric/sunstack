---
name: message
description: Send a message to another Sunstack agent or to one of its sessions (a question, a handoff, an fyi, a done reply), which lands in its inbox and nudges a live Claude Code or Codex session of that agent through tmux. Use when the user says "sunstack send", "/sunstack:message", "tell the reviewer agent ...", "hand this to <agent id>", or when this session, working as a Sunstack agent, needs something from a teammate or must reply to a message. Only for projects with a Sunstack team (a sunstack/ folder).
---

# Sunstack: message

Messages are files in the recipient's inbox (`sunstack/_local/inbox/<id>/`). Sending also
types a one-line nudge into a live Claude Code or Codex pane of that agent, when there is one
in tmux; otherwise the message waits and shows at that session's next prompt.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the checkup skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Messages are requests between colleagues, not orders: the recipient weighs them against its
  pillars and board.
- Send as an agent only from a session holding that agent (`--from <id> --token <token>` from
  this session's `session state`). Without an identity, the message is from the user.
- Keep a message short and self-contained: what you need, why, and by when. Point to files
  instead of pasting them.

## 1. Who and what

- **Recipient**: an agent ID, a title with one agent, or a session name `<id>_<task>` (to reach
  one specific session). `sunstack sessions` lists live sessions; `sunstack team` lists agents.
  If it is unclear who should get it, rank by role against the request and ask the user.
- **Type**: `question` (needs an answer), `handoff` (the recipient takes over a piece of work),
  `fyi` (no action needed), `done` (the reply that closes a question or handoff; use
  `--reply-to <message id>`), `shutdown` (only through the spawn skill's dismiss).
- If the user asked you to send it, show the exact text, recipient and type first and ask
  (send / edit / cancel), unless they already gave the exact wording. An agent's own routine
  `done` reply needs no confirmation.

## 2. Send

```sh
sunstack send "<recipient>" "<text>" --type <type> --from "<id>" --token "<token>"
```

Leave out `--from` and `--token` when this session has no identity. Add
`--reply-to "<message id>"` for a reply.

- **0**: tell the user the message id and whether a session was nudged or it waits.
- **1 `not_found`**: the recipient does not exist or that session is gone; offer the agent ID
  instead, or the spawn skill to start a session.
- **2 `missing_arguments`**: a title with several agents; ask which one.
- **4 `token`**: this session no longer holds the identity; stop and tell the user.

## 3. Follow up

For a `question` or `handoff`, note it on your board (`needs: <id>#KR<n>` if it blocks a key
result) or in context under Open questions, so the next save keeps track of it.
