---
name: save
description: Save what the current Sunstack agent learned into its persistent context, and ask the user to approve any change to its pillars or AGENT.md. Use when the user asks to save Sunstack context ("save" in a session holding a Sunstack identity, "sunstack save", "/sunstack:save"), asks to change a Sunstack pillar or agent rule, before ending or switching a Sunstack identity, or after meaningful work as a Sunstack agent.
---

# Sunstack: save

Two kinds of change, handled differently:

- **Experience** (`context.md`, threads) is written automatically, through a snapshot and a
  checked commit so a concurrent writer is never overwritten.
- **Rules** (pillars, `AGENT.md`) change only with the user's approval, every time. This is how
  the agent improves itself, and the user must see each step.

## 0. Who am I, and the rules

- You need the root, id and token from the `session state` block that `sunstack as` printed in
  this session. If you do not have them (for example after `/clear`), stop and ask the user to
  run the as skill again. Never guess.
- Run every `sunstack` command on its own, with nothing chained after it.
- If a `sunstack` command exits non-zero in a way not handled below, show the error and stop.
  Do not go on to release or to removing a thread.

## 1. Decide what to write (three-layer check)

- **context**: new decisions with reasons, pitfalls, current state, open questions, and
  processed messages that had real effects. Not a log of steps, not what git or the code
  already shows. Write these without asking, unless an entry reverses an earlier decision,
  changes or removes someone else's entry, or compaction would drop content: then ask first.
- **pillars**: did this work show a pillar is outdated, conflicting, or missing? That is a rule
  change: go to step 2.
- **AGENT.md**: should this agent's way of working change for this project? Also a rule
  change: step 2.

The user can also ask directly to change a pillar or this agent's `AGENT.md`; handle it in
step 2 the same way. If nothing is worth keeping, say so and stop.

## 2. Rule changes: ask the user now

For each rule change, and for any still-pending line under `## 提议` in context.md:

1. Snapshot the target (no token needed):
   `sunstack snapshot --root "<root>" "<id>" pillars.md`, or `AGENT.md`, or
   `sunstack snapshot --root "<root>" --team` for the team `PILLARS.md`.
2. Write the complete new file directly into `candidate_dir`, changing only what is proposed.
   Pillars stay one dated, checkable line each. `AGENT.md` keeps its frontmatter.
3. Show the user the change: the file, the lines before and after, and why.
4. Get the approval:
   - **Claude Code**: running `sunstack amend` makes Claude Code show its own approval prompt,
     and that prompt is the approval. Tell the user in one line: approve the prompt to apply
     it, or deny it to edit, reject or postpone. Then run amend.
   - **Codex, or anywhere without that prompt**: ask first, with numbered options (approve /
     edit / reject / later), and wait. On approve, run amend; Codex may prompt again.

   ```sh
   sunstack amend --root "<root>" "<id>" pillars.md "<candidate file>" "<checksum>" --summary "<one line: what changes>"
   ```

   For the team file: `sunstack amend --root "<root>" --team "<candidate file>" "<checksum>" --summary "..."`.
5. Record the outcome in context (step 3 writes it):
   - **approved** (exit 0): remove it from `## 提议`, add under `## 决策`: `<date> 用户批准：<change>`
   - **rejected**: remove it from `## 提议`, add under `## 决策`: `<date> 用户否决：<change>，<reason if given>`.
     Never propose the same change again.
   - **later**, or no answer: keep it under `## 提议`; it is asked again at the next save or as.
   - **edit**: rewrite the candidate with the user's wording and show it again.
   - **exit 3 `mismatch`**: the file changed; snapshot again, redo the candidate, ask again.

Never run amend without first showing the change. Change another agent's rules only when the
user asks for it.

## 3. Snapshot the context

```sh
sunstack snapshot --root "<root>" "<id>" context.md --token "<token>"
```

Use `threads/<topic>.md` instead of `context.md` for a separate line of work. The output gives
`checksum`, `candidate_dir`, and the current content.

## 4. Write the candidate

Write the complete new file directly into `candidate_dir`, with no subfolder: for example
`<candidate_dir>/context.md`, or `<candidate_dir>/<topic>.md` for a thread. Start from the
snapshot content: keep everyone else's lines exactly as they are, keep the fixed sections, one
dated line per entry. Your own intended edits (updating state, recording step 2 outcomes,
compacting) are allowed.

## 5. Commit

```sh
sunstack commit --root "<root>" "<id>" context.md "<candidate file>" "<checksum>" --token "<token>"
```

- **0**: done. Tell the user in one line what was saved, and what rule changes were approved,
  rejected or postponed.
- **3 `mismatch`**: someone changed the file. The output has the fresh content and checksum.
  Merge your additions onto it (if your entry contradicts theirs, keep both and mark yours as
  to be confirmed), rewrite the candidate, and commit again with the new checksum. Give up
  after three rounds and tell the user.
- **4 `busy`**: retry once. If it persists, tell the user (a leftover lock).
- **4 `token`**: this session no longer holds the ID. Stop and tell the user.
- **1 / 2**: show the error. Do not work around it.

A thread is finished: first commit its conclusion into `context.md`, then take a fresh
snapshot of the thread and remove it with
`sunstack commit --root "<root>" "<id>" threads/<topic>.md --delete "<checksum>" --token "<token>"`.
