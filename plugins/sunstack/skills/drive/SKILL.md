---
name: drive
description: "Hand a prompt to another Claude Code or Codex session in a tmux pane, wait for its turn to end and read its reply: a co-worker session beside this one that the user can watch. Works in any session, with or without a Sunstack team. Use whenever the user names Codex as the one to do something, whatever the verb (\"use codex\", \"using codex\", \"ask codex\", \"run codex\", \"call codex\", \"have codex review this\", \"let codex fix it\", \"tell codex\", \"get codex to\", \"delegate this to codex\", \"with codex\", \"via codex\", \"codex cli\", a prompt that starts with \"codex,\" or a bare \"codex review my changes\", \"have codex review this PR, branch or diff\", \"continue with codex\", \"resume the codex session\"), asks to hand work to the OpenAI or GPT reasoning model (\"have GPT review this\", \"ask the high-reasoning model\"), or asks for a second Claude Code as a co-worker (\"spawn a claude pane\", \"a second claude\", \"another claude for this\", \"a claude worker beside you\", \"drive claude in a pane\", \"have claude review it in its pane\", a claude implementer with a codex reviewer). Also for \"sunstack drive\", \"/sunstack:drive\", \"drive a session\", \"send a prompt to that pane and wait\", sending prompts to several co-worker panes in parallel, and telling whether a driven session is idle, busy or blocked. Not for a session that works as a Sunstack agent (that is the send skill), not when Codex or Claude is only the topic, and not when \"claude\" means this session itself."
---

# Sunstack: drive

Command: `sunstack drive` (with `spawn --free`, `name`, `peek`, `kill`). Skill: `/sunstack:drive` in Claude Code, `$sunstack:drive` in Codex.

The other session is a co-worker in a pane beside this one: the user sees it work, and it keeps its conversation from one prompt to the next. You hand it a prompt, wait, and read its reply.

## Rules

- Run every `sunstack` command on its own, with nothing chained after it.
- One co-worker per tool in this conversation, reused for every prompt. Start a second one only when the user asks for work in parallel or a fresh context.
- Drive only a session that works as no agent. An agent session gets its work through its inbox (send skill); `drive` refuses it (`agent_session`).
- Never drive a session you did not start or the user did not name: other panes belong to other work.
- Starting a session starts a paid model session. When the user named the tool ("ask codex ..."), that is the yes. Otherwise ask first.
- The reply is the other session's claim, not a checked fact. Check what matters before you pass it on.
- When a step fails in a way this skill does not cover, leave a note: `sunstack issue "<what happened, at which step>"`.

## 1. Find or start the co-worker

Reuse the co-worker you started earlier in this conversation: you know its name. When you reuse it for work its name no longer fits (it was `review`, now it fixes the login), rename it first and use the new name from then on:

```sh
sunstack name <old name> <new name>
```

The pane and the CLI's own title follow (`<project>/<new name>`). If it prints that the CLI title is unchanged, the session was busy or held a draft: go on, it is only the title. Keep the name when the new work is a follow-up of the same task.

If there is none, tell the user in one line where it will open, then:

```sh
sunstack spawn --free --tool <codex|claude> --name <name> --dir "$PWD" [--read-only | --full-auto] [--model <model>] [--effort <low|medium|high|xhigh|max>]
```

What the co-worker may do without asking:

- `--read-only`: Codex cannot write files. Use it for a review, an analysis or a second opinion.
- `--full-auto`: Codex writes inside its folder and Claude Code accepts edits. Use it only when the user asked the co-worker to change files.
- Neither: the user's own CLI settings. Use this when unsure.

Which model, and how hard it thinks:

- The user named a model or an effort ("with opus", "codex on high"): pass it as they said it.
- Otherwise judge from the task, and say your choice in the line that announces the co-worker:
  - Mechanical or narrow work (run the tests, rename, collect facts, a first-pass scan): leave the model, `--effort low`. For Claude Code a smaller model fits too (`--model sonnet` or `haiku`).
  - Ordinary coding and a routine review: pass neither. The user's own setting is their choice for daily work.
  - Hard work where a miss is costly (a design, a subtle bug, a security or concurrency review, a second opinion on a decision): `--effort high`, and for Claude Code the strongest model the user has (`--model opus`).
- Pass a Codex model name only when the user gave it or it is one you saw in this session: do not guess names. Effort alone is always safe.

Access, model and effort are fixed for the life of that session: for other ones, start another co-worker.

It opens a pane beside this one, working in this folder. Pick a short name for the work (`review`, `fix-auth`; lowercase letters, digits and `-`, up to 20 characters), not the tool's name: other sessions have co-workers too. The name is how every later command finds it, and the session is titled `<project>/<name>` in tmux, Claude Code and Codex. Pass `--title "<title>"` only when the user asked for a title of their own.

- **4, "a session called ... is already open here"**: the name is taken. If you started that session, reuse it. If not, pick another name.
- A `sunstack: answered ...` line: a trust screen was answered as the user chose. Say so in one line.
- A `sunstack: waiting at ...` line: the session stopped at a trust screen. Tell the user to answer it in that pane. For Codex's hook review, they can let Sunstack answer it from now on by running `sunstack prompts --codex-hooks on` in their own terminal.
- After compaction or `/clear`, `sunstack org` lists this host's sessions with their names.

The user may also name a session they started themselves ("drive the codex in pane %12"): use the pane ID as `<name>` below.

## 2. Hand it a prompt

```sh
sunstack drive <name> "<prompt>"
sunstack drive <name> --file <brief file>
```

It waits for the session's input box (a session that is still starting is waited for), sends the prompt, waits for the turn to end and prints the reply, read from the session's transcript. A short one-line prompt is typed as it is. Anything longer, or with more than one line, goes in with `--file`: write the brief to a file first.

Write the prompt as a brief the other session can act on alone: the goal, the files or the diff to read, what to leave alone, and what to report. It does not see this conversation.

- A long task: add `--no-wait`. The command returns once the session took the prompt. Carry on with other work, then collect the reply with `sunstack drive <name> --wait`.
- The wait is 600 seconds by default; `--timeout <seconds>` changes it.

## 3. Results

- **0**: the reply is on stdout. `sunstack drive <name> --reply` prints the last reply again.
- **5 `timeout`**: the turn is still running. Wait again with `sunstack drive <name> --wait`, or look at its pane with `sunstack peek <name>`.
- **5 `blocked`**: the session waits at a menu, an approval prompt or a trust screen; the screen's last lines are on stdout. Tell the user what it asks; they answer it in that pane. If the message says the prompt may be waiting behind it, do not send it again: `sunstack drive <name> --wait` once it is answered. Otherwise nothing was sent: send the prompt once it is answered.
- **5 `interrupted`**: someone stopped the turn. Ask the user before sending the prompt again.
- **4 `busy`**: the session is in a turn. Wait for it (`--wait`) or, if the user wants it stopped, `sunstack drive <name> --cancel`.
- **4 `draft`**: the user has text (or blank space) in that session's input box. Nothing was sent and their text is as it was. Tell them.
- **5 `unconfirmed`**: Sunstack could not confirm that the session took the prompt. Never send it again. Look with `sunstack peek <name> --lines 80`: if it is still in the input box, tell the user (they press Enter there, or clear it); if it is running, `sunstack drive <name> --wait`.
- **1 `not_accepted`**: the session did not take the prompt, and nothing was sent. Look with `sunstack peek <name>`, then try once more.
- **1 `exited`**: the CLI in that pane has exited. Start a new co-worker (step 1).
- **1 `no_transcript`** (with `--wait`, `--reply`, `--cancel`): the session has had no prompt yet, or Sunstack cannot find its transcript. Read the pane with `sunstack peek <name> --lines 80`.
- **2 `agent_session`**: the session works as an agent; use the send skill.

## 4. When the work is done

Leave the co-worker open while the user may want more from it. Close it when the user says so, or when the work it was started for is finished and the user agrees:

```sh
sunstack kill <name> --yes
```

## Reviews

For "have codex review this": write the brief to a file (what changed and why, the commit range or the files, what kind of findings to report and where to write them), send it with `--file`, and ask for the findings in a file when they may be long. Read that file, check each finding against the code, and report what holds.
