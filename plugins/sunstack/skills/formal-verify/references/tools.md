# Tools

Pick by the question. Check each with `command -v` before you use it; when the right one is missing, give the user the install line below and ask first. Install commands assume macOS with Homebrew; on another system use the tool's own instructions.

## Bounded: every case up to a size

- **Hand-written state explorer** in the project's language (see `code.md`). Every order of steps up to a bound. Needs nothing.
- **TLA+ with TLC**: every interleaving of a design or protocol model, with a standard notation others can read. Needs a Java runtime. Install: `brew install openjdk` and the `tla2tools.jar` release from github.com/tlaplus/tlaplus. Apalache (`brew install apalache`) checks the same specs symbolically, for larger bounds.
- **Alloy**: every structure up to a size (relations, permissions, schemas, configurations). Install: the Alloy app from alloytools.org (needs Java).
- **Fuzzing and property tests on the real code**: Go `go test -fuzz` (built in); Python Hypothesis (`uv add --dev hypothesis`); Rust proptest (`cargo add --dev proptest`).
- **Bounded code verifiers**: Kani for Rust (`cargo install --locked kani-verifier && cargo kani setup`); CBMC for C (`brew install cbmc`).

## Unbounded: every case, any size

- **Lean 4**: a machine-checked proof of a theorem about a model (an algorithm, a protocol invariant, a paper's math). Proves the Lean model, not the source code, so state how the model matches the code. Install: `curl https://raw.githubusercontent.com/leanprover/elan/master/elan-init.sh -sSf | sh`, then `lake new <name> math` for a project with Mathlib.
- **Dafny**: write the code with its specification and get it verified, then compile to Go, C#, Java, Python or JavaScript. Install: `brew install dafny`.
- **Z3 (SMT)**: decide a formula over integers, bit vectors, arrays (an index computation, a config constraint). Install: `brew install z3`, or `uv add z3-solver` for Python.

## Choosing

- Bad order of steps: state explorer, or TLA+ when the model is large or will be shared.
- Every input to real code: fuzzing or property tests; Kani or CBMC where they apply.
- Must hold at every size: Lean or Dafny. Cost is high (the proof often outgrows the code), so say so and ask the user before starting.
- A constraint or arithmetic question: Z3.
- Writing: the lists and scripts in `writing.md`; no external tool is needed.
