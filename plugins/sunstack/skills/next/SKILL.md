---
name: next
description: Rank the best next actions for this Sunstack team across setup, what waits on the user, blocked work, board drift and this session's work, then hand the top one to its skill. Use for "sunstack next", "/sunstack:next", "what should I do next", "what needs doing on the team", "what needs me". By default it is local, so in a team it covers that team and outside a team only this session. "Across all teams" or "everything that needs me" is `sunstack org --attention`.
---

# Sunstack: next

## Rules

- Plain `sunstack next` is local to where this session runs. Choose the scope by what the user
  asked, not by guess:
  - Default, in a team (`sunstack/PROTOCOL.md` in this folder or above): that team's list.
  - Default, outside a team: only this session's own items (messages in its inbox, tasks it
    sent that have no reply yet), then one line counting the items elsewhere. No board is
    read, since the session holds no agent.
  - `--root "<dir>"`: that team's list, from anywhere.
  - `sunstack org --attention`: what waits on the user in every team on this host and the
    org's other hosts. Use it only when the user asks about all teams or everything.
  - `--all`: every item instead of the top five, for any of the above.
- This host's install problems, a newer release and mail held on the hub show in every scope.
- Run every `sunstack` command on its own, with nothing chained after it.
- Act on one item per yes. Never run an approval-gated command (`amend`, `hire`, `rename`,
  `fire`, `kill`, `direct`, `answer`, `halt`) without the user's explicit choice for that item.

## 1. Look

```sh
sunstack next
```

It prints the top five for the scope above, ranked, each with a `do:` line:

- `[broken]`: a failed check, a halted team, or this host's missing permission rules
- `[user]`: waits on the user: asks, approvals, migration, decisions, sessions at a prompt,
  a newer release, mail held on the hub
- `[blocked]`: work waiting on another agent, late key results, tasks open too long, and
  outside a team the tasks this session sent that have no reply yet
- `[drift]`: boards to fix at the owner's next save: unaligned, unverified, stale, untidy
- `[yours]`: this session's own next board entry when it holds an agent; outside a team, a
  message waiting in this session's inbox (`sunstack check --session`)

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
- `[yours]`: carry on with that entry as this agent and save when it moves; for a waiting
  message, the check skill
- the closing "more item(s) ... `sunstack org --attention`" line: mention the count once; run
  it only if the user asks

After the item is done, run `sunstack next` again and offer the new first item.
