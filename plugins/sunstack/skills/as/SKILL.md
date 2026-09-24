---
name: as
description: Take on a Sunstack agent identity (for example builder.alice, or just builder when there is one) in this project, as a new session or alongside other sessions already working as that agent. Use when the user says "as <id>", "sunstack as <id>", "/sunstack:as", "be the builder", "take on whichever agent fits", "join builder.alice", or asks to pick up or switch to a Sunstack agent.
---

# Sunstack: as

Claim a Sunstack agent ID for this session and load its identity. Several sessions may work
as the same agent, like one person on several tasks; each has its own claim and token. The
`sunstack` command does the claiming; you decide what to ask the user and how to act after.

## Before you start

- If this session already holds a Sunstack identity, finish it first: run the save skill if
  there is anything worth keeping, then the release skill. Then take on the new one.
- Run every `sunstack` command on its own, with nothing chained after it (no `; echo ...`),
  so the user's permission rule for `sunstack` matches. The tool reports the exit code.
- If the shell says `sunstack` is not found, tell the user to install it and stop:
  `curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh`,
  then `sunstack install`.

## Pick the agent

If the user named an ID or a title, use it. If not (a bare `/sunstack:as`, "be whoever fits",
"take on an agent"), run `sunstack as` with no argument: it exits 2 and lists every agent as
`<id>  <free|active:N>  <role line>`. Then choose from the conversation:

- Rank agents by how well their role fits what this session is working on: the task the user
  described, the files and topics so far, the kind of work (research, building, reviewing,
  planning). Prefer a free agent; an active one can still be joined.
- Ask with the best fit first, marked as recommended, each option with one line on why. Offer
  at most four.
- If nothing has happened in this session yet, ask in one question what the user is about to
  work on, then rank.
- If no agent fits, say so and offer the recruit skill instead of a poor match.

This is a question, not a decision: end your turn and wait for the choice.

## Pick a task label

Give this session a short label for what it will work on: lowercase letters, digits and `-`,
at most 10 characters (`auth`, `kr2-tests`, `docs`). Take it from the user's request; if
there is nothing to go on, leave it out. The session is then named `<id>_<task>`.

## Run

```sh
sunstack as "<id-or-title>" --task "<task>"
```

Run it from the project directory, or add `--root "<dir>"`. Add `--token "<t>"` only to resume
a claim this session already holds: after compaction when the token is still in the
conversation, or when this session was started by `sunstack spawn` and its first prompt gave
the token.

## Handle the exit code

- **0**: the output is your identity. Read all of it: protocol, team pillars, the team
  `BOARD.md`, `AGENT.md`, agent pillars, your `board.md`, context, threads, inbox. From now on
  act as this agent, within the pillars and toward the objectives. Keep the `session state`
  values (root, id, token, task) in the conversation; every later save or release needs them.
  If `protocol` is not `2`, tell the user to run `sunstack update`.
  Then, in this order:
  1. **Name the session.** The tmux pane is already titled `session_name`. You cannot rename
     the Claude Code or Codex session yourself, so give the user the exact step in one line:
     in Claude Code `/rename <session_name>`; in Codex, open the agents list and use rename.
  2. **Directives.** If the output lists directives not aligned yet, check your board against
     each and propose the board changes they need; they are written at the next save (the save
     skill sets `aligned:`). If a directive conflicts with a pillar or another directive, ask.
  3. **Other sessions.** If other sessions work as this agent, say which (their task labels),
     and pick key results they are not on. Do not rewrite their Now entries.
  4. **Proposals.** If context has lines under `## Proposals`, tell the user how many and offer
     to review them now with the save skill's approval step.
  5. **Stale entries.** Entries whose date is old may be out of date: check before relying on
     them.
  6. **Inbox.** If messages are listed, mention them. Handle them with the check skill when
     the first prompt asked for it (a spawned session), or when the user agrees.
- **2 `missing_arguments`**: the first line names what is missing and the rest are the
  choices (for a title with several agents, only that title's). Rank them as in "Pick the
  agent" and ask the user: with AskUserQuestion if you have it, otherwise as numbered options
  in the conversation, and wait for the reply. Then run the command again with the answer. No
  answer, a cancel, or the user moving on is not a choice: stop.
- **2 `usage`**: show the error (for example a task label that is too long), then fix the
  arguments or ask the user. Do not loop.
- **4 `active`**: other sessions are working as this agent; the output lists them with their
  tasks. Ask the user: join them as another session, or pick another agent. On join, rerun
  with `--join`.
- **4 `occupied_same_pane`**: this same tmux pane holds a claim, most likely this session
  before compaction or `/clear`. Tell the user in one line and ask for a quick yes. On yes,
  rerun with `--takeover --expect "<claim>"`, using the `claim=` value from the output.
- **4 `occupied`** after a takeover: the claim you tried to replace is gone or changed. Show
  the current claim lines and ask again.
- **4 `token`**: the token you passed is no longer valid (released or taken over). Do not retry
  with a guessed token; tell the user and ask whether to claim again.
- **1 `empty_team`**: nobody is hired yet; offer the recruit skill.
- **1**: show the error (for example merge conflict markers, or no `sunstack/` folder). Do not
  work around it.

Take over another session's claim (`--takeover --expect`) only when the user confirms that
session is gone, for example its pane closed. Never invent, guess, or reuse a token you did not
get from `sunstack as` in this session.
