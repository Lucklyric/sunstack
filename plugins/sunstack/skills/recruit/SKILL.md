---
name: recruit
description: Add a new agent to this project's Sunstack team from a short description, by hiring from a template, copying the role of an agent already on the team, or drafting a new role for the user to approve. Use when the user says "sunstack recruit", "/sunstack:recruit", "hire an agent that ...", "add another researcher agent", "create a new agent", or describes an agent role they want on the team. Also offers sunstack init when there is no team yet.
---

# Sunstack: recruit

Turn the user's description into a new agent. A new agent's `AGENT.md` is its prompt, so the
user approves it before it exists.

Every agent's ID is `<title>.<name>`: the title is the role (`researcher`), the name is this
one agent (`macro`). A role can have many agents, each with its own prompt copy, pillars and
context.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- If the shell says `sunstack` is not found, tell the user to install it
  (`curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh`,
  then `sunstack install`) and stop. If there is no `sunstack/` folder, offer `sunstack init`
  (it sets up the current folder).
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait. No answer, a cancel, or the user moving on is not approval.

## 1. Look at what exists

```sh
sunstack library
```

```sh
sunstack team
```

## 2. Fill the gaps with a short Q&A

You need four things: **the role** (what the agent is for), **a name** for this agent, **how
it works** (what it must and must not do), and **what it should remember** in this project.
Take what the user already said; ask only for what is missing or too vague to write down.

- Ask at most three questions per round, each with 2 to 4 concrete suggestions drawn from the
  project and the team (the user can always type their own). Suggest names that say what sets
  this agent apart (`macro`, `equities`, `swing`), not people's names unless the user wants.
- If the description is one vague line ("a researcher"), start with the role and its focus.
- Do not ask about things with a sensible default; put the default in the draft instead.
- Stop asking as soon as you can write a specific draft.

## 3. Pick the source

- **A role with that title is already on the team** (`sunstack team` lists it): propose
  another agent of the same role. `sunstack hire <title> <name>` copies the role from a
  template if one exists, otherwise from that colleague (`--from <id>` picks which one). Show
  the `AGENT.md` it will copy and the new ID, and ask (approve / draft a different role /
  cancel).
- **A template fits** (`sunstack library`): propose it, show it
  (`sunstack library show "<title>"`) with the new ID, and ask the same way.
- **Otherwise** draft a new role (step 4).

Only after an explicit approve, go to step 5.

## 4. Draft a new role

Pick a `title` (`[a-z][a-z0-9-]`, 2 to 32 characters, not `review`, not starting with `_`).
Write the draft to a new file for this recruit only, for example
`sunstack/_local/tmp/_recruit/<title>-<time>.md`, in English and in this shape:

```md
---
title: <title>
---
## Role

<one line: what this agent is for>

## How I work

- <how it works, what it must and must not do>

## Context policy

Remember:
- <what it should remember in this project>

Do not record:
- <what it should not record>
```

Keep it short and specific to what the user described. Only `title` goes in the frontmatter;
`sunstack hire` adds `name`, `hired` and `from`.

Show the whole draft and the ID `<title>.<name>`. Options: approve / edit / cancel. Then end
your turn and wait. On edit, write a new draft file and show it again. A description from the
user is not approval of your draft, and the command's own permission prompt is not enough on
its own: Codex may route it to an automatic reviewer.

## 5. Create it

```sh
sunstack hire "<title>" "<name>" --file "<the draft file you showed>"
```

Leave out `--file` when hiring from a template or a colleague (add `--from "<id>"` if the user
chose a specific colleague). Use exactly the file the user approved, unchanged; a draft under
`sunstack/_local/tmp/` is deleted once it is hired. Claude Code and Codex may also ask for
approval of the command itself; that prompt is expected.

- **0**: created. Tell the user the new ID and how to use it (`/sunstack:as <id>` in Claude
  Code, `$sunstack:as <id>` in Codex).
- **2 `missing_arguments: name`**: ask for a name.
- **1 `exists`**: the ID is taken; propose another name.
- **1 / 2 other**: show the error.

## 6. Optional follow-ups

- Suggest one to three first key results for the new agent, each tagged with a team objective
  from `sunstack board`. They are written by the agent's first session (`/sunstack:as <id>`,
  then save), not by you.
- If the user also wants rules only for this agent, propose them as pillars and follow the save
  skill's approval step (`sunstack amend "<id>" pillars.md ...`).
- For a new role, ask whether to keep it as a personal template for other projects. On yes:

  ```sh
  sunstack library save "<id>"
  ```
