---
name: formal-verify
description: "Check one promise of a piece of work against every case in a stated scope, not a sample: code (every order of steps, every input up to a bound), a project design (every state and event), or writing (every requirement, claim and term). The check must catch a deliberate break before it counts. Modes: check (on the fly, leaves nothing), create (leaves a rerunnable formal-verify-<area> skill), maintain (brings a check back in line after changes). Works in any session, with or without a Sunstack team. Use for \"sunstack formal-verify\", \"/sunstack:formal-verify\", \"formal verify\", \"prove this can't happen\", \"model-check this\", \"check every case\", \"is every requirement covered\", \"does the design handle every event\", or when tests or a read-through cannot reach the case that breaks."
---

# Sunstack: formal-verify

Skill only: `/sunstack:formal-verify` in Claude Code, `$sunstack:formal-verify` in Codex. It has no command of its own.

A test or a read-through checks the cases someone thought of. This skill checks every case inside a scope you state, and says what lies outside it. It works on code, on a project design, and on writing. The loop is the same for each; the domain reference says what each step means there.

- Code: `references/code.md`
- Project design (a flow, a protocol, a state machine, a plan): `references/design.md`
- Writing (a proposal, a spec, a paper, docs): `references/writing.md`
- Checkers and how to install them: `references/tools.md`

Pick the mode from the request:

- **check**: answer one question now. Leave nothing in the project except what the user asks for.
- **create**: verify one promise and leave a project-local skill, `formal-verify-<area>`, that lets the next agent rerun the check on a change.
- **maintain**: a `formal-verify-<area>` skill exists and the work changed since it last matched. Bring the check back in line (see Maintain).

## The loop

1. **Pick the promise.** Choose the promise where a break harms the most and ordinary checks reach the least. Start where the user pointed. State the property in one plain sentence, and the harm if it breaks in another. Answer from the work itself; ask the user only what you cannot observe.
2. **Model it truly.** Build the smallest description of the work that can decide the property. Every part points to a line, a section or a requirement that a reviewer can open, in the work's own names. One indivisible step in the work is one step in the model.
3. **Choose the checker.** Let the question pick the tool, not what happens to be installed: the cheapest tool that answers it exhaustively within the bound, or a proof when a bound is not enough (`references/tools.md`). Check whether it is installed (`command -v`). If it is not, tell the user which tool, why it fits better than what is installed, and the exact install command, and ask before installing. If the user declines, use the best installed tool and say in the report what the stronger tool would have covered that this one does not. Say why you chose the tool and what the bound is.
4. **Break it first.** Make a scratch copy of the work with the property broken, such as a removed check, a reordered step, a dropped requirement or an unsourced claim. The check must fail on it. A check you never saw fail proves nothing. Then drop the break.
5. **Run it on the real work.** On a pass, state the scope it covers. On a failure, give the counterexample as a short sequence of steps in the work's own words, then replay it against the real work. If a step cannot happen there, the model is wrong: fix the model and say what was wrong. If every step can happen, it is a finding.
6. **Record it.** Name the files, functions, sections or requirements the check covers, the command that reruns it, and the sync marker: the commit at which you confirmed the model describes the work (a content hash of the checked text when the file is outside git or has uncommitted changes).

## Rules

- **Findings stay findings.** Never weaken the property, the model or the bound until it passes. Do not change the work itself unless the user asks; report the failure.
- **Honest scope.** A pass covers the model and the bound, not the whole system. Name the assumptions and what is left out. For writing, say that this is a stated spec checked exhaustively, not a mathematical proof.
- **Readable without the tool.** The user must be able to trust and act on the result without reading the checker's language.
- **Cheap to rerun.** One command, a fixed tool version, a run short enough to use on every relevant change.
- **Scratch stays scratch.** Breaks, models and scripts for a check go in a scratch folder or a copy of the work, never in the original, unless the mode is create.

## Create: the formal-verify-<area> skill

Write it at `.claude/skills/formal-verify-<area>/SKILL.md` in the project root, with a relative symlink at `.agents/skills/formal-verify-<area>` when that path is free, so Claude Code and Codex both find it. It needs frontmatter that names the area and says when to use it. Write it for an agent who reads it cold, mid-change. From that skill alone, the next agent must be able to tell whether a change touches the covered work, run the check, tell a real finding from a modeling error, and update the model when a change is intended. Put in the property and its harm, the coverage record, the command, the bound, and the sync marker. Use the project's real paths, commands and names; leave no placeholders.

Before you hand it over, follow it once as the next agent would, on a scratch break of the real work. It must catch the break. Then drop the break and confirm the check is back where it started. A skill that was never run is a draft.

## Maintain

Find the `formal-verify-<area>` skills (or the one the user named). If none exists, offer create mode. Run each check once as it is and record the result: a pass now only says the old description still holds. Read every change to the covered work since the sync marker, including moved or renamed parts and new parts that reach the guarded state. When no marker is recorded, use the last commit that changed the check's files, and say so. Update the model from the work, not from the result. Change the property only for a promise that changed on purpose, with the evidence (a commit message, a changelog, the user's words). Keep the bound unless the change makes it wrong, and say so when you reduce it. Rerun, and break the updated model once to show it can still fail. Give each check one outcome:

- **in sync**: nothing since the marker changes what the check describes, and it passes.
- **updated**: the model now describes the changed work, and it passes.
- **bug**: the model describes the work, and the work breaks the property. Name the change that introduced it. A check can be both updated and bug.
- **blocked**: it could not be brought back in line or run. Say why, and do not move the marker.

Move the sync marker only after you confirm the model describes the work at the new marker. Keep model updates in a change of their own, apart from any fix to the work.

## With a Sunstack team

None of this needs a team. In a project with a `sunstack/` folder:

- The verify skill collects a task's requirements. Requirements shaped like properties ("always", "never", "every", "none", "at the same time") suit this skill: the verifier checks them with it and answers per requirement with the property, the scope and the break it caught.
- `formal-verify-*` skills are not the project's `verify-<app>` skill: they model one promise, they do not drive the app.
- An agent that owns a `formal-verify-<area>` skill keeps its sync marker on its board, and runs maintain when a delivery touches the covered work.

## Report

Tell the user, in plain words: the property and its harm, the tool and why, the bound and assumptions, how many items the bound holds and how many of them the check could decide, the break the check caught, the result (each finding as steps or an item in the work's words, with what the work actually does), how to rerun it, and what it does not cover. Suggest a second target only if you found a close one.
