---
name: fire
description: Remove an agent from this project's Sunstack team after the user approves, checking that nothing is lost. Use for "sunstack fire", "/sunstack:fire", "remove the reviewer agent", or deleting a Sunstack agent. Only in projects with a sunstack/ folder.
---

# Sunstack: fire

Deleting an agent deletes its folder: `AGENT.md`, pillars, board, context, threads and
archive. It needs the user's explicit approval.

## Rules

- If there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the checkup skill sets one up.
- Act only on this folder's team (its sessions on other hosts included). Work for a different
  team is delegated: send it as a task (message skill) to its agent that owns that work, else
  to its lead (its `pm` agent; none: ask the user), as `<team>/<id>` or `<host>:<team>/<id>`,
  and stop. Hiring, firing, spawning, dismissing or directing in a different team yourself
  needs the user's explicit yes first.
- Run every `sunstack` command on its own, with nothing chained after it.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer or a cancel is not approval.

## 1. Check

```sh
sunstack team
```

```sh
sunstack board "<id>"
```

Tell the user what will go: the role line, open key results (Now, Next), and any other
agent's `needs:` that points at this agent (`sunstack board` lists those). Suggest handing
open key results to another agent first, and `sunstack library save "<id>"` if the role is
worth keeping as a template.

## 2. Confirm and fire

Options: fire / cancel. On an explicit yes:

```sh
sunstack fire "<id>"
```

- **0**: fired. Remind the user to commit the removal under `sunstack/`.
- **4 `occupied`**: sessions are working as this agent; they must save and release first.
- **1 `would_lose_work`**: uncommitted files or pending messages. Show them and suggest
  committing first. Only if the user separately approves losing them, run
  `sunstack fire "<id>" --discard`.
- **1 / 2 other**: show the error.
