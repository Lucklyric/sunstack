#!/bin/sh
# Adds the SHA-256 of every PROTOCOL.md template in this repo's history to
# internal/assets/project/protocol-history.txt, oldest first (design §35).
# It only ever adds: a hash once listed stays, so an unedited older copy keeps
# refreshing on its own. Run it after changing PROTOCOL.md; commit both.
set -eu
cd "$(dirname "$0")/.."
if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
	echo "protocol-history: this clone is shallow; run git fetch --unshallow first" >&2
	exit 1
fi
f=internal/assets/project/PROTOCOL.md
out=internal/assets/project/protocol-history.txt
tmp=$(mktemp)
{
	echo "# SHA-256 of every built-in PROTOCOL.md, oldest first; written by scripts/protocol-history.sh"
	[ -f "$out" ] && grep -v '^#' "$out"
	for c in $(git log --reverse --format=%H -- "$f"); do
		git show "$c:$f" | shasum -a 256 | cut -d' ' -f1
	done
	shasum -a 256 "$f" | cut -d' ' -f1 # the working copy, if not committed yet
} | awk 'NF && !seen[$0]++' > "$tmp"
mv "$tmp" "$out"
echo "wrote $out ($(grep -vc '^#' "$out") hashes)"
