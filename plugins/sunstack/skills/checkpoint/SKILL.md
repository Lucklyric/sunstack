---
name: checkpoint
description: Give a quick progress report on the work this session is doing in this folder (what is done, what is in progress, the next steps, and what waits on the user) from git, the project's status docs, any Sunstack team board, this session's messages and the conversation. Read-only. Works in any Claude Code or Codex session, with or without a Sunstack team. Use for "sunstack checkpoint", "/sunstack:checkpoint", "checkpoint", "progress report", "where are we", "what's done and what's next", "status of this project", "after compacting, where were we", "进度", "汇报", "进展". Not the ranked to-do list (next skill), the team's OKRs (board skill) or every team (org skill), and not Claude Code's own rewind checkpoints.
---

# Sunstack: checkpoint

Command: `sunstack checkpoint`. Skill: `/sunstack:checkpoint` in Claude Code, `$sunstack:checkpoint` in Codex.

A short report the user can read in a minute. It changes nothing. The wrap skill uses the same
inventory to save what is not written down yet, before a compaction.

## Rules

- Read-only: write, commit or send nothing.
- Run every `sunstack` command on its own, with nothing chained after it.
- Every line comes from a source you read. Never invent progress, and never count work as done
  without its evidence (a commit, a passing check, the user's yes).
- No `checkpoint` command (an older binary: exit 2 with an unknown command): read git
  (`git log`, `git status`) and the docs below yourself and say the CLI part is missing.

## 1. Read

```sh
sunstack checkpoint
```

It prints, as far as each exists: the last wrap note for this folder (its age and whether this
session wrote it), git for this folder and each checked-out submodule under it (commits since
the note, uncommitted files), the Sunstack team's Now entries, `next`, and this session's
messages. Use `--root <team folder>` for a team elsewhere. Outside a team, `next` also lists
host-wide items (the issue log, the install, other teams): leave those out of the report.

Then read, briefly:

- the docs the wrap note says to read first;
- the progress or status notes the project's `AGENTS.md` or `CLAUDE.md` names (a plan,
  a changelog, a design doc's latest entry);
- this conversation: what was asked, decided and left open.

## 2. Report

Four parts, about 15 lines in all, each line tagged with its source (`git`, `board`, the doc,
`conversation`):

- **Done**, since the wrap note (or since this session started), with its evidence.
- **In progress**: where each stands.
- **Next**: the top few steps, in order.
- **Waiting on you**: questions, approvals and answers the user owes.

The wrap note is as of its save time: where git, the board or a doc says otherwise, they win,
and an item the note lists as open but later work finished is reported as done.

Mark a line `not written down` when it exists only in the conversation and in no file. When
there are such lines, end with one line offering the wrap skill to save them.
