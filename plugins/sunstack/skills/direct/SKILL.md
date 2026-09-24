---
name: direct
description: Give the Sunstack team, one role, or one agent a dated directive from the user (a priority, a change of focus, a constraint for now), which every addressed agent aligns its board with at its next as or save. Use when the user says "direct", "sunstack direct", "/sunstack:direct", "tell the team to ...", "from now on everyone should ...", "the researchers should focus on ...", or gives an instruction meant for agents that are not this session.
---

# Sunstack: direct

A directive is the user's instruction to the team, recorded as a dated line in
`sunstack/BOARD.md` (`- <date> D<n> <text> (to: all | <ids or titles>)`). Addressed agents
check their boards against it at their next `as` or save and record `aligned: D<n>`.

## Rules

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
