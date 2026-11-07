---
name: formal-verify
description: "Check one promise of a piece of work against every case in a stated scope, not a sample: code (every order of steps, every input up to a bound), a project design (every state and event), or writing (every requirement, claim and term). The check must catch a deliberate break before it counts. Modes: check (on the fly, leaves nothing), create (leaves a rerunnable check in the project's .formal-verify/ folder), maintain (brings a check back in line after changes). Works in any session, with or without a Sunstack team. Use for \"sunstack formal-verify\", \"/sunstack:formal-verify\", \"formal verify\", \"prove this can't happen\", \"model-check this\", \"check every case\", \"is every requirement covered\", \"does the design handle every event\", or when tests or a read-through cannot reach the case that breaks."
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
- **create**: verify one promise and leave a check at `.formal-verify/<area>/` in the project, so the next agent can rerun it on a change.
- **maintain**: a check exists under `.formal-verify/` and the work changed since it last matched. Bring the check back in line (see Maintain).

## The loop

1. **Pick the promise.** Choose the promise where a break harms the most and ordinary checks reach the least. Start where the user pointed. State the property in one plain sentence, and the harm if it breaks in another. Answer from the work itself, including its design docs and notes; ask the user only what you cannot observe. If the promise as asked is false by design, narrow it to the one the work makes and say why. A promise about history ("after the user closes it, it stays closed") needs a history variable in the model: set where the decision is made, cleared where the work reports a failure.
2. **Model it truly.** Build the smallest description of the work that can decide the property. Every part points to a line, a section or a requirement that a reviewer can open, in the work's own names. One indivisible step in the work is one step in the model. Find every writer of the guarded state, also outside the files you were pointed at: the root cause is often in another one.
3. **Choose the checker.** Let the question pick the tool, not what happens to be installed: the cheapest tool that answers it exhaustively within the bound, or a proof when a bound is not enough (`references/tools.md`). Check whether it is installed (`command -v`). If it is not, tell the user which tool, why it fits better than what is installed, and the exact install command, and ask before installing. If the user declines, use the best installed tool and say in the report what the stronger tool would have covered that this one does not. Say why you chose the tool and what the bound is.
4. **Break it first.** Break the property the way a plausible change would: a removed check, a reordered step, a dropped requirement, an unsourced claim. When the check runs the real work, break a scratch copy of it; when it runs a hand-written model, break the model the same way. The check must fail on it. A check you never saw fail proves nothing, and a hand-written checker is code that can be wrong: a pass before any break failed is suspect. If a break passes because the checker was broken, fix the checker and say so. Then drop the break.
5. **Run it on the real work.** On a pass, state the scope it covers. On a failure, give the counterexample as a short sequence of steps in the work's own words, then replay it against the real work. If a step cannot happen there, the model is wrong: fix the model and say what was wrong. If every step can happen, it is a finding. A step that only happens in a narrow timing window: widen the window and look for other actors on the same resource before you change the model. When a live replay is impractical, walk the steps through the code, and label the replay as a code walk. To look past a first finding, exclude it with a named switch and run again; that run is never a pass. A checker that stops at the first violation reports the states it visited, not coverage.
6. **Record it.** Name the files, functions, sections or requirements the check covers, the command that reruns it, and the sync marker: the commit at which you confirmed the model describes the work (when the file is outside git or has uncommitted changes, a content hash of the checked text plus a copy of it kept with the check, so a later maintain can diff against it). Before you record it, check that the work has not moved since you ran (`git rev-parse HEAD`), and record the marker that still holds. For a hand-written checker, record the toolchain version.

## Rules

- **Findings stay findings.** Never weaken the property, the model or the bound until it passes. Do not change the work itself unless the user asks; report the failure.
- **Honest scope.** A pass covers the model and the bound, not the whole system. Name the assumptions and what is left out. For writing, say that this is a stated spec checked exhaustively, not a mathematical proof.
- **Readable without the tool.** The user must be able to trust and act on the result without reading the checker's language.
- **Cheap to rerun.** One command, a fixed tool version, a run short enough to use on every relevant change.
- **Scratch stays scratch.** Breaks, models and scripts for a check go in a scratch folder or a copy of the work, never in the original, unless the mode is create.

## Create: the check folder

All checks of a project live in one folder at the top of the project, `.formal-verify/`, one subfolder per area. Nothing else in the project changes. Each area folder holds `check.md` and the files it needs (a model, a script, a kept copy of checked text):

```
.formal-verify/
  lock/
    check.md
    model.go
    go.mod
```

The folder is data, not a skill: this skill reads and runs it, in Claude Code and Codex alike. A model in the project's own language gets its own module or package file (`go.mod`, `pyproject.toml`) so the project's build and tests never pick it up. Commit the folder with the work, so other sessions and hosts find it.

Write `check.md` for an agent who reads it cold, mid-change. Start it with `# <area>: <property in one sentence>`. From it alone, the next agent must be able to tell whether a change touches the covered work, run the check, tell a real finding from a modeling error, and update the model when a change is intended. Put in the property and its harm, the bound and assumptions, the command (run from the area folder, with the tool version), each break it catches and how to apply it, and the coverage record, which `sunstack next` reads to flag a stale check:

```
## Coverage

sync: <commit>
covers: internal/core/lock.go internal/core/prefs.go
```

`sync:` is the marker from step 6. For work outside git or with uncommitted changes, it is `sha256:` followed by the output of `shasum -a 256 <the covered files, in the covers order> | shasum -a 256` run at the top of the project (the first field only), and the kept copy goes in the area folder. `covers:` lists the covered files, relative to the top of the project. Use the project's real paths, commands and names; leave no placeholders.

Before you hand it over, follow it once as the next agent would, on a scratch break of the real work. It must catch the break. Then drop the break and confirm the check is back where it started. A skill that was never run is a draft.

## Maintain

Find the checks under `.formal-verify/` (or the one the user named). If none exists, offer create mode. Run each check once as it is and record the result: a pass now only says the old description still holds. Read every change to the covered work since the sync marker, including moved or renamed parts and new parts that reach the guarded state. When no marker is recorded, use the last commit that changed the check's files, and say so. When the marker is a hash with no kept copy, the changes cannot be read: map the model against the work again from scratch, and say that the change history is unknown. Update the model from the work, not from the result. Change the property only for a promise that changed on purpose, with the evidence (a commit message, a changelog, the user's words). Keep the bound unless the change makes it wrong, and say so when you reduce it. Rerun, and break the updated model once to show it can still fail. Give each check one outcome:

- **in sync**: nothing since the marker changes what the check describes, and it passes.
- **updated**: the model now describes the changed work, and it passes.
- **bug**: the model describes the work, and the work breaks the property. Name the change that introduced it. A check can be both updated and bug.
- **blocked**: it could not be brought back in line or run. Say why, and do not move the marker.

Move the sync marker only after you confirm the model describes the work at the new marker. Keep model updates in a change of their own, apart from any fix to the work.

## With a Sunstack team

None of this needs a team. In a project with a `sunstack/` folder:

- The verify skill collects a task's requirements. Requirements shaped like properties ("always", "never", "every", "none", "at the same time") suit this skill: the verifier checks them with it and answers per requirement with the property, the scope and the break it caught.
- A check under `.formal-verify/` is not the project's `verify-<app>` skill: it models one promise, it does not drive the app.
- `sunstack next` flags each check whose covered files changed since its sync marker. An agent that owns a check runs maintain when a delivery touches the covered work.

## Report

Tell the user, in plain words: the property and its harm, the tool and why, the bound and assumptions, how many items the bound holds and how many of them the check could decide, the break the check caught, the result (each finding as steps or an item in the work's words, with what the work actually does), how to rerun it, and what it does not cover. Suggest a second target only if you found a close one.
