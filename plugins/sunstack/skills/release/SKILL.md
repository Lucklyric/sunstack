---
name: release
description: Release this session's claim on its Sunstack agent ID so another session can take it. Use for "sunstack release", "/sunstack:release", "release builder.alice", or when ending or switching a Sunstack identity after saving. Not a software or version release.
---

# Sunstack: release

Command: `sunstack release`. Skill: `/sunstack:release` in Claude Code, `$sunstack:release` in Codex.

Run the save skill first if there is anything worth keeping. If save failed or its result is
unclear, do not release: tell the user.

Release returns every task or handoff this session still holds to the agent's inbox, marked
`returned:`. Before releasing, finish each one, or send its sender an `fyi --reply-to <id>`
with `not finished, returned: <state and next step>` (the send skill; an `fyi`, since a `done`
would close the task), so the sender can judge whether it still holds.

Then, with the root, id and token from the `session state` block that `sunstack as` printed in
this session, run this on its own:

```sh
sunstack release --root "<root>" "<id>" --token "<token>"
```

- **0**: released. From now on this session has no Sunstack identity: stop following that
  agent's pillars and identity, and forget the token.
- **4 `token`**: this session no longer holds the claim (someone took over). Tell the user;
  there is nothing to release.
- **4 `busy`**: retry once, then tell the user.
- **1 / 2**: show the error. Do not work around it.

If you do not have the token, recover it as the as skill says (Recover this session's identity); if that finds nothing, stop
and tell the user. Never guess one.
