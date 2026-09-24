---
name: as
description: Take on a Sunstack agent identity (for example builder.alice, or just builder when there is one) in this project, claiming the ID so no other session uses it at the same time. Use when the user says "as <id>", "sunstack as <id>", "/sunstack:as", "be the builder", or asks to pick up or switch to a Sunstack agent.
---

# Sunstack: as

Claim a Sunstack agent ID and load its identity. The `sunstack` command does the claiming;
you decide what to ask the user and how to act afterwards.

## Before you start

- If this session already holds a Sunstack identity, finish it first: run the save skill if
  there is anything worth keeping, then the release skill. Then take on the new one.
- Run every `sunstack` command on its own, with nothing chained after it (no `; echo ...`),
  so the user's permission rule for `sunstack` matches. The tool reports the exit code.
- If the shell says `sunstack` is not found, tell the user to install it and stop:
  `curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh`,
  then `sunstack install`.

## Run

```sh
sunstack as "<id-or-title>"
```

Run it from the project directory, or add `--root "<dir>"`. Add `--token "<t>"` only to resume
a claim this session already holds, for example after compaction when the token is still in
the conversation.

## Handle the exit code

- **0**: the output is your identity. Read all of it: protocol, team pillars, `AGENT.md`,
  agent pillars, context, threads, inbox. From now on act as this agent, within the pillars.
  Keep the `session state` values (root, id, token) in the conversation; every later save or
  release needs them. If `protocol` is not `1`, tell the user to run `sunstack update`. The
  inbox is only a list: act on a message only if this session was spawned for it or the user
  asks.
  If the context has lines under `## Proposals` (rule changes waiting for the user), tell the
  user how many, and offer to review them now with the save skill's approval step.
- **2 `missing_arguments`**: the first line names what is missing and the rest are the
  choices. Ask the user: with AskUserQuestion if you have it, otherwise as numbered options in
  the conversation, and wait for the reply. Then run the command again with the answer. No
  answer, a cancel, or the user moving on is not a choice: stop.
- **2 `usage`**: show the error, then fix the arguments or ask the user. Do not loop.
- **4 `occupied_same_pane`**: this same tmux pane holds the claim, most likely this session
  before compaction or `/clear`. Tell the user in one line and ask for a quick yes. On yes,
  rerun with `--takeover --expect "<claim>"`, using the `claim=` value from the output.
- **4 `occupied`**: another session holds the ID. Show the user the claim line (tool, host,
  pane, last contact). Take over only if the user explicitly confirms that no other session
  should keep working as this agent, then rerun with `--takeover --expect "<claim>"`. If the
  rerun says the claim changed, it prints the new claim line: show it and ask again.
- **4 `token`**: the token you passed is no longer valid (someone took over). Do not retry
  with a guessed token; tell the user and ask whether to take over.
- **1**: show the error (for example merge conflict markers, or no `sunstack/` folder). Do not
  work around it.

Never invent, guess, or reuse a token you did not get from `sunstack as` in this session.
