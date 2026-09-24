---
name: board
description: Show and align the Sunstack team's objectives and key results (OKRs) across the user and every agent, with dependencies, stale or overdue entries and directives not yet followed, and help fix what is off. Use when the user says "board", "sunstack board", "/sunstack:board", "OKR", "objectives", "what is everyone working on", "who is blocked", "align the team", or asks to set or change team objectives.
---

# Sunstack: board

The team board (`sunstack/BOARD.md`) holds the user's objectives, the user's own key results
and the user's directives. Each agent's `board.md` holds its key results in Now / Next / Done,
each serving an objective, with `needs:` pointing at what it waits on. Every entry starts with
the date it last changed.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer or a cancel is not approval.
- Objectives and the user's key results change only with the user's explicit approval of the
  exact wording, through `sunstack amend --team BOARD.md`. An agent's own board is written
  only by a session working as that agent (the save skill).

## 1. Look

```sh
sunstack board
```

Add an ID (`sunstack board builder.alice`) to see one agent's whole board.

## 2. Report

Summarize for the user, short:

- each objective and how its key results stand (who is on what, what is done);
- **Needs attention**, grouped: blocked (waiting on someone), broken (needs something that
  does not exist, or serves no objective), stale (Now entry not updated for a week), overdue,
  undated, and directives an agent has not aligned with yet;
- where the user is the blocker (`user#KR<n>`, or an agent waiting on `user`).

## 3. Offer next steps

Pick what fits and ask:

- **No objectives yet**: ask the user for one to three outcomes, draft them as
  `- <today> O<n> <outcome> (due: <date>)`, show the draft, and on approve write them (step 4).
- **Change an objective or the user's own key results**: draft the exact new lines, show
  before and after, approve / edit / cancel, then step 4.
- **Give the team a direction**: hand over to the direct skill.
- **An agent's board is stale, broken or not aligned**: suggest taking on that agent
  (`/sunstack:as <id>`, Codex `$sunstack:as <id>`) and saving; if this session already is that
  agent, run the save skill now.
- **Dependencies disagree** (A needs B#KR2, but B has no such key result): tell the user and
  suggest which agent should add or fix it.
- **Old finished entries pile up**: `sunstack tidy --all` archives them by month.

## 4. Write objectives (after approval)

```sh
sunstack snapshot --team BOARD.md
```

Write the complete new `BOARD.md` into `candidate_dir` under a new name, changing only the
approved lines (keep Directives exactly as they are), then:

```sh
sunstack amend --team BOARD.md "<candidate file>" "<checksum>" --summary "<one line>"
```

On exit 3 `mismatch`, snapshot again, redo the change on the new content, show it again if it
differs, and retry.
