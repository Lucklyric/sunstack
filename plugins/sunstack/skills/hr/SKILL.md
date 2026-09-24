---
name: hr
description: Review the Sunstack team's staffing against the project and suggest changes, such as new hires for uncovered objectives or missing roles, a second agent for an overloaded role, or retiring idle agents, then carry out the ones the user approves. Use when the user says "sunstack hr", "/sunstack:hr", "which agent should we hire", "does the team need another agent", "is the Sunstack team right for this project", or asks what agent roles the project is missing. Only for projects with a Sunstack team (a sunstack/ folder).
---

# Sunstack: hr

The CLI gathers the facts; you judge what they mean for this project and propose changes.
Nothing changes without the user's approval: hires go through the recruit skill, removals
through the fire skill.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the checkup skill sets one up.
- Run every `sunstack` command on its own, with nothing chained after it.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer or a cancel is not approval.
- Suggest few, well-argued changes (at most three at a time). A small team that covers the
  objectives beats a large one; do not hire for work nobody has asked for.

## 1. Gather

```sh
sunstack hr
```

```sh
sunstack board
```

`hr` shows each agent's load (Now and Next entries, sessions, waiting messages, last
activity, flagged OVERLOADED or IDLE), objectives nobody works toward, needs and directives
that point at someone not on the team, templates not in use, and what the project is made of.
Then look at the project itself as far as needed: `AGENTS.md`, the README, the main folders,
recent git history, and what the user has been doing in this conversation.

## 2. Judge

Look for, in this order:

- **Uncovered objectives**: an objective with no key result. Either an existing agent's role
  fits (suggest it takes a key result; no hire), or no role fits (suggest a hire).
- **Gaps**: a `needs:` or directive pointing at a role nobody has.
- **Missing roles the project clearly calls for**: for example, code with no tests and no
  reviewer, a paper with no editor, data pipelines with no one owning data quality. Tie each
  to evidence you saw (folders, files, board entries, the user's goals).
- **Overload**: an agent with many Now entries or waiting messages; suggest a second agent of
  the same role (`<title>.<name>`, which copies the role) or moving key results.
- **Idle agents**: no board entries and no activity for weeks; suggest giving them key
  results, or retiring them (fire) if their role no longer fits.
- **Names**: unnamed agents from older versions; suggest `sunstack rename` (the checkup skill
  does it).

## 3. Propose

Show a short list. For each change: what (hire `<title>.<name>`, second agent of a role,
retire `<id>`), why (the evidence), and what it would own (one or two key results under which
objective). Mark the one you recommend most. Options per change: do it / skip.

## 4. Carry out

- **Hire**: follow the recruit skill with the role you proposed as the description; it drafts
  and asks for approval of the exact `AGENT.md` before `sunstack hire`.
- **Retire**: follow the fire skill.
- **Rebalance**: suggest the moves on the boards; the agents make them at their next save.

End with a one-line summary of what changed and what is left for later.
