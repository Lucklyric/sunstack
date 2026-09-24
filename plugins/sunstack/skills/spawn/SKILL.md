---
name: spawn
description: Start a new Claude Code or Codex session as a Sunstack agent in a tmux window, list live sessions, or ask a session to finish (dismiss). Use when the user says "spawn", "start a session for <agent>", "open another builder", "get the reviewer working on this", "who is running", "sessions", "dismiss", "stop <agent>", "/sunstack:spawn", or when a message waits for an agent that has no live session.
---

# Sunstack: spawn

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- Starting a session starts a paid model session: confirm with the user every time (agent,
  tool, task label, first instruction). Closing one needs a confirm too.
- `spawn` needs this session to run inside tmux. If it does not, give the user the steps to
  start it themselves: open a terminal in the project, run `claude` or `codex`, then
  `/sunstack:as <id>` (Codex: `$sunstack:as <id>`).

## Where things are

```sh
sunstack sessions
```

Lists every live session: name (`<id>_<task>`), tool, host, tmux place, and the command to
resume it. `sunstack team` shows the same per agent.

## Start a session

1. Choose the agent (rank by role against the work, as the as skill does), the tool (default:
   the one this session runs), a task label (up to 10 characters), and a one-line first
   instruction (no single quotes), for example "handle message <id>" or "work on KR2".
2. Show the plan and ask: start / edit / cancel.
3. On yes:

   ```sh
   sunstack spawn "<id>" --tool <claude|codex> --task "<task>" --note "<first instruction>"
   ```

   It opens a tmux window in the project root, claims the agent for the new session, and
   starts the CLI with a first prompt that takes on the identity. The new session may stop
   at its first-run prompts (folder trust, sign-in); tell the user which window to look at.
   - **1 `no_tmux`**: give the manual steps above.
   - **1 / 2 other**: show the error.

To give it work, send a message (message skill) to the new session name.

## Ask a session to finish

```sh
sunstack dismiss "<id or id_task>"
```

This sends a `shutdown` message (and a nudge). The session saves, replies `done`, and
releases. `dismiss --force` closes the pane at once, only for a session `spawn` started and
only after the user confirms losing unsaved work; for any other session, tell the user to
close it themselves.
