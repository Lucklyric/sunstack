# Verifier's procedure

For the agent that received a verification task. Do not run the numbered steps of the verify skill's `SKILL.md`; its sections "The verification document" and "Expected results first" give the shapes this procedure names.

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
   Write the baseline conditions, and each feature's preconditions and scenarios, in the shape
   "The verification document" gives. A feature file without them gains them now. The builder
   of the delivery never writes or changes its scenarios.
   A feature file that already exists, for example one holding only expected results, gets the
   missing sections added, never recreated.
   A new or changed run map runs once from launch to cleanup before it counts. If that
   run used a stand-in for the real target (a container for a fresh machine, a local stack for
   a remote one), list each setup step the stand-in did that the target will not; they stay
   unverified until a run on the target.
3. **Drive an instance you own**, with its own data, never one the user or another agent is
   using; if you cannot get one, the requirement is `unverifiable` with that prerequisite. Run
   Doctor before the first drive and after any drive that fails. Then confirm the baseline
   conditions, and for each feature its preconditions, each by its read-only look. Drive the
   feature's scenarios in order from the baseline state, and restore the seeded data after a
   scenario that changed any; relaunch only when it cannot be restored. Prefer the real user path over
   internal setters; check side effects (files, rows, messages) as well as what is visible.
   Keep, per scenario, the action and the state after it, not only the last screen, and for a
   change to stored data its second view. Name each artifact with the feature and `S<n>`.
   Never kill a process by name.
4. **Verdicts**: `met`, `partly` (what is missing), `not met` (what happened instead), `changed`
   (the delivery departs on purpose: cite the user's agreement), `pending` (it depends on what
   the user meant: raise an ask as the save skill describes and name it `<your id>#Q<n>`) or
   `unverifiable` (the prerequisite and the route tried). Say how each was checked (live, tests
   or read) and which scenarios prove it. A requirement about something a user runs (a feature,
   UI or data result) is `met` only when a scenario was driven live: tests or a read may support
   it, never replace it. A check that could not run is `unverifiable`, never `met`. An entry
   point that was not driven is reported as skipped with the reason, never as verified through
   another path. Scenarios the brief left out are listed as "not in scope". Seed data that lacks a journey's precondition makes it `unverifiable`, with the
   reason in that feature's Gotchas: never weaken the journey to make it pass (a scenario is a
   journey written down). Work the user
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
