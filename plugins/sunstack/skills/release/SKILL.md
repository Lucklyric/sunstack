---
name: release
description: Release this session's claim on its Sunstack agent ID so another session can take it. Use when the user says "release", "/sunstack:release", or when ending or switching a Sunstack identity after saving.
---

# Sunstack: release

## Script path

`${CLAUDE_PLUGIN_ROOT}` below is this plugin's root folder. If it still appears literally
(not replaced by a real path), use the folder two levels above this SKILL.md instead.

Run the save skill first if there is anything worth keeping. Then, with the root, id and token
from the `session state` block of the as skill in this session:

```sh
sh "${CLAUDE_PLUGIN_ROOT}/scripts/release.sh" --root "<root>" "<id>" --token "<token>"
```

- **0** — released. From now on this session has no Sunstack identity: stop following that
  agent's pillars and identity, and forget the token.
- **4 `token`** — this session no longer holds the claim (someone took over). Tell the user;
  nothing to release.
- **4 `busy`** — retry once, then tell the user.

If you do not have the token, stop and tell the user. Never guess one.
