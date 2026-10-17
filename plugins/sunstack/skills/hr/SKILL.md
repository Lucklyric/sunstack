---
name: hr
description: Review the Sunstack team's staffing against the project (uncovered objectives, missing roles, overloaded or idle agents) and carry out the changes the user approves. Use for "sunstack hr", "/sunstack:hr", "does the team need another agent", "what roles is the team missing". Only in projects with a sunstack/ folder.
---

# Sunstack: hr

Command: `sunstack hr`. Skill: `/sunstack:hr` in Claude Code, `$sunstack:hr` in Codex.

The CLI gathers the facts; you judge what they mean for this project and propose changes.
Nothing changes without the user's approval: more sessions go through the spawn skill, role
changes and hires through the hire skill, removals through the fire skill.

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
  fits (suggest it takes a key result; no hire), or one almost fits (suggest extending its
  `AGENT.md`), or no role fits (suggest a hire).
- **Gaps**: a `needs:` or directive pointing at a role nobody has.
- **Missing roles the project clearly calls for**: for example, code with no tests and no
  reviewer, a paper with no editor, data pipelines with no one owning data quality. Tie each
  to evidence you saw (folders, files, board entries, the user's goals).
- **Overload**: an agent with many Now entries or waiting messages; suggest more sessions of
  that agent (spawn skill, within the team's `max_sessions`) or moving key results. A second agent of the same role only when
  its work needs separate lasting context.
- **Overlap**: two agents whose roles cover the same work with the same pillars and no need to
  stay independent (a verifier and the builder it checks must stay apart); suggest folding one
  into the other: hand over its open key results and lasting context, then retire it (Retire,
  below).
- **Idle agents**: no board entries and no activity for weeks; suggest giving them key
  results, or retiring them (fire) if their role no longer fits.
- **Names**: unnamed agents from older versions; suggest `sunstack rename` (the health skill
  does it).

## 3. Propose

Show a short list. For each change: what (another session of `<id>`, extend `<id>`'s
`AGENT.md`, hire `<title>.<name>`, retire `<id>`), why (the evidence), and what it would own
(one or two key results under which
objective). Mark the one you recommend most. Options per change: do it / skip.

## 4. Carry out

- **Another session**: follow the spawn skill.
- **Extend a role**: as in the hire skill (snapshot, show the exact before and after,
  amend on a yes).
- **Hire**: follow the hire skill with the role you proposed as the description; it drafts
  and asks for approval of the exact `AGENT.md` before `sunstack hire`.
- **Retire**: first send the agent taking over a `handoff` (send skill) with its open key
  results and useful context, wait for its `done` reply, then follow the fire skill.
- **Rebalance**: suggest the moves on the boards; the agents make them at their next save.

End with a one-line summary of what changed and what is left for later.
