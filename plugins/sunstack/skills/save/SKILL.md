---
name: save
description: Save what the current Sunstack agent learned into its persistent context, safely against other writers. Use when the user asks to save Sunstack context ("save" in a session holding a Sunstack identity, "sunstack save", "/sunstack:save"), before ending or switching a Sunstack identity, or after meaningful work as a Sunstack agent.
---

# Sunstack: save

Write this agent's new knowledge into `context.md` (or a thread) through a snapshot and a
checked commit, so a concurrent writer is never overwritten.

## 0. Who am I, and the rules

- You need the root, id and token from the `session state` block that `sunstack as` printed in
  this session. If you do not have them (for example after `/clear`), stop and ask the user to
  run the as skill again. Never guess.
- Run every `sunstack` command on its own, with nothing chained after it.
- If `snapshot` or `commit` exits non-zero in a way not handled below, show the error and stop.
  Do not go on to release or to removing a thread.

## 1. Decide what to write (three-layer check)

- **context**: new decisions with reasons, pitfalls, current state, open questions, and
  processed messages that had real effects. Not a log of steps, not what git or the code
  already shows.
- **pillars**: did this work show a pillar is outdated, conflicting, or missing? Add a line
  under `## 提议` in context.md, tagged `[pillars]`, and tell the user.
- **AGENT.md**: should this agent's way of working change for this project? Add an `[agent]`
  proposal the same way and tell the user.

If nothing is worth keeping, say so and stop. Do not save for the sake of saving.

## 2. Snapshot

```sh
sunstack snapshot --root "<root>" "<id>" context.md --token "<token>"
```

Use `threads/<topic>.md` instead of `context.md` for a separate line of work. The output gives
`checksum`, `candidate_dir`, and the current content.

## 3. Write the candidate

Write the complete new file directly into `candidate_dir`, with no subfolder: for example
`<candidate_dir>/context.md`, or `<candidate_dir>/<topic>.md` for a thread. Start from the
snapshot content: keep everyone else's lines exactly as they are, keep the fixed sections, one
dated line per entry. Your own intended edits (updating state, removing your applied
proposals, compacting) are allowed.

## 4. Commit

```sh
sunstack commit --root "<root>" "<id>" context.md "<candidate file>" "<checksum>" --token "<token>"
```

- **0**: done. Tell the user in one line what was saved.
- **3 `mismatch`**: someone changed the file. The output has the fresh content and checksum.
  Merge your additions onto it (if your entry contradicts theirs, keep both and mark yours as
  to be confirmed), rewrite the candidate, and commit again with the new checksum. Give up
  after three rounds and tell the user.
- **4 `busy`**: retry once. If it persists, tell the user (a leftover lock; `sunstack health`
  reports it).
- **4 `token`**: this session no longer holds the ID. Stop and tell the user.
- **1 / 2**: show the error. Do not work around it.

A thread is finished: first commit its conclusion into `context.md`, then take a fresh
snapshot of the thread and remove it with
`sunstack commit --root "<root>" "<id>" threads/<topic>.md --delete "<checksum>" --token "<token>"`.
