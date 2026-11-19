---
name: clean
description: Close the Sunstack team's unused sessions and leftover panes in one go (agent and free sessions at their prompt over 24h with nothing unfinished, spawned sessions that released their agent, panes whose Claude or Codex exited, and claims whose pane is closed), after one confirmation. Use for "sunstack clean", "/sunstack:clean", "clean up", "clean up sessions", "clean up panes", "close idle sessions", "close unused sessions", "close finished sessions", "close exited panes", "too many sessions", "sunstack kill --idle". Closing one named session is the spawn skill. Only in projects with a Sunstack team (sunstack/PROTOCOL.md).
---

# Sunstack: clean

Command: `sunstack clean` (also `kill --idle`). Skill: `/sunstack:clean` in Claude Code, `$sunstack:clean` in Codex.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the health skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Closing sessions needs the user's yes, once for the whole list.

## What it closes

`sunstack next --team` shows one `[user]` line when there is something to close, for example
`2 agent session(s) idle over 24h: builder.alice_csv (3d), reviewer_ui (1d); 1 pane(s) whose CLI
exited: scratch (free, claude exited)`. `sunstack clean` closes five kinds, all checked by the CLI,
not by you:

- **Idle agent sessions:** in a tmux pane, at their prompt, nothing for 24 hours (no claim,
  save, `doing` line or transcript change), no taken message and no staged save.
- **Idle free sessions** of this team: in a tmux pane, at their prompt, no transcript change
  for 24 hours, no taken message in their session inbox. A free session that took an agent
  is checked as that agent's session instead.
- **Exited panes:** panes sunstack opened for this team (a spawn or a free session) where
  Claude or Codex has exited and the pane is back at its shell with nothing running (no
  background job). An agent's
  claim on such a pane is dropped too. A window or tmux home closes with its last pane.
- **Released sessions:** panes sunstack spawned for an agent whose session released it (after
  a shutdown or a finished task) and still runs, at its prompt for 10 minutes, with no taken
  message in its session inbox. Only the session that released runs there counts: a CLI
  started in that pane later is not closed.
- **Stale claims:** claims whose tmux pane is confirmed closed, as `sunstack release
  <id_task> --stale` drops them. Nothing is closed; the claim goes and its taken messages go
  back to the inbox.

It never closes this session's own pane, and it closes an exited pane only when sunstack opened
it and it runs nothing but its shell. An idle session is closed even in a pane you opened, so
check the list. It checks only taken messages and staged saves for unfinished work: a session
with other work (a Now entry on its board, files it has not saved) should be dismissed instead
(spawn skill), so it saves first. Right before closing, it checks the list again and leaves alone
anything no longer on it.

1. Run `sunstack clean` (no `--yes`). From an agent session it changes nothing: it exits 2,
   lists exactly what would close and ends with the command to run, `sunstack clean --root
   <root> --yes --plan <code>`. Or it says there is nothing to close. (In the user's own terminal it asks
   there instead.)
2. Show the list and ask once: close all / cancel. To keep one open, dismiss or leave it and
   close the others one by one with `sunstack kill "<id_task>"` (or `sunstack kill <label>` for
   a free session) instead.
3. On yes: run that command exactly as printed in step 1. The CLI asks the
   user to approve that command; this is expected. Each one prints `closed <name>`, or `left
   alone <name>` when it is no longer idle. Errors name what went wrong for each (the reasons
   are as for `sunstack kill`; a pane can be closed and its claim or messages still need a fix).
   - **4 `changed`:** the list changed after the user saw it, and nothing was closed. Show the
     new list it prints and ask again. Never run the new command without a new yes.

Agents never run it for themselves; a lead or PM session runs it only on the user's yes. It
covers this team only: for free sessions outside a team, list them with `sunstack org --by host`
and close one with `sunstack kill <label>`.
