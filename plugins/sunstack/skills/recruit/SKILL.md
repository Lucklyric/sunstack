---
name: recruit
description: Add a new agent to this project's Sunstack team from a short description, by hiring from a template or drafting a new role for the user to approve. Use when the user says "recruit", "sunstack recruit", "/sunstack:recruit", "hire a ...", "I need an agent that ...", "create a new agent", or describes a role they want on the team.
---

# Sunstack: recruit

Turn the user's description into a new agent. A new agent's `AGENT.md` is its prompt, so the
user approves it before it exists.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- If the shell says `sunstack` is not found, tell the user to install it
  (`curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh`,
  then `sunstack install`) and stop. If there is no `sunstack/` folder, offer `sunstack init`.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer, a cancel, or the user moving on is not approval.

## 1. Look at what exists

```sh
sunstack library
```

```sh
sunstack team
```

If a template already fits the description, propose hiring from it instead of drafting: say
which template and why, then go to step 4 with `sunstack hire <title> [name]`.

## 2. Draft the agent

Pick a `title` (the role, `[a-z][a-z0-9-]`, 2 to 32 characters, not `review`, not starting
with `_`) and, if that title already has an instance, a `name` for this one. Then write the
draft file into `sunstack/_local/tmp/_recruit/<title>.md`, in this shape and in the language
the team's files use:

```md
---
title: <title>
---
## 职责

<one line: what this agent is for>

## 工作方式与规则

- <how it works, what it must and must not do>

## Context 策略

记：
- <what it should remember in this project>

不记：
- <what it should not record>
```

Keep it short and specific to what the user described. Only `title` goes in the frontmatter;
`sunstack hire` adds `name`, `hired` and `from: custom`.

## 3. Show it and ask

Show the whole draft and the resulting ID (`<title>` or `<title>.<name>`). Options: approve /
edit / cancel. On edit, change the draft and show it again. A description from the user is
not approval of your draft.

## 4. Create it

```sh
sunstack hire "<title>" "<name>" --file "sunstack/_local/tmp/_recruit/<title>.md"
```

Leave out `"<name>"` when there is none, and leave out `--file` when hiring from a template.
In Claude Code and Codex this command asks the user for approval itself; that prompt is
expected.

- **0**: created. Tell the user the new ID and how to use it (`/sunstack:as <id>` in Claude
  Code, `$sunstack:as <id>` in Codex).
- **2 `missing_arguments: name`**: the title already has an instance; ask for a name.
- **1 `exists`**: the ID is taken; propose another name.
- **1 / 2 other**: show the error.

## 5. Optional follow-ups

- If the user also wants rules only for this agent, propose them as pillars and follow the save
  skill's approval step (`sunstack amend "<id>" pillars.md ...`).
- Ask whether to keep this role as a personal template for other projects. On yes:

  ```sh
  sunstack library save "<id>"
  ```
