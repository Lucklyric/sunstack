---
name: save
description: Save what this Sunstack agent learned into its persistent context, safely against other writers. Use when the user says "save", "/sunstack:save", before ending or switching a Sunstack identity, or after meaningful work as a Sunstack agent.
---

# Sunstack: save

## Script path

`${CLAUDE_PLUGIN_ROOT}` below is this plugin's root folder. If it still appears literally
(not replaced by a real path), use the folder two levels above this SKILL.md instead.

Run each script as its own command, with nothing chained after it (no `; echo ...`), so a
permission rule for the plugin scripts can match; the tool already reports the exit code.

Write this agent's new knowledge into `context.md` (or a thread) through a snapshot and a
checked commit, so a concurrent writer can never be overwritten.

## 0. Who am I

You need the root, id and token from the `session state` block printed by the as skill in
this session. If you do not have them (for example after compaction), stop and ask the user
to run as again. Never guess.

## 1. Decide what to write (three-layer check)

- **context** — new decisions with reasons, pitfalls, current state, open questions, and
  processed messages that had real effects. Not a log of steps, not what git or the code
  already shows.
- **pillars** — did this work show a pillar is outdated, conflicting, or missing? Add a line
  under `## 提议` tagged `[pillars]` and tell the user.
- **AGENT.md** — should this agent's way of working change for this project? Add a `[agent]`
  proposal and tell the user.

If nothing is worth keeping, say so and stop. Do not save for the sake of saving.

## 2. Snapshot

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/snapshot.sh" --root "<root>" "<id>" context.md --token "<token>"
```

Use `threads/<topic>.md` instead of `context.md` for a separate line of work. The output gives
`checksum`, `candidate_dir`, and the current content.

## 3. Write the candidate

Write the complete new file into `candidate_dir` (for example `<candidate_dir>/context.md`).
Start from the snapshot content: keep everyone else's lines exactly as they are, keep the fixed
sections, one dated line per entry. Your own intended edits (updating state, removing your
applied proposals, compacting) are allowed.

## 4. Commit

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/commit.sh" --root "<root>" "<id>" context.md "<candidate file>" "<checksum>" --token "<token>"
```

- **0** — done. Tell the user in one line what was saved.
- **3 `mismatch`** — someone changed the file. The output has the fresh content and checksum:
  merge your additions onto it, rewrite the candidate, commit again with the new checksum.
  Give up after three rounds and tell the user.
- **4 `busy`** — retry once; if it persists, tell the user (a leftover lock; health reports it).
- **4 `token`** — this session no longer holds the ID. Stop and tell the user.
- **1 / 2** — show the error. Do not work around it.

A thread is finished: first commit its conclusion into `context.md`, then remove it with
`commit.sh ... threads/<topic>.md --delete "<checksum>"` using a fresh snapshot checksum.
If any step fails, do not continue to the next one.
