#!/bin/sh
# Writes internal/assets/project/protocol-history.txt: the SHA-256 of every
# PROTOCOL.md template in this repo's history, oldest first (design §35).
# Run it after changing internal/assets/project/PROTOCOL.md and commit both.
set -eu
cd "$(dirname "$0")/.."
f=internal/assets/project/PROTOCOL.md
out=internal/assets/project/protocol-history.txt
{
	echo "# SHA-256 of every built-in PROTOCOL.md, oldest first; written by scripts/protocol-history.sh"
	for c in $(git log --reverse --format=%H -- "$f"); do
		git show "$c:$f" | shasum -a 256 | cut -d' ' -f1
	done
	# The working copy, when it is not committed yet.
	shasum -a 256 "$f" | cut -d' ' -f1
} | awk '!seen[$0]++' > "$out"
echo "wrote $out ($(grep -vc '^#' "$out") hashes)"
