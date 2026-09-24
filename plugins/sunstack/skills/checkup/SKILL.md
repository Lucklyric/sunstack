---
name: checkup
description: Check this project's Sunstack setup and team, and suggest the best next steps, such as init, cleanup, updating the install, approving pending rule changes, or recruiting an agent. Use when the user says "checkup", "sunstack checkup", "/sunstack:checkup", "sunstack health", "sunstack status", "set up sunstack", "sunstack init", "what should I do next with sunstack", or asks whether Sunstack is working.
---

# Sunstack: checkup

Find out where this project stands and walk the user through the next steps, one at a time.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it, from the folder the
  user is working in. Sunstack uses the current folder, never the git repository root.
- If the shell says `sunstack` is not found, tell the user to install it
  (`curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh`,
  then `sunstack install`) and stop.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer, a cancel, or the user moving on is not approval.
- Never delete, commit, hire, fire or amend without an explicit yes for that step.

## 1. Look

```sh
sunstack health
```

Exit 1 only means a check failed; read the output either way. If there is a project, also run:

```sh
sunstack team
```

## 2. Report

Give the user a short summary: whether there is a project here, who is on the team and who is
claimed, and the numbered `next steps` list that health printed, in its order (fail, then
warn, then next). Add one line of plain explanation per step where the fix text is not
self-explanatory. If health says `none, all good`, say so and stop.

## 3. Offer the steps

Offer the first step, or let the user pick one (options: do it / skip / stop). Do one step per
answer, then run `sunstack health` again and offer the next.

How to do each kind of step:

- **no sunstack/ here**: confirm the folder first (`pwd`): `sunstack init` creates `sunstack/`,
  an `AGENTS.md` block and a `.gitignore` line right here. On yes, run `sunstack init`.
- **PROTOCOL.md or AGENTS.md block is outdated**: `sunstack init --refresh`, then show the
  `git diff` of what changed.
- **install or rules missing or outdated**: `sunstack install` or `sunstack update`, as health
  says. They may ask their own questions in the terminal; if so, give the user the command to
  run themselves.
- **leftover lock, staging files, inbox of a fired agent, unfinished message**: show the exact
  path and what is in it (`ls -la`), then remove only that path on yes. Never remove a lock
  while a sunstack command may be running.
- **claim from a closed pane**: offer `/sunstack:as <id>` (Codex: `$sunstack:as <id>`) with
  takeover, or leave it.
- **damaged claim file**: show its content; delete it only on yes.
- **merge conflict markers**: show the conflicting lines and help resolve them by hand.
- **uncommitted changes under sunstack/**: show `git status --short sunstack/` and the diff;
  commit only if the user asks.
- **context.md too long**: suggest taking on that agent and saving, which compacts it.
- **no agents hired yet**: run `sunstack library`, ask what the user needs, then follow the
  recruit skill (`/sunstack:recruit`, Codex: `$sunstack:recruit`).
- **rule changes waiting for approval / unread messages**: suggest taking on that agent
  (`/sunstack:as <id>`, Codex: `$sunstack:as <id>`); its save step asks about each proposal.

When the user wants a live view instead, point them to `sunstack tui` in a terminal.
