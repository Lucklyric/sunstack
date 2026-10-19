---
name: clean
description: Close the Sunstack team's idle sessions and leftover panes in one go (agent and free sessions at their prompt over 24h with nothing unfinished, and panes whose Claude or Codex exited), after one confirmation. Use for "sunstack clean", "/sunstack:clean", "clean up", "clean up sessions", "clean up panes", "close idle sessions", "close exited panes", "sunstack kill --idle". Closing one named session is the spawn skill. Only in projects with a sunstack/ folder.
---

# Sunstack: clean

Command: `sunstack clean` (also `kill --idle`). Skill: `/sunstack:clean` in Claude Code, `$sunstack:clean` in Codex.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the health skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Closing sessions needs the user's yes, once for the whole list.

## What it closes

`sunstack next` in a team shows one `[user]` line when there is something to close, for example
`2 agent session(s) idle over 24h: builder.alice_csv (3d), reviewer_ui (1d); 1 pane(s) whose CLI
exited: scratch (free, claude exited)`. `sunstack clean` closes three kinds, all checked by the CLI,
not by you:

- **Idle agent sessions:** in a tmux pane, at their prompt, nothing for 24 hours (no claim,
  save, `doing` line or transcript change), no taken message and no staged save.
- **Idle free sessions** of this team: in a tmux pane, at their prompt, no transcript change
  for 24 hours, no taken message in their session inbox.
- **Exited panes:** panes sunstack opened for this team (a spawn or a free session) where
  Claude or Codex has exited and the pane is back at its shell with nothing running. An agent's
  claim on such a pane is dropped too. A window or tmux home closes with its last pane.

It never closes this session's own pane, a pane you opened yourself, or a pane running anything
but its shell. A session with unsaved or taken work is never idle; dismiss it instead (spawn
skill), so it saves first.

1. Run `sunstack clean` (no `--yes`). It changes nothing: it exits 2 and lists exactly
   what would close, or says there is nothing to close.
2. Show the list and ask once: close all / cancel. To keep one open, dismiss or leave it and
   close the others one by one with `sunstack kill "<id_task>"` (or `sunstack kill <label>` for
   a free session) instead.
3. On yes: `sunstack clean --yes`. Each one prints `closed <name>`. Errors name the ones
   not closed (the reasons are as for `sunstack kill`).

Agents never run it for themselves; a lead or PM session runs it only on the user's yes. It
covers this team only: for free sessions outside a team, list them with `sunstack org --by host`
and close one with `sunstack kill <label>`.
