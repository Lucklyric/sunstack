#!/bin/sh
# shellcheck disable=SC2015 # ok() never fails, so A && ok || bad is safe here
# Script-level checks for the build step 1 spike (design §12): claims, takeover,
# snapshot/commit, races, and path guards. Runs in a throwaway project under
# .spike/ (git-ignored). Usage: sh tests/spike-test.sh

set -u
here=$(cd "$(dirname "$0")/.." && pwd -P)
S=$here/plugins/sunstack/scripts
P=$here/.spike/test-proj
rm -rf "$P"
mkdir -p "$P/sunstack"
unset TMUX_PANE

pass=0 failn=0
ok() { pass=$((pass + 1)); printf 'ok    %s\n' "$1"; }
bad() { failn=$((failn + 1)); printf 'FAIL  %s\n' "$1"; }
# expect <exit> <label> <cmd...>: run, compare exit code, keep output in $out
expect() {
    want=$1 label=$2; shift 2
    out=$("$@" 2>&1); got=$?
    if [ "$got" -eq "$want" ]; then ok "$label"; else bad "$label (exit $got, want $want): $out"; fi
}
token_of() { printf '%s\n' "$1" | sed -n 's/^token: //p'; }
sum_of() { printf '%s\n' "$1" | sed -n 's/^checksum: //p'; }

hire() {
    mkdir -p "$P/sunstack/$1"
    printf -- '---\ntitle: %s\nfrom: custom\nhired: 2026-09-22\n---\n## 职责\n' "${1%%.*}" > "$P/sunstack/$1/AGENT.md"
    printf '## 当前状态\n## 决策\n' > "$P/sunstack/$1/context.md"
}
hire builder.alice
hire builder.bob
hire reviewer
printf -- '- 2026-09-22 所有 migration 必须可回滚\n' > "$P/sunstack/PILLARS.md"
cd "$P" || exit 1

# --- as: resolution and wizard contract
expect 2 "as with no id returns missing_arguments" sh "$S/as.sh"
printf '%s' "$out" | grep -q '^missing_arguments' || bad "  ...roster listed"
expect 2 "as builder (two instances) asks which" sh "$S/as.sh" builder
expect 1 "as unknown title fails" sh "$S/as.sh" nobody
expect 2 "as with a bad name is a usage error" sh "$S/as.sh" 'Bad/Name'

# --- claim, occupied, resume, takeover
expect 0 "as builder.alice claims" sh "$S/as.sh" --tool claude builder.alice
T1=$(token_of "$out")
[ -n "$T1" ] && ok "  ...token returned" || bad "  ...token returned"
printf '%s' "$out" | grep -q 'PILLARS.md' && ok "  ...bundle includes team pillars" || bad "  ...bundle includes team pillars"
expect 4 "second as on a claimed id is refused" sh "$S/as.sh" builder.alice
expect 0 "as with the right token resumes" sh "$S/as.sh" builder.alice --token "$T1"
[ "$(token_of "$out")" = "$T1" ] && ok "  ...same token on resume" || bad "  ...same token on resume"
expect 4 "as with a wrong token is refused" sh "$S/as.sh" builder.alice --token deadbeef
expect 4 "takeover with a stale --expect is refused" sh "$S/as.sh" builder.alice --takeover --expect deadbeef
expect 0 "takeover with the shown claim succeeds" sh "$S/as.sh" builder.alice --takeover --expect "$T1"
T2=$(token_of "$out")
[ "$T2" != "$T1" ] && ok "  ...token rotated" || bad "  ...token rotated"
expect 4 "old token can no longer snapshot" sh "$S/snapshot.sh" builder.alice context.md --token "$T1"

# --- same-pane recovery hint
expect 0 "claim reviewer from pane %99" env TMUX_PANE=%99 sh "$S/as.sh" reviewer
TR=$(token_of "$out")
expect 4 "same pane without token gets occupied_same_pane" env TMUX_PANE=%99 sh "$S/as.sh" reviewer
printf '%s' "$out" | grep -q occupied_same_pane && ok "  ...reason is occupied_same_pane" || bad "  ...reason is occupied_same_pane"
expect 4 "another pane gets plain occupied" env TMUX_PANE=%12 sh "$S/as.sh" reviewer
expect 0 "reviewer releases" sh "$S/release.sh" reviewer --token "$TR"

# --- concurrent claims on a free id: exactly one wins
for i in 1 2 3 4 5 6; do
    ( sh "$S/as.sh" reviewer >/dev/null 2>&1; echo $? > "$P/.race-as.$i" ) &
done
wait
wins=$(cat "$P"/.race-as.* | grep -c '^0$')
[ "$wins" -eq 1 ] && ok "6 concurrent as on a free id: exactly 1 wins" || bad "6 concurrent as on a free id: $wins won"

# --- snapshot / commit
expect 0 "snapshot context.md" sh "$S/snapshot.sh" builder.alice context.md --token "$T2"
C1=$(sum_of "$out")
D=$P/sunstack/_tmp/builder.alice
printf '## 当前状态\n- 2026-09-22 A\n## 决策\n' > "$D/cand-a"
printf '## 当前状态\n- 2026-09-22 B\n## 决策\n' > "$D/cand-b"
expect 0 "commit A from the snapshot" sh "$S/commit.sh" builder.alice context.md "$D/cand-a" "$C1" --token "$T2"
expect 3 "commit B from the same old snapshot is a mismatch" sh "$S/commit.sh" builder.alice context.md "$D/cand-b" "$C1" --token "$T2"
printf '%s' "$out" | grep -q -- '- 2026-09-22 A' && ok "  ...mismatch returns the fresh content" || bad "  ...mismatch returns the fresh content"
grep -q 'A$' "$P/sunstack/builder.alice/context.md" && ok "  ...file holds A only" || bad "  ...file holds A only"

# concurrent commits from one snapshot: exactly one lands
expect 0 "snapshot again" sh "$S/snapshot.sh" builder.alice context.md --token "$T2"
C2=$(sum_of "$out")
for i in 1 2 3 4 5; do
    printf '## 当前状态\n- race %s\n' "$i" > "$D/cand-r$i"
    ( sh "$S/commit.sh" builder.alice context.md "$D/cand-r$i" "$C2" --token "$T2" >/dev/null 2>&1; echo $? > "$P/.race-c.$i" ) &
done
wait
wins=$(cat "$P"/.race-c.* | grep -c '^0$')
[ "$wins" -eq 1 ] && ok "5 concurrent commits from one snapshot: exactly 1 lands" || bad "5 concurrent commits: $wins landed ($(cat "$P"/.race-c.* | tr '\n' ' '))"

# threads: create from absent, then delete
expect 0 "snapshot a missing thread" sh "$S/snapshot.sh" builder.alice threads/auth-refactor.md --token "$T2"
[ "$(sum_of "$out")" = absent ] && ok "  ...checksum is absent" || bad "  ...checksum is absent"
printf '目标：重构 auth\n' > "$D/cand-t"
expect 0 "commit creates the thread" sh "$S/commit.sh" builder.alice threads/auth-refactor.md "$D/cand-t" absent --token "$T2"
expect 0 "snapshot the thread" sh "$S/snapshot.sh" builder.alice threads/auth-refactor.md --token "$T2"
expect 0 "commit --delete removes it" sh "$S/commit.sh" builder.alice threads/auth-refactor.md --delete "$(sum_of "$out")" --token "$T2"
[ ! -e "$P/sunstack/builder.alice/threads/auth-refactor.md" ] && ok "  ...thread gone" || bad "  ...thread gone"

# --- guards
printf 'x\n' > "$P/outside"
expect 2 "candidate outside _tmp/<id>/ is refused" sh "$S/commit.sh" builder.alice context.md "$P/outside" absent --token "$T2"
expect 2 "target outside the allowed set is refused" sh "$S/snapshot.sh" builder.alice AGENT.md --token "$T2"
expect 2 "thread topic with traversal is refused" sh "$S/snapshot.sh" builder.alice 'threads/../x.md' --token "$T2"
printf '<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> b\n' > "$D/cand-c"
expect 1 "candidate with conflict markers is refused" sh "$S/commit.sh" builder.alice context.md "$D/cand-c" absent --token "$T2"
mkdir -p "$P/sunstack/builder.bob/threads"
ln -s "$P/outside" "$P/sunstack/builder.bob/threads/evil.md"
expect 0 "claim builder.bob" sh "$S/as.sh" builder.bob
TB=$(token_of "$out")
expect 1 "symlinked target is refused" sh "$S/snapshot.sh" builder.bob threads/evil.md --token "$TB"

# --- a leftover lock (kill -9 writer) blocks with busy, not silently
mkdir -p "$P/sunstack/_locks/builder.bob"
echo "stale" > "$P/sunstack/_locks/builder.bob/owner"
expect 4 "leftover lock gives busy after retries" sh "$S/release.sh" builder.bob --token "$TB"
printf '%s' "$out" | grep -q busy && ok "  ...reason is busy" || bad "  ...reason is busy"
rm -rf "$P/sunstack/_locks/builder.bob"

# --- release
expect 4 "release with a wrong token is refused" sh "$S/release.sh" builder.alice --token "$T1"
expect 0 "release with the current token" sh "$S/release.sh" builder.alice --token "$T2"
expect 0 "the id is free again" sh "$S/as.sh" builder.alice
[ ! -d "$P/sunstack/_locks/builder.alice" ] && ok "no lock left behind" || bad "no lock left behind"

printf '\n%s passed, %s failed\n' "$pass" "$failn"
[ "$failn" -eq 0 ]
