---
name: board
description: Show and align the Sunstack team's objectives and key results across the user and every agent, with dependencies, stale or overdue entries and unfollowed directives. Use for "sunstack board", "/sunstack:board", "the team's OKRs", "the team's dependencies", "align the team". Not the ranked to-do list (next skill).
---

# Sunstack: board

Command: `sunstack board` (also `answer`, `amend --team BOARD.md`). Skill: `/sunstack:board` in Claude Code, `$sunstack:board` in Codex.

The team board (`sunstack/BOARD.md`) holds the user's objectives, the user's own key results
and the user's directives. Each agent's `board.md` holds its key results in Now / Next / Done,
each serving an objective, with `needs:` pointing at what it waits on. Every entry starts with
the date it last changed.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the health skill sets one up.
- Act only on this folder's team (its sessions on other hosts included). Work for a different
  team is delegated: send it as a task (send skill) to its agent that owns that work, else
  to its lead (its `pm` agent; none: ask the user), as `<team>/<id>` or `<host>:<team>/<id>`,
  and stop. Hiring, firing, spawning, dismissing or directing in a different team yourself
  needs the user's explicit yes first.
- Run every `sunstack` command on its own, with nothing chained after it.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer or a cancel is not approval.
- Objectives and the user's key results change only with the user's explicit approval of the
  exact wording, through `sunstack amend --team BOARD.md`. An agent's own board is written
  only by a session working as that agent (the save skill), or by `sunstack tidy`.

## 1. Look

```sh
sunstack board
```

Add an ID (`sunstack board builder.alice`) to see one agent's whole board.

## 2. Report

Summarize for the user, short:

- each objective and how its key results stand (who is on what, what is done);
- **Needs attention**: the board prints only a count; `sunstack next --all` lists the items,
  ranked. Group them: blocked (waiting on someone), broken (needs something that does not
  exist, or serves no objective), stale (Now entry not updated for a week), overdue, undated,
  and directives an agent has not aligned with yet;
- where the user is the blocker (`user#KR<n>`, or an agent waiting on `user`);
- **Asks** (`<id>#Q<n> asks the user: ...`): each question with its options and default, so the
  user can answer them in one pass. Answer with
  `sunstack answer "<id>" Q<n> "<answer>"` (add `--root "<team root>"` for another team) with
  the user's exact choice once they have chosen (never choose for them); asks decided by
  default stay listed until archived, so the user can overrule them.

## 3. Offer next steps

For one ranked list across the whole team (setup, asks, blocked work, drift), use the next
skill. Here, pick what fits the board and ask:

- **No objectives yet**: ask the user for one to three outcomes, draft them as
  `- <today> O<n> <outcome> (due: <date>)`, show the draft, and on approve write them (step 4).
- **Change an objective or the user's own key results**: draft the exact new lines, show
  before and after, approve / edit / cancel, then step 4.
- **Give the team a direction**: hand over to the direct skill.
- **Agents' boards are stale, broken or not aligned**: offer to align the team (step 5). If
  this session already is one of those agents, run the save skill for it now.
- **Dependencies disagree** (A needs B#KR2, but B has no such key result): tell the user and
  suggest which agent should add or fix it.
- **Old finished entries pile up**: `sunstack tidy --all` archives them by month.

## 4. Write objectives (after approval)

Follow step 2 of the save skill (snapshot, candidate, show, approve, amend) with the team
`BOARD.md`: `sunstack snapshot --team BOARD.md`, then
`sunstack amend --team BOARD.md "<candidate file>" "<checksum>" --summary "<one line>"`.
Change only the approved lines and keep Directives exactly as they are.

## 5. Align the team

When the user asks to align the team ("sunstack align"), or accepts the offer in step 3. Each
agent's board is written only by that agent, so this step asks each agent to fix its own.

1. From `sunstack next --all`, group what needs attention by agent: directives it has
   not aligned with, key results with no or an unknown objective, `needs:` pointing at a key
   result that does not exist, stale Now entries, overdue and undated entries, Done entries
   without a `verified:` line, asks without a default, asks past their default date, tasks
   open too long, and task chains that failed twice. A broken need
   (A needs B#KR2, B has none) goes to whichever of the two should change; if unclear, ask.
   Leave out what only the user can fix (objectives, the user's own key results) and list it
   separately for the user.
2. Show the user one line per agent with what it should fix, for example
   `builder.alice: align with D3; KR2 serves no objective; KR4 not updated since 2026-09-12`.
   Ask: send all / pick / cancel. No answer or a cancel is not approval.
3. For each approved agent, one message:

   ```sh
   sunstack send "<id>" "Align your board: <the fixes from the line above>. Update it at your next save and set aligned: to the last directive checked." --type fyi --op "align-<today>-<id>"
   ```

   Add `--from "<id>" --token "<token>"` only if this session holds an agent and the request
   is that agent's own; otherwise the message goes in the user's name. A live session gets a
   nudge; others see it when they next start (`/sunstack:as <id>`, Codex: `$sunstack:as <id>`), or start one with the spawn
   skill if the user wants it done now.
4. Tell the user who was asked, who was nudged and who waits, and that `sunstack next` shows
   what is still off once they have saved.
