---
name: whoami
description: Find which Sunstack agent identity this Claude Code or Codex session holds, with its root, id, token and session name, from the session's own ID or tmux pane. Use when the user asks "sunstack whoami", "/sunstack:whoami", "which agent am I", "what is my Sunstack identity", or after compaction or /clear when the session state is missing. Only for projects with a Sunstack team (a sunstack/ folder).
---

# Sunstack: whoami

```sh
sunstack whoami
```

Add `--root <dir>` if the project is not the current folder. Run it on its own.

- **0**: the output is this session's `session state` (root, id, token, task, session name).
  Keep it in the conversation; save, check and release need it. If the session state was lost
  (compaction, `/clear`), run the as skill with that id and `--token` to reload the identity
  files; it resumes the claim and makes no new one.
  - If it says the tmux pane matched a different CLI session (for example before `/clear`),
    confirm with the user in one line before using it.
- **1 `not_found`**: this session holds no identity here. Say so; offer the as skill.
- **1 `no_root`**: no Sunstack team in this folder or above.

Never guess or invent a token.
