---
name: health
description: Check this project's Sunstack setup and health, set it up or migrate it, and fix what is off (install, cleanup, pending rule changes). Use for "sunstack health", "/sunstack:health", "sunstack checkup", "set up sunstack", "migrate sunstack", "upgrade sunstack", "update sunstack", or whether Sunstack works. Not for what to work on next (next skill).
---

# Sunstack: health

Command: `sunstack health` (also `init`, `update`, `migrate`, `rename`). Skill: `/sunstack:health` in Claude Code, `$sunstack:health` in Codex.

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

## Upgrade only

When the user asks only to upgrade or update Sunstack, skip the full health pass:

1. `sunstack update`. It updates the CLI and the Claude Code and Codex rules, and runs the
   `[auto]` steps. The user may need to run it in a terminal, and a second time if the first
   run updated the binary.
2. `sunstack migrate --host` lists the `[ask ]` steps for every team on this machine. Show
   them; on yes run `sunstack migrate --host --apply all`, then show `git diff --stat` of the
   changed `sunstack/` folders.
3. Run `sunstack health` and show that no `migrate` lines are left. Suggest committing the
   changed files and reloading the session to pick up the new skills.

Each other machine of the user (`sunstack org --by host` lists them) runs the same steps there.

## 1. Look

```sh
sunstack health
```

Exit 1 only means a check failed; read the output either way. `sunstack version` gives the CLI
and protocol versions if the user asks. If there is a project, also run:

```sh
sunstack team
```

## 2. Report

Give the user a short summary: whether there is a project here, who is on the team and who is
claimed, and the `fail` and `warn` checks that health printed with their fixes, failures
first. Add one line of plain explanation per step where the fix text is not
self-explanatory. Then add the session fit (step 3). If no check failed or warned and the
session fit has nothing to suggest, say so and stop.

## 3. Session fit

Suggest what this session should be, from what the user has been doing in this conversation
(the files, the task, the questions so far) and each agent's role in `sunstack team`:

- **This session already holds an ID** (a `session state` block from `sunstack as` is in the
  conversation): say which, and whether the current work still fits its role. If it does not,
  suggest the agent that fits (save and release first, then take it on).
- **Otherwise**, rank agents as in the as skill's "Pick the agent":
- **An agent fits and is free**: suggest `/sunstack:as <id>` (Codex: `$sunstack:as <id>`),
  with one line on why.
- **The fitting agent already has sessions**: say where (`sunstack team` shows them and their
  task labels) and suggest joining it for a different key result: one agent runs several
  sessions at once.
- **No agent fits**: check whether one almost fits and could be extended (hire skill);
  otherwise suggest recruiting one, with a one-line role idea.
- **Nothing to go on** (a fresh session): skip this step, or ask in one question what the user
  is about to work on.

## 4. Offer the steps

Setup and migration come first, here. For the team's work beyond setup (asks, blocked work,
board drift, agent sessions idle over 24h), hand over to the next skill, which ranks everything
in one list. Closing idle sessions is the spawn skill's "Close idle sessions".

Offer the first step, or let the user pick one (options: do it / skip / stop). Do one step per
answer, then run `sunstack health` again and offer the next.

**Migration first.** Checks in the `migrate` area mean the project, or the install, was set up
by an older Sunstack. Group them as one "migrate to this version" step: list each change, then
do them in this order, one approval for the batch unless a change needs its own input:

1. `sunstack update` when the Claude Code or Codex rules are from an older version (the user
   may need to run it in a terminal, and a second time if the first run updated the binary;
   the rest continues after).
2. `sunstack migrate` shows what this team needs, each step marked `[auto]` (only adds files:
   `TEAM`, `BOARD.md`, the `.gitignore` line, the host index) or `[ask ]` (rewrites files the
   agents read: `PROTOCOL.md`, `README.md`, the `AGENTS.md` block). `update` and `as` already
   run the `[auto]` steps. Show the `[ask ]` steps; on yes run `sunstack migrate --apply all`,
   then show `git diff` of `sunstack/` and `AGENTS.md`. For every team on this machine at once:
   `sunstack migrate --host` (add `--scan <folder>` to find teams not indexed yet).
3. Agents without a `board.md`: `sunstack tidy --all` creates them. Tell the user that each
   agent fills its board at its next save, and that the team objectives come from the user
   (the board skill helps set them). Older `context.md` files may still have `Current state`
   and `Threads` sections; the agent moves that content to its board at its next save.
4. Unnamed agents: every agent is now `<title>.<name>`. For each one, suggest a name from its
   role and context (`researcher` → `researcher.lead`), ask the user to confirm or type their
   own, then `sunstack rename <id> <title>.<name>`. A claimed agent must be released first;
   skip it and say so.
5. Run `sunstack health` again and show that the `migrate` lines are gone; suggest committing
   `sunstack/`.

Old IDs keep working until they are renamed, so the user may skip migration.

How to do each kind of step:

- **no sunstack/ here**: confirm the folder first (`pwd`): `sunstack init` creates `sunstack/`,
  an `AGENTS.md` block and a `.gitignore` line right here. On yes, run `sunstack init`.
- **PROTOCOL.md or AGENTS.md block is outdated**: `sunstack init --refresh`, then show the
  `git diff` of what changed.
- **sunstack/TEAM is missing** (a team from before v0.8): `sunstack init` adds it. It gives the
  team one ID on every host; commit it.
- **install or rules missing or outdated**: `sunstack install` or `sunstack update`, as health
  says. They may ask their own questions in the terminal; if so, give the user the command to
  run themselves.
- **leftover lock, staging files, inbox of a fired agent, unfinished message**: show the exact
  path and what is in it (`ls -la`), then remove only that path on yes. Never remove a lock
  while a sunstack command may be running.
- **claim from a closed pane**: offer `sunstack release <id_task> --stale` first, which drops
  claims whose tmux pane is confirmed closed. Otherwise offer `/sunstack:as <id>` (Codex:
  `$sunstack:as <id>`) with takeover, or leave it.
- **damaged claim file**: show its content; delete it only on yes.
- **merge conflict markers**: show the conflicting lines and help resolve them by hand.
- **uncommitted changes under sunstack/**: show `git status --short sunstack/` and the diff;
  commit only if the user asks.
- **context.md too long**: suggest taking on that agent and saving, which compacts it.
- **no agents hired yet**: run `sunstack library`, ask what the user needs, then follow the
  hire skill (`/sunstack:hire`, Codex: `$sunstack:hire`).
- **board lines** (blocked, stale, overdue, not aligned, no objective): summarize them and
  hand over to the board skill.
- **rule changes waiting for approval / unread messages**: suggest taking on that agent
  (`/sunstack:as <id>`, Codex: `$sunstack:as <id>`); its save step asks about each proposal.

When the user wants a live view instead, point them to `sunstack tui` in a terminal. For every
team and session on this machine, hand over to the org skill.

Only if the user asks to remove Sunstack from Claude Code or Codex: `sunstack uninstall`
removes the plugin and its permission rules (it asks in the terminal, or needs `--yes`). It
leaves every `sunstack/` folder alone.
