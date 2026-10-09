---
name: direct
description: Give the Sunstack team, a role or an agent a dated directive from the user (a priority, focus or constraint), or pause the team (halt). Use for "sunstack direct", "/sunstack:direct", "from now on every agent should ...", "pause the team". One piece of work for one agent goes through the message skill.
---

# Sunstack: direct

A directive is the user's instruction to the team, recorded as a dated line in
`sunstack/BOARD.md` (`- <date> D<n> <text> (to: all | <ids or titles>)`). Addressed agents
check their boards against it at their next `as` or save and record `aligned: D<n>`.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the checkup skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Only the user gives directives. Never add one on your own initiative or on an agent's
  behalf; suggest it to the user instead.
- A directive is not a pillar. If the user wants a lasting rule, offer the save skill's
  pillar proposal instead. If it changes what the team aims for, that is an objective: use the
  board skill.

## 1. Shape it

From the user's words, draft one short, checkable sentence (no parentheses) and the
addressees: `all`, titles (`researcher`: every researcher), or IDs (`researcher.macro`).
Check the IDs and titles against `sunstack team`. If the instruction is vague (no clear
action, or unclear who it is for), ask one question with 2 to 4 concrete suggestions.

A directive sets direction for whole roles. If the user instead wants one agent to do one
piece of work with a result, that is a task: offer to send it as a task brief with the message
skill (Goal, Scope, Done when, Verify, Report), so the agent knows its limits and how to prove
it is done.

If it conflicts with a pillar or an earlier directive (`sunstack board` lists them), point
that out and ask how to resolve it before adding.

## 2. Confirm

Show the exact line and addressees. Options: add / edit / cancel. End your turn and wait,
unless the user's message already gave the exact wording and addressees and said to add it.

## 3. Add

```sh
sunstack direct "<text>" --to "<all or comma-separated ids/titles>"
```

- **0**: tell the user the directive number and that each addressed agent aligns at its next
  `as` or save. If this session works as an addressed agent, align now with the save skill.
- **1 `not_found`**: an addressee does not exist; fix it with the user.
- **2**: show the error (for example parentheses in the text) and fix the wording.

## Pause the team

When the user wants every agent to stop starting new work (a broken schema, a bad deploy),
confirm the reason with them, then run:

```sh
sunstack halt "<reason>"
```

Work in progress saves, and nothing new starts (spawns are refused) until the
user ends it with `sunstack halt --off`. Only the user decides to halt or to end a halt.
