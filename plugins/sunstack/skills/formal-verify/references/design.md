# Project design

Use this when the promise is about how a flow, protocol, plan or system is meant to behave, before or beside the code. The design may live in a doc, a diagram, a board, or the code itself.

## Properties that earn a formal check

- Completeness: every state meets every event with a defined outcome. No case is "we will see".
- Safety: a bad state is never reached ("two leases are never held", "a closed window never reopens on its own").
- Progress: from every state the work can reach an end state (no stuck flag, no wait that never clears).
- Contract: each interface states what it requires and what it ensures, and every caller meets the requirement.

## Model

- List the states and events in the design's own names. Add the outside events that people forget: a crash, a restart, a timeout, a second user, a retry, a step done twice, steps in the other order.
- For each state and event, write the outcome. A blank cell is a finding before any checker runs.
- Write each safety rule as an invariant over the state.

## Checker

- **State × event table:** check that every pair has an outcome and that no outcome contradicts another. A short script over the table, or a careful pass with each cell cited, is exhaustive for this question.
- **Reachable states:** when outcomes chain, explore every reachable state from the start states, up to a bound, and check each invariant in each state (the same breadth-first explorer as in `code.md`, over the design's states). TLA+ with TLC suits it when a Java runtime is present.
- **Contracts:** list each interface's requires and ensures, then every caller, and check each caller meets each requires.

## True to the design

Each state, event and rule points to the sentence, section or diagram it comes from. When the design is silent, say so as a finding; do not fill the gap with your own reading.

## Breaks to try

Delete one transition, swap two steps, drop a guard, remove the restart event. The check must catch it.

## Counterexample

Tell it as a path the reader recognizes: "1. the job is running, 2. the server restarts, 3. the scheduler restores its queue, the old job comes back beside the new one".

## Rerun record

The table or model file, the command, the bound, the design sections covered, and the sync marker (commit, or a content hash plus a copy of the checked text for a doc outside git).
