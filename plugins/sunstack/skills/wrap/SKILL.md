---
name: wrap
description: Before a compaction, /clear or the end of a session, write this session's progress back into the project's docs and leave a resume note, so the next session (or this one after compacting) gets the latest state back in a minute. Works in any Claude Code or Codex session, with or without a Sunstack team. Use for "sunstack wrap", "/sunstack:wrap", "wrap up before compacting", "before I compact", "prepare for compaction", "we are about to compact", "update the docs before compact", "save everything before compacting", "压缩前", "compact 之前更新文档". Not the save skill, which saves one Sunstack agent's board and context (wrap runs it when the session holds an agent).
---

# Sunstack: wrap

Skill only: `/sunstack:wrap` in Claude Code, `$sunstack:wrap` in Codex. It runs `sunstack checkpoint`.

Run it on purpose before `/compact` or `/clear`: a hook fires only once compaction has started,
too late for this. After the compaction, the SessionStart hook says a wrap note exists, and
`sunstack checkpoint` (or the checkpoint skill) prints it with the current state.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- Update existing docs only. Create none, except the wrap note.
- Never edit an agreed `## Expected results` section (verify skill).
- Follow the project's own commit rules. Commit nothing yourself: list what is left to commit.
- Write only what happened in this session, from its sources. Label a claim measured (with its
  evidence), inferred or a guess.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait.
- No `checkpoint` command (an older binary): do steps 1 to 4 from git and the conversation,
  then show the wrap note in the conversation instead of saving it, and say so.

## 1. Inventory

Do the checkpoint skill's step 1 (`sunstack checkpoint` and the reading). List what this
session did and left open: changes and commits, decisions, findings, open items with their next
step, background jobs and their output files, questions waiting on the user.

## 2. Find where each item belongs

Without guessing:

- the docs this session read or edited (a design note, a spec, a plan, a changelog);
- the progress or status notes the project's `AGENTS.md` or `CLAUDE.md` names;
- the save skill, when this session holds a Sunstack agent (its board and context);
- the run map, when a verification changed what it describes (verify skill).

An item with no home stays only in the wrap note.

## 3. Confirm once

Show a list of `item → file` (what will be written where), and ask: proceed / edit / cancel.

## 4. Write

Update each file in its own style. Each open item says where it stands and the next step, so a
fresh session can continue from the file alone. When the session holds an agent, run the save
skill for its part.

## 5. Leave the wrap note

Write the note to a file in this session's scratch or temporary folder, then:

```sh
sunstack checkpoint --save "<note file>"
```

It stores the note for this folder and session (host-local, replacing the last one) and stamps
it with the time and each repo's commit. Keep it short:

- **Read first:** the doc paths, most useful first.
- **Open:** each open item, where it stands, its next step.
- **Running:** background jobs and their output files.
- **Waiting on the user:** questions and approvals.
- **Not committed:** files changed and not committed, and why.

Exit 2 `no_session`: this CLI gave no session ID. Show the note in the conversation instead.

## 6. Report

One line per file updated, the note's path, what is left to commit, and: "Ready to compact.
After it, run `sunstack checkpoint` (or the checkpoint skill)."
