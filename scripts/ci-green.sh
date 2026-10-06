#!/usr/bin/env bash
# ci-green.sh <commit> [--wait]
#
# Exits 0 only when the "ci" workflow run for <commit> on main has finished
# and every OS job (ubuntu, macos, windows) passed. Anything else exits 1 and
# says why: no run yet, still running, a failed or missing job.
# --wait waits for the run to appear and finish first (up to 30 minutes).
set -euo pipefail

usage() { echo "usage: scripts/ci-green.sh <commit> [--wait]" >&2; exit 2; }
[ $# -ge 1 ] || usage
sha=$(git rev-parse --verify "$1^{commit}" 2>/dev/null) || { echo "ci-green: unknown commit $1" >&2; exit 2; }
wait=0
[ "${2:-}" = "--wait" ] && wait=1
repo=${SUNSTACK_REPO:-Lucklyric/sunstack}
want=(ubuntu-latest macos-latest windows-latest)

run=""
for _ in $(seq 1 60); do
  run=$(gh run list -R "$repo" --workflow ci --commit "$sha" --limit 1 --json databaseId -q '.[0].databaseId' 2>/dev/null || true)
  if [ -n "$run" ] || [ $wait -eq 0 ]; then
    break
  fi
  sleep 10
done
if [ -z "$run" ]; then
  echo "ci-green: no ci run for ${sha:0:7} yet (push main first)" >&2
  exit 1
fi
if [ $wait -eq 1 ]; then
  gh run watch "$run" -R "$repo" --exit-status >/dev/null 2>&1 || true
fi

status=$(gh run view "$run" -R "$repo" --json status -q .status)
if [ "$status" != "completed" ]; then
  echo "ci-green: run $run for ${sha:0:7} is $status, not finished" >&2
  exit 1
fi
jobs=$(gh run view "$run" -R "$repo" --json jobs -q '.jobs[] | .name + "\t" + .conclusion')
ok=1
for os in "${want[@]}"; do
  line=$(printf '%s\n' "$jobs" | grep -F "($os)" || true)
  conclusion=${line##*$'\t'}
  if [ -z "$line" ]; then
    echo "ci-green: no $os job in run $run" >&2
    ok=0
  elif [ "$conclusion" != "success" ]; then
    echo "ci-green: $os $conclusion in run $run" >&2
    ok=0
  else
    echo "ci-green: $os success"
  fi
done
if [ $ok -ne 1 ]; then
  echo "ci-green: ${sha:0:7} is not green; do not tag it" >&2
  exit 1
fi
echo "ci-green: ${sha:0:7} passed on all three OSes (run $run)"
