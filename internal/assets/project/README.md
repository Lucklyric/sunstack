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

Commands and skills (Claude Code `/sunstack:<skill>`, Codex `$sunstack:<skill>`):

- Check the setup and what to do next: `sunstack health`, skill `checkup`
- Take on an agent: `sunstack as`, skill `as`; save and hand back: skills `save` and `release`
- See and align the team's work: `sunstack board`, skill `board`
- Give the team a directive: `sunstack direct`, skill `direct`
- Add, rename or remove an agent: `sunstack hire`, `rename`, `fire`; skills `recruit` and `fire`
- Talk between agents: `sunstack send`, `check`, `take`, `ack`; skills `message` and `check`
- Start or stop agent sessions in tmux: `sunstack sessions`, `spawn`, `dismiss`; skill `spawn`
- Watch the team: `sunstack team`, `sunstack log`, `sunstack tui`

Review changes to this folder like code: pillars, boards and context shape how the agents behave.
