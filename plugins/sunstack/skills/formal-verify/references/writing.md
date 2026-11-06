# Writing

Use this when the promise is about a document: a proposal, a spec, a paper, a report, docs, an email, a design note checked against the code it describes. Here "formal" means a stated spec checked against every item in the document, not a mathematical proof. Say that in the report.

## Properties that earn a formal check

Pick one, as SKILL.md step 1 says; build only the lists that property needs.

- Coverage: every requirement in the brief, call or template is answered by a passage.
- Grounding: every factual claim, number and citation is true of its source (a paper, a dataset, the code).
- Consistency: each term, number and name is the same everywhere it appears; no two passages contradict each other.
- Form: every rule of the house style or the venue holds (limits, sections, banned patterns).

## Model

The bound is the items of the document (or the section you name) plus the sources they point to. Extract every item with a script, one row each, with its location:

- Split into sentences on sentence-ending punctuation followed by a space and any character, not only a capital (code names start in lowercase). A heading is one item. A list item is one item.
- Give each item a kind from a fixed set. For grounding: `claim` (says how the source is now), `history` (what happened then), `process` (what someone did), `external` (a fact about the world or another tool), `heading`, `intent`. Every `claim`, `history`, `process` and `external` row states a fact and is checked against its source (the log, the commit, the other tool's docs); only `heading` and `intent` rows state none. When some facts cannot be checked (no source exists), say so and narrow the property before you check, never after.
- A sentence that a later passage of the same document says was replaced is `history`. Note it when a reader who stops early would be misled.
- **Requirements:** quoted from the brief, the call, the template or the user's words, with their source.
- **Terms:** each defined term, acronym, product name and key number, with every place it appears.

Anchor each row to its whole sentence (or a hash of it), so any edit to the sentence shows as drift.

## Checker

- **Coverage matrix:** for each requirement, the passage that meets it, or "missing".
- **Claims ledger:** for each row that states a fact, its source and whether the source says it. When the source is code, cite `file:line` and the text there. Whether the code does what the sentence says is your reading; record it once per row. A script then checks what is mechanical: every item has exactly one row, every anchor still matches its sentence, every cited source line still says what it said. Say in the report that the script checks completeness and drift, not truth.
- **Term table:** each term with every spelling and value found. More than one is a finding unless the document explains it.
- **Deterministic linters:** run the project's own linters and any check the writing already has (a word counter, a citation checker). Write a short script for a rule that can be checked mechanically (banned words, sentence length, section order).

Check each list against the whole document or section. Do not sample.

## Breaks to try

On a copy, never the original. Pick the breaks that match the lists you built:

- Delete a passage that meets a requirement (coverage).
- Change one number in one place (grounding, consistency).
- Add a claim with no source (grounding).
- Remove a citation (grounding, when there are citations).
- For a document checked against code: change the code under a claim, in a copy of the code.

## Counterexample

Name the item, where it fails, and what the source actually says or does: "section 3 says the details pane keeps 30 columns; `ui/layout.go:118` keeps 30 for the pane and its borders, so the text gets 24". When the source is code, replay the claim on the real program where you can (a test, a run on a private instance) and give that as evidence.

## Rerun record

In check mode, keep the lists and scripts in a scratch folder, not beside the document. In create mode they go beside the document or in the `formal-verify-<area>` skill. Record the script command, the document version (the commit; a content hash plus a copy of the checked text when the file is outside git or has uncommitted changes), the source version, and what was not checked (the truth of a source itself, `heading` and `intent` rows, facts with no source).

## Report

Give counts per verdict (for example "92 items: 47 claims, 45 true or partial; 45 non-claims: 26 history, 7 process, ..."), so the reader sees how much of "every item" was checkable.
