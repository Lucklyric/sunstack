---
name: save
description: Save what the current Sunstack agent is doing and has learned into its board and persistent context, archive what is outdated, and ask the user to approve any change to its pillars, AGENT.md or the team objectives. Use when the user asks to save Sunstack context ("save" in a session holding a Sunstack identity, "sunstack save", "/sunstack:save"), asks to change a Sunstack pillar or agent rule, before ending or switching a Sunstack identity, or after meaningful work as a Sunstack agent.
---

# Sunstack: save

Two kinds of change, handled differently:

- **Plan and experience** (`board.md`, `context.md`, threads, archive) are written
  automatically, through a snapshot and a checked commit so a concurrent writer (another
  session working as the same agent, or another tool) is never overwritten.
- **Rules and objectives** (pillars, `AGENT.md`, objectives and the user's key results in
  `BOARD.md`) change only with the user's approval, every time. This is how the agent improves
  itself, and the user must see each step.

Every entry you write starts with today's date: `- <YYYY-MM-DD> ...`.

## 0. Who am I, and the rules

- You need the root, id and token from the `session state` block that `sunstack as` printed in
  this session. If you do not have them (for example after `/clear`), stop and ask the user to
  run the as skill again. Never guess.
- Run every `sunstack` command on its own, with nothing chained after it.
- If a `sunstack` command exits non-zero in a way not handled below, show the error and stop.
  Do not go on to release or to removing a thread.

## 1. Decide what to write

- **board**: which key results this session moved. Update their line and date, move finished
  ones to Done with today's date, add new ones under Now or Next, each tagged with the team
  objective it serves (`[O<n>]`) and its `needs:`. If a directive newer than `aligned:` is
  addressed to this agent, make the board follow it and set `aligned:` to the last one checked.
  Leave other sessions' Now entries as they are. A key result that fits no objective goes to
  Open questions in context, for the user.
- **context**: new decisions with reasons, pitfalls, open questions, and processed messages
  that had real effects. Not a log of steps, not what git or the code already shows. Write
  these without asking, unless an entry reverses an earlier decision, changes or removes
  someone else's entry, or would drop content: then ask first.
- **archive**: entries in context that are no longer true or useful (superseded decisions,
  fixed pitfalls, answered questions) move to `archive/<YYYY-MM>.md` of the month they were
  written. Move, never just delete.
- **objectives**: if the work shows a team objective or one of the user's key results is
  wrong or missing, that is a proposal to the user: step 2, with the team `BOARD.md`.
- **pillars**: did this work show a pillar is outdated, conflicting, or missing? That is a rule
  change: go to step 2.
- **AGENT.md**: should this agent's way of working change for this project? Also a rule
  change: step 2.

The user can also ask directly to change a pillar or this agent's `AGENT.md`; handle it in
step 2 the same way. If nothing is worth keeping, say so and stop.

## 2. Rule changes: ask the user now

For each rule change, and for any still-pending line under `## Proposals` in context.md:

1. Snapshot the target (no token needed):
   `sunstack snapshot --root "<root>" "<id>" pillars.md`, or `AGENT.md`, or
   `sunstack snapshot --root "<root>" --team` for the team `PILLARS.md`, or
   `sunstack snapshot --root "<root>" --team BOARD.md` for objectives and the user's key results.
2. Write the complete new file directly into `candidate_dir` under a name used only for this
   proposal (for example `pillars-<time>.md`), changing only what is proposed. Pillars stay one
   dated, checkable line each. `AGENT.md` keeps its frontmatter. Use that exact file for amend,
   and do not rewrite it after the user has seen it; an edit means a new file and a new showing.
3. Show the user the change: the file, the lines before and after, and why. Do this even when
   the user asked for the change: a request is not approval of your exact wording.
4. Ask: approve / edit / reject / later (with AskUserQuestion if you have it, otherwise as
   numbered options), end your turn, and wait for the user's reply. Run amend only after an
   explicit approve in that reply. Claude Code or Codex may then show their own prompt for the
   command, or an automatic reviewer may decide it; either way, the user's reply to your
   question is the approval that counts.

   ```sh
   sunstack amend --root "<root>" "<id>" pillars.md "<candidate file>" "<checksum>" --summary "<one line: what changes>"
   ```

   For the team files: `sunstack amend --root "<root>" --team "<candidate file>" "<checksum>" --summary "..."`
   (PILLARS.md), or `sunstack amend --root "<root>" --team BOARD.md "<candidate file>" "<checksum>" --summary "..."`.
5. Record the outcome in context (step 3 writes it):
   - **approved** (exit 0): remove it from `## Proposals`, add under `## Decisions`:
     `- <date> User approved: <change>`
   - **rejected**: remove it from `## Proposals`, add under `## Decisions`:
     `- <date> User rejected: <change> — <reason if given>`. Never propose the same change again.
   - **later**, or no answer: keep it under `## Proposals`; it is asked again at the next save or as.
   - **edit**: rewrite the candidate with the user's wording and show it again.
   - **exit 3 `mismatch`**: the file changed; snapshot again, redo the candidate, ask again.

Never run amend without first showing the change. Change another agent's rules only when the
user asks for it.

## 3. Snapshot each file you will write

```sh
sunstack snapshot --root "<root>" "<id>" board.md --token "<token>"
```

Then the same for `context.md`, and for `threads/<topic>.md` or `archive/<YYYY-MM>.md` when
you write them. Each output gives `checksum`, `candidate_dir` (this session's own folder), and
the current content. When moving entries to the archive, commit the archive first, then the
file they left, so an entry is never lost in between.

## 4. Write the candidate

Write the complete new file directly into `candidate_dir`, with no subfolder: for example
`<candidate_dir>/board.md`, `<candidate_dir>/context.md`, or `<candidate_dir>/<topic>.md` for
a thread. Start from the snapshot content: keep everyone else's lines exactly as they are, keep
the fixed sections, one dated line per entry. Your own intended edits (updating your key
results, recording step 2 outcomes, moving entries to the archive) are allowed.

## 5. Commit

```sh
sunstack commit --root "<root>" "<id>" board.md "<candidate file>" "<checksum>" --token "<token>"
```

Commit each file you snapshotted (board, context, thread, archive) the same way.

- **0**: done. Tell the user in one line what was saved (key results moved, entries archived),
  and what rule changes were approved, rejected or postponed.
- **3 `mismatch`**: someone changed the file, often another session working as this agent.
  The output has the fresh content and checksum. Merge your changes onto it: keep every other
  entry as it is and add or update only yours. If your entry contradicts one written by
  someone else, do not pick one: keep both, add an Open question in context, and tell the
  user. Rewrite the candidate and commit again with the new checksum. Give up after three
  rounds and tell the user.
- **4 `busy`**: retry once. If it persists, tell the user (a leftover lock).
- **4 `token`**: this session no longer holds the ID. Stop and tell the user.
- **1 / 2**: show the error. Do not work around it.

A thread is finished: snapshot it, commit its conclusion into `context.md`, then remove it with
the checksum of that same snapshot:
`sunstack commit --root "<root>" "<id>" threads/<topic>.md --delete "<checksum>" --token "<token>"`.
If this says `mismatch`, the thread changed after you summarized it: fold the new content into
context first, then delete against the new checksum.

## 6. Tidy

Finally run `sunstack tidy "<id>" --root "<root>"`: it moves Done entries older than a week
to the archive. If the board is still longer than about 40 lines, shorten Next.
