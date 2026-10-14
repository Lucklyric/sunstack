---
name: next
description: Rank the best next actions for this Sunstack team across setup, what waits on the user, blocked work, board drift and this session's work, then hand the top one to its skill. Use for "sunstack next", "/sunstack:next", "what should I do next", "what needs doing on the team", "what needs me". In a team, that team; elsewhere, every team on this host and the org's other hosts.
---

# Sunstack: next

## Rules

- Outside a team (no `sunstack/PROTOCOL.md` in this folder or above), `sunstack next` lists
  what waits on the user across every team on this host and the org's other hosts; there is no
  `[yours]` entry.
- Run every `sunstack` command on its own, with nothing chained after it.
- Act on one item per yes. Never run an approval-gated command (`amend`, `hire`, `rename`,
  `fire`, `kill`, `direct`, `answer`, `halt`) without the user's explicit choice for that item.

## 1. Look

```sh
sunstack next
```

Add `--root "<dir>"` for another team, and `--all` when the user wants every item. It prints
the top five, ranked, each with a `do:` line:

- `[broken]`: a failed check, a halted team, or this host's missing permission rules
- `[user]`: waits on the user: asks, approvals, migration, decisions, sessions at a prompt,
  a newer release, mail held on the hub
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
- migration, install, health fixes, `sunstack update`: the checkup skill
- `sunstack attach ...`, `sunstack hub held`, another host: the org skill
- `sunstack tasks`, blocked work, a session to reach: the org skill, or the message skill for
  a question to the owner
- drift on boards (one line in the short list; `--all` lists them): the board skill's align step
- a halt: tell the user what the halt says; only the user runs `sunstack halt --off`
- `[yours]`: carry on with that entry as this agent; save when it moves

After the item is done, run `sunstack next` again and offer the new first item.
