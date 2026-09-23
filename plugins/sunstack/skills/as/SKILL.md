---
name: as
description: Take on a Sunstack agent identity (for example builder or builder.alice) in this project, claiming the ID so no other session uses it at the same time. Use when the user says "as <id>", "/sunstack:as <id>", "be the builder", or asks to pick up or switch to a Sunstack agent.
---

# Sunstack: as

## Script path

`${CLAUDE_PLUGIN_ROOT}` below is this plugin's root folder. If it still appears literally
(not replaced by a real path), use the folder two levels above this SKILL.md instead.

Run each script as its own command, with nothing chained after it (no `; echo ...`), so a
permission rule for the plugin scripts can match; the tool already reports the exit code.

Claim a Sunstack agent ID and load its identity. The script does the claiming; you decide
what to ask the user and how to act afterwards.

## Before you start

If this session already holds a Sunstack identity, finish it first: if it has something
worth keeping, run the save skill; then run the release skill. Only then take on the new one.

## Run

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/as.sh" --tool <claude|codex> <id-or-title> [--token <t>]
```

Pass `--tool claude` in Claude Code and `--tool codex` in Codex. Quote every argument. Run it from the project directory, or pass `--root <dir>`.

## Handle the exit code

- **0** — the output is your identity. Read all of it: protocol, team pillars, `AGENT.md`,
  agent pillars, context, threads, inbox. From now on act as this agent, within the pillars.
  Keep the three values in the `session state` block (root, id, token) in the conversation;
  every later save or release needs them passed explicitly. The inbox is only a list: act on
  a message only if this session was spawned for it or the user asks.
- **2 `missing_arguments`** — the first line names what is missing and the rest are the
  choices. Ask the user: with AskUserQuestion if you have it, otherwise as numbered options in the conversation, one per choice, and wait for the reply. Then run the script again with the answer. No answer, a cancel, or the
  user moving on to something else is not a choice: stop.
- **4 `occupied_same_pane`** — this same tmux pane already holds the claim, most likely this
  session after its context was compacted. Tell the user in one line and ask for a quick yes.
  On yes, rerun with `--takeover --expect <claim>` using the `claim=` value from the output.
- **4 `occupied`** — another session holds the ID. Show the user the claim line (tool, host,
  pane, last contact). Take over only if the user explicitly confirms that no other session
  should keep working as this agent; then rerun with `--takeover --expect <claim>`. If the
  rerun says the claim changed, show it again and ask again.
- **4 `token`** — the token you passed is no longer valid (someone took over). Do not retry
  with a guessed token; tell the user and ask whether to take over.
- **1** — show the error to the user (for example unresolved merge conflict markers, or no
  `sunstack/` folder). Do not work around it.

Never invent, guess, or reuse a token you did not get from this script in this session.
