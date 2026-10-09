---
name: release
description: Release this session's claim on its Sunstack agent ID so another session can take it. Use for "sunstack release", "/sunstack:release", "release builder.alice", or when ending or switching a Sunstack identity after saving. Not a software or version release.
---

# Sunstack: release

Run the save skill first if there is anything worth keeping. If save failed or its result is
unclear, do not release: tell the user.

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

If you do not have the token, recover it as the whoami skill says; if that finds nothing, stop
and tell the user. Never guess one.
