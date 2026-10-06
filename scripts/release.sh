#!/usr/bin/env bash
# release.sh vX.Y.Z [--update-local]
#
# The only way to release Sunstack:
#   1. checks: on main, clean tree, the plugin manifests say X.Y.Z, the tag is new
#   2. go vet and go test locally
#   3. pushes main and waits for CI; stops unless all three OS jobs pass
#      (scripts/ci-green.sh)
#   4. tags and pushes vX.Y.Z, and waits for the release build to succeed
#   5. with --update-local, runs sunstack update on this machine
# Nothing is tagged, and nothing local is updated, after a failed step.
set -euo pipefail
cd "$(dirname "$0")/.."

usage() { echo "usage: scripts/release.sh vX.Y.Z [--update-local]" >&2; exit 2; }
[ $# -ge 1 ] || usage
tag=$1
update=0
[ "${2:-}" = "--update-local" ] && update=1
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
version=${tag#v}
repo=${SUNSTACK_REPO:-Lucklyric/sunstack}
step() { printf '\n== %s\n' "$*"; }
stop() { echo "release: $*; stopped, nothing tagged" >&2; exit 1; }

step "checks"
[ "$(git rev-parse --abbrev-ref HEAD)" = "main" ] || stop "not on main"
[ -z "$(git status --porcelain)" ] || stop "the tree has uncommitted changes"
for f in plugins/sunstack/.claude-plugin/plugin.json .claude-plugin/marketplace.json; do
  grep -q "\"version\": \"$version\"" "$f" || stop "$f does not say version $version"
done
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null || git ls-remote --tags origin "$tag" | grep -q .; then
  stop "tag $tag already exists"
fi

step "local tests"
go vet ./... || stop "go vet failed"
go test -count=1 ./... || stop "go test failed"

step "push main"
if ! git push -q origin main 2>/dev/null; then
  # Some hosts reach GitHub only over HTTPS.
  git push -q "https://github.com/$repo.git" main || stop "could not push main"
  git update-ref refs/remotes/origin/main HEAD
fi

step "CI on all three OSes"
scripts/ci-green.sh HEAD --wait || stop "CI is not green"

step "tag $tag"
git tag -a "$tag" -m "$tag"
if ! git push -q origin "$tag" 2>/dev/null; then
  git push -q "https://github.com/$repo.git" "$tag" || { git tag -d "$tag" >/dev/null; stop "could not push the tag"; }
fi

step "release build"
run=""
for _ in $(seq 1 60); do
  run=$(gh run list -R "$repo" --workflow release --branch "$tag" --limit 1 --json databaseId -q '.[0].databaseId' 2>/dev/null || true)
  [ -n "$run" ] && break
  sleep 10
done
[ -n "$run" ] || { echo "release: no release run for $tag; check GitHub Actions" >&2; exit 1; }
if ! gh run watch "$run" -R "$repo" --exit-status >/dev/null 2>&1; then
  echo "release: the release build for $tag failed (run $run); local install not updated" >&2
  exit 1
fi
echo "release: $tag published"

if [ $update -eq 1 ]; then
  step "update this machine"
  sunstack update </dev/null
  sunstack --version | head -1
fi
