# Sunstack team

This folder holds the project's agent team, managed by [Sunstack](https://github.com/Lucklyric/sunstack).

- `BOARD.md`: your objectives, your own key results, and your directives to the team.
- `PILLARS.md`: rules for every agent in this project.
- `<id>/AGENT.md`: one agent's role and way of working. `<id>` is `<title>.<name>`: the title is the role, the name is this one agent.
- `<id>/pillars.md`: rules for that agent only (optional).
- `<id>/board.md`: that agent's key results (Now, Next, Done), each serving one of your objectives.
- `<id>/context.md` and `<id>/threads/`: what that agent has learned.
- `<id>/archive/` and `archive/`: finished and outdated entries by month, for reference only.
- `PROTOCOL.md`: how agents read and write these files.
- `_local/`: claims, messages, locks and the event log on this machine. Not committed.

Every entry starts with a date, so you can see how fresh it is. Agents update their own boards and context automatically. Changes to objectives, `PILLARS.md`, `pillars.md` or `AGENT.md` happen only after you approve them.

Commands and skills: each skill has the name of the command it runs, `sunstack <name>` in a terminal, `/sunstack:<name>` in Claude Code, `$sunstack:<name>` in Codex. `save` and `verify` are skills only.

- What to do next: `next` (this team; every team: `sunstack org --attention`)
- Set up, check and upgrade: `health` (also `init`, `update`, `migrate`)
- Take on an agent: `as`; save and hand back: `save`, `release`
- See and align the team's work: `board`
- Give the team a directive: `direct`
- Add or remove an agent: `hire`, `fire` (rename: `sunstack rename`, skill `health`)
- Send work or a question to an agent or session: `send`; read your inbox: `check` (with `take`, `ack`)
- Start or stop agent sessions in tmux: `spawn` (also `team --sessions`, `dismiss`, `kill`)
- Close sessions idle over 24h and panes whose CLI exited: `clean`
- Review staffing and suggest hires: `hr`
- Check a task against your requirements before you accept it: `verify`
- Watch the team: `sunstack team`, `sunstack log`, `sunstack tui`

Review changes to this folder like code: pillars, boards and context shape how the agents behave.
