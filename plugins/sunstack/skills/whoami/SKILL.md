---
name: whoami
description: Find which Sunstack agent identity this session holds (root, id, token, session name) from its session ID or tmux pane. Use for "sunstack whoami", "/sunstack:whoami", "which agent am I", or after compaction or /clear lost the session state. Not for choosing an agent to become (the as skill).
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
