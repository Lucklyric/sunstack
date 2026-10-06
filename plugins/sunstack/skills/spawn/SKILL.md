---
name: spawn
description: Start a new Claude Code or Codex session as a Sunstack agent in a tmux pane beside this one (or a new window), list live Sunstack sessions, ask a session to finish (dismiss), or close one at once (kill). Use when the user says "sunstack spawn", "/sunstack:spawn", "start a session for <agent id>", "open another builder session", "which Sunstack sessions are running in this project", "sunstack sessions", "dismiss <agent id>", "kill <session name>", or when a message waits for an agent that has no live session. Only for projects with a Sunstack team (a sunstack/ folder).
---

# Sunstack: spawn

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the checkup skill sets one up.
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

1. Choose the agent (rank as in the as skill's "Pick the agent"), the tool (default: the
   agent's `tool:` in its `AGENT.md`, else the one this session runs), a task label (up to 10 characters), and a one-line first
   instruction (no single quotes), for example "handle message <id>" or "work on KR2".
2. Choose where: by default a pane beside this one, in the current window. Suggest a new
   window (`--window`) only with a reason, and say it in the plan:
   - the current window already has three or more panes, so another would be too small;
   - several sessions are being started at once;
   - long background work the user does not need to watch;
   - the user asked for a separate window.
   If the new session is for a piece of work with a result, write a task brief (the message
   skill's template: Goal, Scope, Done when, Verify, Report) to a file and pass it with
   `--brief`; the session finds it in its inbox as its first task.
3. Show the plan (agent, tool, task, first instruction, brief, pane or window) and ask:
   start / edit / cancel.
4. On yes:

   ```sh
   sunstack spawn "<id>" --tool <claude|codex> --task "<task>" --note "<first instruction>"
   ```

   Add `--window` for a new window, and `--brief "<brief file>"` for a task brief. It splits this pane (side by side when wide, else
   stacked) in the project root, claims the agent for the new session, and starts the CLI
   with a first prompt that takes on the identity. The new session may stop at its first-run
   prompts (folder trust, sign-in); tell the user which pane to look at.
   - A `warning:` line means the CLI was not seen running a few seconds after launch: it may
     be at a login or trust prompt, or have exited. Tell the user which pane to look at.
   - **4 `task_taken`**: another session of that agent already has this task label; pick
     another.
   - **4 `team_full`**: the team already has `max_sessions` live sessions (`sunstack/TEAM`,
     default 6); the error lists them with their `doing` lines. Offer to finish one (dismiss) or
     to start it anyway; only on the user's yes rerun with `--over-cap`. A lead handing out
     many tasks starts the next session when one finishes, not all at once.
   - **2 `missing_brief`**: the brief lacks the labels named; fill them in. Nothing was opened.
   - **1 `halted`**: the team is halted (the error gives the reason). Tell the user; only
     they end it with `sunstack halt --off`.
   - **1 `no_space`**: this pane is too small to split; offer `--window` instead.
   - **1 `no_tmux`**: give the manual steps above.
   - **1 / 2 other**: show the error.

To give it work, send a message (message skill) to the new session name.

## Ask a session to finish

```sh
sunstack dismiss "<id or id_task>"
```

This sends a `shutdown` message (and a nudge). The session saves and releases; it is done
when `sunstack sessions` no longer lists it. Prefer this: nothing is lost.

## Close a session now (kill)

Only when the user asks for it, or a session is stuck and the user agrees. It closes that
one session's tmux pane at once; unsaved work in it is lost.

1. Name the exact session (`sunstack sessions`); if the agent has several, ask which.
2. Tell the user what will close and that unsaved work is lost; ask: close / cancel.
3. On yes:

   ```sh
   sunstack kill "<id_task>" --yes
   ```

   Claude Code and Codex also ask before running it. The claim is dropped and messages the
   session had taken go back to the inbox.
   - **1 `no_pane`**: the session is not in a tmux pane sunstack knows; the user closes it.
   - **1 `not_running`**: the pane no longer runs that CLI, so it is left alone; tell the user.
   - **2 `missing_arguments`**: several sessions; ask which.
   - **2 `self`**: that is this session's own pane; the user closes it.

From a terminal, the user can run `sunstack kill <id_task>` directly; it asks for
confirmation.
