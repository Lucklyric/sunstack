---
name: next
description: Decide the best next action for this Sunstack team from its current state, one ranked list across setup, what waits on the user, blocked work, board drift and this session's own work, then offer the top item and hand over to the skill that does it. Use when the user asks "sunstack next", "/sunstack:next", "what should I do next", "what's the best next step", "analyze the current status", "what needs doing on the team", or comes back to a project and wants to pick up. Only for projects with a Sunstack team (a sunstack/ folder).
---

# Sunstack: next

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so
  in one line and offer the checkup skill, which sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Act on one item per yes. Never run an approval-gated command (`amend`, `hire`, `rename`,
  `fire`, `kill`, `direct`, `answer`, `halt`) without the user's explicit choice for that item.

## 1. Look

```sh
sunstack next
```

Add `--root "<dir>"` for another team, and `--all` when the user wants every item. It prints
the top five, ranked, each with a `do:` line:

- `[broken]`: a failed check or a halted team; nothing else is safe until it is fixed
- `[user]`: waits on the user: asks, approvals, migration, decisions
- `[blocked]`: work waiting on another agent, late key results, tasks open too long
- `[drift]`: boards to fix at the owner's next save: unaligned, unverified, stale, untidy
- `[yours]`: this session's own next board entry, when it holds an agent

## 2. Report

Give the list in plain words, one line each, in its order. Group several asks from the same
agent into one line. Say what the user gains by doing the first one.

## 3. Offer the first item

Ask: do it / pick another / stop. On yes, hand over to the skill that owns it, by its `do:`:

- `sunstack answer ...`, asks, approvals of rule changes: the board skill (asks) or the save
  skill (Proposals, run by the agent that owns them)
- migration, install, health fixes: the checkup skill
- `sunstack tasks`, blocked work, a session to reach: the org skill, or the message skill for
  a question to the owner
- drift on boards: the board skill's align step
- a halt: tell the user what the halt says; only the user runs `sunstack halt --off`
- `[yours]`: carry on with that entry as this agent; save when it moves

After the item is done, run `sunstack next` again and offer the new first item.
