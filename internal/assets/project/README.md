# Sunstack team

This folder holds the project's agent team, managed by [Sunstack](https://github.com/Lucklyric/sunstack).

- `PILLARS.md`: rules for every agent in this project.
- `<id>/AGENT.md`: one agent's role and way of working. `<id>` is `<title>` or `<title>.<name>`.
- `<id>/pillars.md`: rules for that agent only (optional).
- `<id>/context.md` and `<id>/threads/`: what that agent has learned.
- `PROTOCOL.md`: how agents read and write these files.
- `_local/`: claims, messages, locks and the event log on this machine. Not committed.

Agents update their own context automatically. Changes to `PILLARS.md`, `pillars.md` or `AGENT.md` happen only after you approve them.

Useful commands: `sunstack team`, `sunstack log`, `sunstack tui`, `sunstack health`. In Claude Code, take on an agent with `/sunstack:as <id>`; in Codex, `$sunstack:as <id>`.

Review changes to this folder like code: pillars and context shape how the agents behave.
