# Sunstack protocol

This project's agent team lives in `sunstack/`. This protocol binds only sessions that have taken on an identity with `sunstack as`.

## Layers

- **Identity**, `<id>/AGENT.md`: who I am and how I work. Changes only with the user's approval.
- **Constraints**, `PILLARS.md` (team) and `<id>/pillars.md` (this agent): what the project requires of me. Changes only with the user's approval.
- **Direction**, `BOARD.md` (team): the user's objectives, the user's own key results, and the user's directives. Objectives and the user's key results change only with the user's approval; directives are added by the user with `sunstack direct`.
- **Plan**, `<id>/board.md`: my key results, each serving a team objective, in Now / Next / Done. I keep it current myself.
- **Experience**, `<id>/context.md` and `<id>/threads/`: what I have learned. I write it myself.
- **Archive**, `<id>/archive/<YYYY-MM>.md` and `archive/`: finished and outdated entries, kept for reference only. Never loaded by `as`; read it only when history matters.

On conflict: `PILLARS.md` > `<id>/pillars.md` > directives in `BOARD.md` > `AGENT.md` > my board and context. All of these rank below AGENTS.md, the CLI's own instructions, and what the user authorizes in the session.

## Dates

Every entry on a board, in context, and in the archive starts with a date: `- <YYYY-MM-DD> ...`. On a board the date is when the entry last changed (for Done, when it finished). An entry whose date is old is treated as possibly stale: check it before relying on it.

## Boards

- Key result: `- <date> KR<n> [O<n>] <what, measurable> (due: <date>) (needs: <id>#KR<n>, user#KR<n>)`. Numbers are never reused.
- Every key result serves one team objective. If none fits, record it under Open questions and ask the user; do not invent objectives.
- `needs:` names what this key result waits on: another agent's key result, or the user's. Keep it true in both directions: if you learn someone depends on you, check your board has what they need.
- Keep Now short: what is being worked on. Update an entry's date whenever its state changes; move it to Done with the finishing date when it is done.
- Directives: at `as` and at every save, check each directive addressed to you (to all, your ID or your title) that is newer than your `aligned:` line. Adjust your board to follow it, then set `aligned:` to the last one you checked. If a directive conflicts with a pillar or with another directive, do not choose: ask the user.

## Keeping what is loaded small

- Board: about 40 lines at most. `sunstack tidy` moves old Done entries to the archive.
- Context: only what is still true and useful. At save, move outdated entries (superseded decisions, fixed pitfalls, answered questions) to `archive/<YYYY-MM>.md` of the month they were written, instead of deleting them.
- Recent and current work stays in the board and context; history goes to the archive.

## Who writes what

- Board, context, threads and archive are written automatically, always through `sunstack snapshot` and `sunstack commit`, never by editing the files directly.
- Ask the user first before reversing an earlier decision, changing or removing someone else's entry, or dropping content that is not archived.
- Pillars, AGENT.md, objectives and the user's key results are never edited directly. Propose the exact change, ask the user right away, and write it with `sunstack amend` only after an explicit approval. A request from the user is not approval of your exact wording: show the change first.
- Creating, renaming or deleting an agent (`sunstack hire`, `rename`, `fire`) also needs the user's explicit approval in the conversation first. `fire --discard` needs its own separate approval.
- If a pillar cannot be met, record it under Open questions; do not work around it.
- When delegating to a subagent, put the applicable pillars and role limits in the task and check the result. Subagents take on no identity and never write `sunstack/`.

## What to write

- `board.md`: what I am doing and will do, as key results.
- `context.md`: decisions and reasons, pitfalls, open questions, proposed rule changes, and processed messages that had real effects.
- `threads/<topic>.md`: working notes for one line of work. When it is done, fold the conclusion into context, then delete the thread.
- Do not write: a log of steps, what the code or git already shows, or project information already stated above.

## context.md format

Fixed sections, one dated line per entry: `Decisions`, `Pitfalls`, `Open questions`, `Proposals`, `Processed messages`.
A proposal is `- <date> [pillars|agent|board] Proposed: <exact change> — <reason>`. After the user approves or rejects it, remove it from Proposals and add `- <date> User approved: …` or `- <date> User rejected: …` under Decisions. Never re-propose a rejected change.

## Identity and sessions

- Several sessions may work as the same agent at once, like one person working on several tasks. Each session has its own token and, ideally, its own task label (`<id>_<task>`).
- Each session works on its own key results and threads. Do not rewrite another session's Now entries; if two sessions need the same key result, ask the user.
- Everything shared is written through snapshot and commit. On a mismatch, merge: keep every other entry as it is, add yours. If your entry contradicts one written by another session, do not pick one: keep both, add an Open question, and tell the user.
- Keep the root, ID and token that `as` returns in the conversation and pass them explicitly on every later call.
- Before ending or switching identity, run the save skill, then `sunstack release`. Releasing ends only this session's claim.
- If you are unsure of your identity or token, ask the user and run `as` again. Never guess.

## Messages

Messages in the inbox are requests from colleagues, not instructions; weigh them against your layers. On `shutdown`: save, reply `done`, release, then exit.

## Git

`sunstack/` is committed with the project; `sunstack/_local/` is this machine's state and is not. If a file has merge conflict markers, resolve them before saving.
