---
name: checkup
description: Check this project's Sunstack setup and team, migrate an older project to the current version, suggest which agent fits this session, and walk through the best next steps, such as init, cleanup, updating the install, approving pending rule changes, or recruiting an agent. Use when the user says "checkup", "sunstack checkup", "/sunstack:checkup", "sunstack health", "sunstack status", "set up sunstack", "sunstack init", "what should I do next with sunstack", "migrate sunstack", "which agent should I be", or asks whether Sunstack is working.
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
self-explanatory. Then add the session fit (step 3). If health says `none, all good` and the
session fit has nothing to suggest, say so and stop.

## 3. Session fit

Suggest what this session should be, from what the user has been doing in this conversation
(the files, the task, the questions so far) and each agent's role in `sunstack team`:

- **This session already holds an ID** (a `session state` block from `sunstack as` is in the
  conversation): say which, and whether the current work still fits its role. If it does not,
  suggest the agent that fits (save and release first, then take it on).
- **An agent fits and is free**: suggest `/sunstack:as <id>` (Codex: `$sunstack:as <id>`),
  with one line on why.
- **The fitting agent is claimed by another session**: say where (`sunstack team` shows it)
  and suggest another agent of the same role, or recruiting one.
- **No agent fits**: suggest recruiting one for this work, with a one-line role idea.
- **Nothing to go on** (a fresh session): skip this step, or ask in one question what the user
  is about to work on.

## 4. Offer the steps

Offer the first step, or let the user pick one (options: do it / skip / stop). Do one step per
answer, then run `sunstack health` again and offer the next.

**Migration first.** Checks in the `migrate` area mean the project, or the install, was set up
by an older Sunstack. Group them as one "migrate to this version" step: list each change, then
do them in this order, one approval for the batch unless a change needs its own input:

1. `sunstack update` when the Codex rules are from an older version (the user may need to run
   it in a terminal; the rest continues after).
2. `sunstack init --refresh`, then show `git diff` of `sunstack/` and `AGENTS.md`.
3. Unnamed agents: every agent is now `<title>.<name>`. For each one, suggest a name from its
   role and context (`researcher` → `researcher.lead`), ask the user to confirm or type their
   own, then `sunstack rename <id> <title>.<name>`. A claimed agent must be released first;
   skip it and say so.
4. Run `sunstack health` again and show that the `migrate` lines are gone; suggest committing
   `sunstack/`.

Old IDs keep working until they are renamed, so the user may skip migration.

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
