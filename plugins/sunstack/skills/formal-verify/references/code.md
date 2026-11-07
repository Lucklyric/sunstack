# Code

Use this when the promise is about what a program does.

## Properties that earn a formal check

- Ordering: steps of two processes, goroutines, requests or retries can interleave so a rule breaks (lost update, double open, stuck flag, deadlock).
- Every input: a parser, an encoder, an index computation, a state machine fed by outside data.
- Safety: overflow, out-of-bounds, nil use, unsafe code.
- Agreement: two implementations, or old and new code, must give the same result.

## Pick the checker by the question

Prefer what the project already uses. When the right tool is not installed, ask the user to install it (install lines in `tools.md`).

- **Can steps happen in a bad order?** Write a small model of the steps and explore every interleaving, up to a bound on processes and steps. Without extra tools, write the explorer in the project's language: the state as a struct, each step as a function that returns the next states, a breadth-first walk over all reachable states that checks the invariant in each and keeps the path to the first state that breaks it. TLA+ with TLC suits the same question when a Java runtime is present.
- **Does the code hold for every input?** Run the real code, not a hand model of it. For an exhaustive verdict, either enumerate every input of a stated finite domain (every byte string up to 4 bytes, every pair of options) in a test over the real function, or use a code-level verifier (Kani, CBMC, Dafny, an SMT-backed tool) when the language has one. Fuzzing (`go test -fuzz`) and property tests (Hypothesis, proptest) sample inputs: use them as extra evidence beyond the bound, and when they are all you have, report the check as not exhaustive.
- **Do real goroutines or threads race?** Add the race detector (`go test -race`) or the language's sanitizer to a test that drives the concurrent path.
- **Must it hold at every size, where a bound is not enough?** That needs a proof: Lean 4 for a theorem about a model of the algorithm, Dafny to verify the code with its specification. Say so, and ask the user before you start one.

## True to the code

- Each model step names the function and line it stands for. A file rename or a single system call is one step; two calls are two steps. A section run under a lock may be one step, but only when every competing writer of that state takes the same lock.
- Each model variable stands for real state: a field, a file, an option, a row.
- Include the outside actors that matter: a second process, a crash between two writes, a timeout, a user action.

## Breaks to try

Remove a lock or a check, reorder two writes, drop a retry limit, widen a type, delete an error branch. The check must fail on each break you claim it guards.

## Counterexample

Tell it as numbered steps in the code's words: "1. process A reads the file, 2. process B reads it, 3. A writes, 4. B writes, A's change is lost". Then replay it on the real code, as a test or a live reproduction. If it does not reproduce, suspect the model first.

## Rerun record

The command (for example `cd .sunstack/formal-verify/lock && go run .`), the Go or tool version, the bound, the covered files and functions, and the sync commit.
