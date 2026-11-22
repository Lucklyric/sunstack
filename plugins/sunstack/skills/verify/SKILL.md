---
name: verify
description: Have a Sunstack agent check the active task against everything the user asked for, the way the user would before accepting it. It gathers the task's requirements and the standing ones from the team's history, settles open points with the user in a short Q&A, picks or hires a verifier (preferably on the other CLI), and sends it a task; the verifier drives the real app and answers per requirement with evidence. On request it also agrees a feature's expected results with the user before work starts and freezes them in the run map. Use for "sunstack verify", "/sunstack:verify", "verify this", "check it does what I asked", "acceptance check", "write the expected results first", "agree the results before we build", "keep checking deliveries", "does the verifier need an update". A verifier that received such a task follows the Verifier's procedure section. Any session, with or without a team, may also use it to run, launch or drive this project's app from its run map in `sunstack/verify/<app>/run.md`.
---

# Sunstack: verify

Skill only: `/sunstack:verify` in Claude Code, `$sunstack:verify` in Codex. It has no command of its own.

The user checks deliveries by hand: does it run, and is it what they asked for, given
everything they said before. This skill hands that check to an agent. The requester (this
session) collects the requirements, because it heard the conversation. The verifier, a
different agent and preferably on the other CLI, checks the delivery against them by the
Verifier's procedure at the end of this skill, using the project's run map
(`sunstack/verify/<app>/`). This skill owns that map: it creates it, keeps it true and reads it;
the project holds only the data.

**Project data folder.** Sunstack skills keep a project's data in `sunstack/` at the top of the project (beside the team files, when there is a team), or in the folder that `sunstack.json` there names (`{"data": "<path inside the project>"}`). Paths in this skill that start with `sunstack/verify/` or `sunstack/formal-verify/` mean that folder. It holds only what is committed, never run output. Before a skill creates it for the first time, settle the place with the user in a short Q&A (AskUserQuestion in Claude Code, a numbered question in Codex), showing what will go in it: `sunstack/` (recommended: one folder for skill data and any later team), `.sunstack/` (hidden), `docs/sunstack/`, or create nothing (check only, leave nothing behind); the user may name another folder. For any folder other than `sunstack/`, write `sunstack.json` with its path and commit both. When the folder already exists, use it without asking.

## Rules

- **Just running the app** (no verification asked), with or without a team: follow `sunstack/verify/<app>/run.md`, Launch to Cleanup. If Launch or Doctor fails because the map no longer matches the repo, fix the map from the repo (step 2 of the Verifier's procedure) and run it once more. No map yet: say so and offer to write one; do not guess a launch command into it. Evidence goes to `sunstack/_local/verify/run-<UTC time>/` in a team (a `sunstack/PROTOCOL.md` here or above, which also keeps `_local/` out of git), else to this session's scratch folder.
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
  directive or user instruction; a real conflict goes to the user. Agreed expected results are
  the exception: a later instruction that contradicts one is a change to it (Expected results
  first, Change), recorded with the user's yes before anything is checked against it.
- Leave secrets and unrelated work out of the brief.
- Ask with AskUserQuestion if you have it, otherwise as numbered options in the conversation;
  then end your turn and wait.

## Expected results first (opt-in)

Not the default. Use it only when the user asks for it, or a task's `Done when` names expected
results that do not exist yet. A lead may suggest it for a feature the user can see; the user
decides. Without that ask, steps 1 to 6 run as they always have and no file is created.

The expected results live in the feature's run-map file,
`sunstack/verify/<app>/features/<feature>.md`, as its first section:

```markdown
## Expected results

Status: agreed <date> by user, results <hash>

- R1 (feature) A member can invite a teammate by email.
- R2 (UI) At 390 px and 1280 px the invite dialog shows the email field and one primary button "Send invite", with no horizontal scroll.
- R3 (data) After an invite, one pending invitation exists with the email and role, and the Members screen lists it as "Invited".
- R4 (code) The invite endpoint refuses a viewer's request on the server. Checked by: test TestInviteNeedsMemberRole.

Changes:
- <date> R2 reworded from "<old>": results <old hash> to <new hash>, user agreed (<message id or this session>)
```

- **One level per result** (feature, UI, data or code), since the level decides the evidence. A
  code result names its check (`Checked by: test ...` or `Checked by: formal-verify ...`).
- **IDs are stable.** `R<n>` is never reused. A dropped result stays, marked
  `dropped: <date>, user agreed`.
- **Results only:** no implementation plan, no verdicts and no run output.
- **The hash** is the first 8 hex digits of SHA-256 over the section's result lines (the lines
  starting `- R<digit>`), always computed with:
  `awk '/^## Expected results/{f=1;next} /^## /{f=0} f && /^- R[0-9]/' <file> | shasum -a 256 | cut -c1-8`

**Write and agree** (the requester, at the start of the task, never the builder):

1. Skip the delivery identity in step 1: there is no delivery yet. Note the feature and the task.
2. Run steps 2 and 3, level by level: what each role can and cannot do; what the screen shows,
   its text and layout at the widths that matter (390 px and 1280 px unless the result names
   others); what is stored and which other screens show it; which rules the server must enforce
   and how each is checked. For each level, results or a deliberate "none for this feature".
3. Write the section into the feature file (create the file, and the `<app>` folder after the
   data folder Q&A, when missing; add to an existing file, never recreate it). Show it and ask
   the user to agree. Only the user agrees: an agent's yes, or a relayed `user (via ...)`, needs
   the user's confirmation in this session.
4. On yes, write the Status line with the date and the hash, and commit the file.

**Keep it frozen.** The builder treats the section as read-only: a result that looks wrong is a
`question` to the requester or the user, never a rewording. The verifier never edits it.

**Change.** Only the user changes an agreed result: the old and new wording, the user's yes, the
result line edited, a new hash on the Status line and a `Changes:` line with the old and new
hash. Results checked under the old wording are checked again.

**Checking later.** Once a feature has agreed results, every later verification of a delivery on
it is checked against them, not only the task that wrote them. A task checks every agreed
result of its feature unless its `Done when` names fewer. The section is checked only when the
recomputed hash equals the Status hash and the "to" hash of the last `Changes:` line (when there
is one). A mismatch means a result changed without agreement: show it to the user before
anything else.

**Unit tests: fewer, not none.** Expected results are the measure, not a test count. Existing
tests keep passing, and a session still adds a test where the project's conventions or its own
judgment call for it: a rule a walk-through cannot reach (a server refusal, a migration on old
rows, a race), a bug it pins, a refactor it protects.

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

When the feature has agreed expected results, check its hash (Expected results first, Checking
later), then take each result as a requirement, quoted with the source
`<feature file> R<n> (agreed <date>)`; step 3 then asks only about what the file does not
answer, and anything new goes into the file as a change.

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
  `sunstack/verify/<app>/features/`, with `F` its last commit, `git log --oneline F..HEAD -- <the task's files>`
  and uncommitted changes to them. Changed code means "check this feature's instructions", not
  that they are wrong.

No repository, no commit for `C` or `F`, no run map or no file for a feature: say
`unknown` for that line. Each finding is one line under `Verifier freshness:` in the brief's
Context; `fresh` only when every check ran and found nothing.

## 5. Send the task

Write the brief to a file in this session's scratch or temporary folder:

```text
Goal: Check <the product task: its message ID, or a one-line description> against the user's requirements below, the way the user would before accepting it.
Scope: Verify <the task's files and behavior> at <commit> plus <uncommitted files and diff fingerprint, if any>. You may write the project's run map (`sunstack/verify/<app>/`), your evidence folder (`sunstack/_local/verify/<this task's message ID>/`), and your own Sunstack state. Product code, other agents' state and the user's live data stay read-only.
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
  Expected results: <feature file> R<a>-R<b>, results <hash> (only when the feature has them)
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
  on the Done entry (with expected results: `(verified: <counts> by <verifier>, <feature file> R<a>-R<b> @ <commit>)`), and may then send its own `done` reply for the task.
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
   authority rule, and add what it missed, marked as added by you. Verify this task only. When
   the brief names expected results, read them from the feature file, not the brief, and
   recompute the hash as Expected results first describes; a mismatch is a finding to report
   first, not a verdict.
2. **The run map.** For a task that changes something a user runs, use the project's run map:
   `sunstack/verify/<app>/run.md` at the top of the project (several apps: ask the requester
   which). An older project may still have it as a `verify-*` skill under `.claude/skills/` or
   `.agents/skills/`: that is only input for the map. Move it to `sunstack/verify/<app>/`
   (`git mv`, `SKILL.md` to `run.md`, frontmatter dropped, `features/` along), remove its
   `.agents/skills/` symlink, say so in the reply, and work from the moved map. None: create `sunstack/verify/<app>/run.md` (the project data folder Q&A first, when the folder does not exist yet) and commit
   it with the project. It is data this skill reads, not a skill: no frontmatter, five sections
   grounded in this repo, no placeholders: Launch (the exact command from an empty data dir, migrations and seed data
   included; ready means usable, not that a process answers; teardown), Doctor (one
   read-only check that the instance is worth driving), Drive (the repo's real commands, routes
   and handles), Evidence (what to capture; each run keeps it in `sunstack/_local/verify/<task message ID>/`,
   never in the committed map), Cleanup (stop only what the run started; evidence survives). Add `features/README.md` and one file per feature this task
   touches (Sub-features, How to get to it, Driving it, Gotchas); the map grows task by task.
   A feature file that already exists, for example one holding only expected results, gets the
   missing sections added, never recreated.
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
   Evidence by level for expected results: feature, a walk-through as each role; UI,
   screenshots at each width the result names; data, the stored records and the other screens
   that show them; code, the refused request or the check its `Checked by:` names. A unit test
   counts only for a code result that names it.
   A requirement checked with the formal-verify skill answers with the property, the scope (bound
   and assumptions) and the deliberate break the check caught.
5. **Map or product.** Where the run map describes the app wrongly, fix the map, except
   `## Expected results`, which you never edit. Where
   the app breaks an agreed requirement, report it and keep the requirement: never rewrite the
   map to match a regression. Edit nothing outside the brief's Scope.
6. **Reply** with a `done` message to the verification task you received
   (`--reply-to <its message ID>`, never the product task's; send skill) in the brief's
   Report shape, with the identity you tested (commit, diff fingerprint, build or config when
   relevant) and
   `verified: <n> met, <n> partly, <n> not met, <n> changed, <n> pending, <n> unverifiable by <your id>`.
