---
name: verify
description: Have a Sunstack agent check the active task against everything the user asked for, the way the user would before accepting it. It gathers the task's requirements and the standing ones from the team's history, settles open points with the user in a short Q&A, picks or hires a verifier (preferably on the other CLI), and sends it a task; the verifier drives the real app and answers per requirement with evidence. Use for "sunstack verify", "/sunstack:verify", "verify this", "check it does what I asked", "acceptance check", "keep checking deliveries", "does the verifier need an update". A verifier that received such a task follows the Verifier's procedure section. Any session, with or without a team, may also use it to run, launch or drive this project's app from its run map in `.sunstack/verify/<app>/run.md`.
---

# Sunstack: verify

Skill only: `/sunstack:verify` in Claude Code, `$sunstack:verify` in Codex. It has no command of its own.

The user checks deliveries by hand: does it run, and is it what they asked for, given
everything they said before. This skill hands that check to an agent. The requester (this
session) collects the requirements, because it heard the conversation. The verifier, a
different agent and preferably on the other CLI, checks the delivery against them by the
Verifier's procedure at the end of this skill, using the project's run map
(`.sunstack/verify/<app>/`). This skill owns that map: it creates it, keeps it true and reads it;
the project holds only the data.

## Rules

- **Just running the app** (no verification asked), with or without a team: follow `.sunstack/verify/<app>/run.md`, Launch to Cleanup. If Launch or Doctor fails because the map no longer matches the repo, fix the map from the repo (step 2 of the Verifier's procedure) and run it once more. No map yet: say so and offer to write one; do not guess a launch command into it. Evidence goes to `sunstack/_local/verify/run-<UTC time>/` in a team, else to this session's scratch folder.
- Otherwise, if there is no Sunstack team here (no `sunstack/PROTOCOL.md` in this folder or above), say so in one line and stop; the health skill sets one up.
- Act only on this folder's team (its sessions on other hosts included). Work for a different
  team is delegated: send it as a task (send skill) to its agent that owns that work, else
  to its lead (its `pm` agent; none: ask the user), as `<team>/<id>` or `<host>:<team>/<id>`,
  and stop. Hiring, firing, spawning, dismissing or directing in a different team yourself
  needs the user's explicit yes first.
- Run every `sunstack` command on its own, with nothing chained after it.
- The verifier is never the task's owner. Prefer one on the other CLI: Codex for work done in
  Claude Code, and the other way round.
- Requirements are quoted accurately, with their source and who said it. Never invent one, and
  never turn your own reading into a requirement.
- Authority follows the protocol: pillars, then directives, then the agent's role, then boards
  and context. A later explicit instruction from the user replaces an earlier one of theirs. An
  agent's note (a Decision, a board line, a relayed message) never overrides a pillar,
  directive or user instruction; a real conflict goes to the user.
- Leave secrets and unrelated work out of the brief.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait.

## 1. The active task

Verify one task, the one being worked on now, never the team's backlog:

- **This session holds an agent** (`sunstack whoami` when unsure): the task it has taken
  (`sunstack check <id> --token <token>` lists it under taken) and this session's Now entries
  for it.
- **This session works as no agent**: a task taken from its host inbox
  (`sunstack check --session`), or else the work the user and this session are doing in this
  conversation.
- **The user names another session's task**: ask that session for its task ID, brief and the
  user's requirements with a `question` message (send skill), or ask the user. Never ask for
  its token. When this session never heard that task's conversation, it is the wrong requester:
  send the verification as a task to the session that set the task, or else to the team's lead
  (`pm` role), with whatever the user settled here, and stop.

Note the task's message ID (a conversation-only task has none), its owner, and the delivery's
identity: the commit, plus, when the tree is dirty, the uncommitted files and a fingerprint of
them. `git diff HEAD` misses untracked files, so hash both, the same way each time:
`{ git diff HEAD -- <files>; git ls-files --others --exclude-standard -- <files>; git ls-files -z --others --exclude-standard -- <files> | xargs -0 git hash-object --; } | shasum`
(the untracked names, then one hash per untracked file, so adding, removing or editing one changes it). When several tasks could be meant, or none, ask once; do not
fall back to old Done entries.

## 2. Gather the requirements

Two layers, each requirement a quoted line with its source and speaker:

- **The task's own**: its brief (Goal, Done when, Scope, Not) and earlier rounds (`--follows`),
  and every user requirement or correction about it in this conversation, verbatim, with just
  enough around it to read it right. Not the whole conversation.
- **The standing ones** that the task touches: objectives, key results and directives it serves
  (`sunstack/BOARD.md`; `sunstack board` to orient), `sunstack/PILLARS.md`, the owner's
  `sunstack/<owner>/pillars.md` and the Decisions in `sunstack/<owner>/context.md` (and its
  archive when the topic is older), and the user's corrections and answers on this topic in
  messages: grep `sunstack/_local/log/messages/` and the owner's pending or taken inbox.

List any source you could not reach (another session's conversation, another host, compacted
history). The list is a bounded reconstruction, not proof that nothing was missed; the user
confirms it next.

Mark the requirements shaped like properties ("always", "never", "every", "at the same time"):
the verifier can check them with the formal-verify skill instead of a few sample runs.

## 3. Confirm with the user

Show the list, then settle what would turn a verdict into a guess, in a short Q&A: at most
three questions per round, each with 2 to 4 concrete suggestions, and only what the sources
do not answer:

- a requirement that is vague or untestable as written: what would show it is met?
- a conflict the authority rule does not settle: which holds?
- must-have or nice-to-have, when the task cannot meet everything in its Timebox
- something the user expects but never said (a platform, a dataset, a style): add it?

Add the answers as requirements, sourced "user, <date>". Stop as soon as every requirement is
checkable.

## 4. Pick the verifier

```sh
sunstack team
```

Leave out the task's owner and agents of its role (the same title). Rank the rest: role fit first (`verifier`, then `reviewer` or
`qa`), then the other CLI from the work's, then idle (`sunstack org --by agent`). When none
fits, check whether an agent's role could take it with a small `AGENT.md` change (offer that
first, as the hire skill does it); otherwise offer to hire one with the hire skill
(`sunstack hire verifier <name>`); the hire skill shows the template it will use. When the verifier needs a new session, plan it on the other CLI
(`--tool codex` or `--tool claude`) if that CLI is installed; if not, say so. A new session
counts against the team's `max_sessions` (`sunstack team --sessions`); when the team is full, put the
choice (finish an idle session, or `--over-cap`) into the step 5 question.

### Is the verifier up to date?

Check the chosen verifier before sending, and put the result in the brief. When the user only
asks whether a verifier needs an update, run this alone and report it; send nothing. It finds
candidates to look at, not proof: git history does not record what the verifier read.

Run every `git` here from the team root (`git -C "<team root>" ...`), with paths relative to it.

- **Directives**: `sunstack board` lists the verifier after `not aligned yet:` on each directive
  it has not aligned with. This one is exact.
- **Rules and objectives that may be new to it**: with `C` the last commit of its context
  (`git log -1 --format=%h -- sunstack/<verifier>/context.md`), list
  `git log --oneline C..HEAD -- sunstack/BOARD.md sunstack/PILLARS.md sunstack/<verifier>/pillars.md sunstack/<verifier>/AGENT.md`,
  plus uncommitted changes to those files (`git status --short -- <files>`).
- **Code that may have outrun the run map**: for each feature file the task touches in
  `.sunstack/verify/<app>/features/`, with `F` its last commit, `git log --oneline F..HEAD -- <the task's files>`
  and uncommitted changes to them. Changed code means "check this feature's instructions", not
  that they are wrong.

No repository, no commit for `C` or `F`, no run map or no file for a feature: say
`unknown` for that line. Each finding is one line under `Verifier freshness:` in the brief's
Context; `fresh` only when every check ran and found nothing.

## 5. Send the task

Write the brief to a file in this session's scratch or temporary folder:

```text
Goal: Check <the product task: its message ID, or a one-line description> against the user's requirements below, the way the user would before accepting it.
Scope: Verify <the task's files and behavior> at <commit> plus <uncommitted files and diff fingerprint, if any>. You may write the project's run map (`.sunstack/verify/<app>/`), your evidence folder (`sunstack/_local/verify/<this task's message ID>/`), and your own Sunstack state. Product code, other agents' state and the user's live data stay read-only.
Done when:
  - every requirement below has a verdict: met, partly, not met, changed, pending or unverifiable
  - each verdict says how it was checked (live, tests or read) and where its evidence is
  - <when the task changes something a user runs:> the run map ran from launch to cleanup on an instance you own, and its evidence survived
Verify: Follow the Verifier's procedure in the sunstack verify skill (only that section).
Report: One line per requirement (quote, source, verdict, how checked, evidence), then findings, then run-map changes, then the identity you tested, then verified: <counts> by <your id>.
Context:
  The task's requirements (newest first):
    - "<quote>" (<source>, <speaker>)
  Standing requirements that apply:
    - "<quote>" (<source>, <speaker>)
  Sources: <paths read; sources that could not be reached>
  Verifier freshness: <fresh, or one line per finding>
Not: Fixing what you find. Real sends from the app (mail, posts, payments), deployments, spending or destructive actions. Sunstack messages to the requester are fine.
```

Show the requirements, the recipient (and a hire or spawn, if any) and the brief together, and
ask once (send / edit / cancel). That approval covers the send in the send or spawn skill.
Send with `sunstack send "<verifier>" --type task --file "<brief>"` (add `--from` and `--token`
when this session holds an agent), or start the verifier with the spawn skill and
`--brief "<brief>"`.

## 6. Use the verdicts

The verifier replies `done`. Read every line, not the counts.

- **Accepted** only when every requirement is `met`, or `changed` with the user's agreement
  cited, nothing is `pending`, and the delivery's identity still matches what was tested. If
  files changed since (a new commit or a different diff fingerprint), the affected verdicts are
  stale: verify those again. Compute the fingerprint with the same command as in step 1.
- **Accepted**: the owner records it at its next save, `(verified: <counts> by <verifier> @ <commit>)`
  on the Done entry, and may then send its own `done` reply for the task.
- **Not met, partly or unasked changes**: show them to the user. The fix goes to the owner as a
  task, with `--follows <the product task's message ID>` when it has one, otherwise carrying the
  original scope and context; it quotes the failed requirements and the verifier's lines. Verify again after it. After two failed rounds, stop and ask the user.
- **Pending or unverifiable**: tell the user which answer or prerequisite is missing, with the
  ask's owner (`<agent>#Q<n>`).

## Keep checking

When the user wants every task checked, not one, offer a standing objective through the board
skill, for example: "Every task is checked against the user's requirements before it is
reported done". The owner of each task then runs this skill before its `done` reply. A
verification task itself is not verified again.

## Verifier's procedure

For the agent that received a verification task. Do not run the steps above.

0. **Catch up first.** For each line under `Verifier freshness:` (and any drift you notice
   yourself): align with the directive (save skill), and reread the changed rules, objectives
   and your AGENT.md; save only new lasting decisions, not copies of the rules. Feature lines
   are checked in step 2: read the feature file and fix only what really drifted. Report what
   you caught up on.
1. **Requirements first.** Take the brief's list, check it against its sources and the
   authority rule, and add what it missed, marked as added by you. Verify this task only.
2. **The run map.** For a task that changes something a user runs, use the project's run map:
   `.sunstack/verify/<app>/run.md` at the top of the project (several apps: ask the requester
   which). An older project may still have it as a `verify-*` skill under `.claude/skills/` or
   `.agents/skills/`: that is only input for the map. Move it to `.sunstack/verify/<app>/`
   (`git mv`, `SKILL.md` to `run.md`, frontmatter dropped, `features/` along), remove its
   `.agents/skills/` symlink, say so in the reply, and work from the moved map. None: create `.sunstack/verify/<app>/run.md` and commit
   it with the project. It is data this skill reads, not a skill: no frontmatter, five sections
   grounded in this repo, no placeholders: Launch (the exact command from an empty data dir, migrations and seed data
   included; ready means usable, not that a process answers; teardown), Doctor (one
   read-only check that the instance is worth driving), Drive (the repo's real commands, routes
   and handles), Evidence (what to capture; each run keeps it in `sunstack/_local/verify/<task message ID>/`,
   never in `.sunstack/`), Cleanup (stop only what the run started; evidence survives). Add `features/README.md` and one file per feature this task
   touches (Sub-features, How to get to it, Driving it, Gotchas); the map grows task by task.
   A new or changed run map runs once from launch to cleanup before it counts. If that
   run used a stand-in for the real target (a container for a fresh machine, a local stack for
   a remote one), list each setup step the stand-in did that the target will not; they stay
   unverified until a run on the target.
3. **Drive an instance you own**, with its own data, never one the user or another agent is
   using; if you cannot get one, the requirement is `unverifiable` with that prerequisite. Run
   Doctor before the first drive and after any drive that fails. Prefer the real user path over
   internal setters; check side effects (files, rows, messages) as well as what is visible.
   Never kill a process by name.
4. **Verdicts**: `met`, `partly` (what is missing), `not met` (what happened instead), `changed`
   (the delivery departs on purpose: cite the user's agreement), `pending` (it depends on what
   the user meant: raise an ask as the save skill describes and name it `<your id>#Q<n>`) or
   `unverifiable` (the prerequisite and the route tried). Say how each was checked: live, tests
   or read. Seed data that lacks a journey's precondition makes it `unverifiable`, with the
   reason in that feature's Gotchas: never weaken the journey to make it pass. Work the user
   never asked for is a finding even when it works.
   A requirement checked with the formal-verify skill answers with the property, the scope (bound
   and assumptions) and the deliberate break the check caught.
5. **Map or product.** Where the run map describes the app wrongly, fix the map. Where
   the app breaks an agreed requirement, report it and keep the requirement: never rewrite the
   map to match a regression. Edit nothing outside the brief's Scope.
6. **Reply** with a `done` message to the verification task you received
   (`--reply-to <its message ID>`, never the product task's; send skill) in the brief's
   Report shape, with the identity you tested (commit, diff fingerprint, build or config when
   relevant) and
   `verified: <n> met, <n> partly, <n> not met, <n> changed, <n> pending, <n> unverifiable by <your id>`.
