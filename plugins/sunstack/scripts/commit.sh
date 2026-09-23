#!/bin/sh
# commit.sh [--root DIR] <id> <target> <candidate> <checksum> --token T
# commit.sh [--root DIR] <id> <target> --delete <checksum> --token T
#
# Under the per-ID lock: check token, check the target still matches the
# snapshot checksum, then replace (or delete) it. Design §8 steps 3-6.

# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

root='' token='' del='' n=0 id='' rel='' cand='' sum=''
while [ $# -gt 0 ]; do
    case $1 in
        --root) root=$2; shift 2 ;;
        --token) token=$2; shift 2 ;;
        --delete) del=1; shift ;;
        -*) fail 2 usage "unknown option: $1" ;;
        *)
            n=$((n + 1))
            case $n in
                1) id=$1 ;;
                2) rel=$1 ;;
                3) if [ -n "$del" ]; then sum=$1; else cand=$1; fi ;;
                4) sum=$1 ;;
                *) fail 2 usage "too many arguments" ;;
            esac
            shift ;;
    esac
done
[ -n "$id" ] && [ -n "$rel" ] && [ -n "$sum" ] || { printf 'missing_arguments: id target checksum\n'; exit 2; }
[ -n "$del" ] || [ -n "$cand" ] || { printf 'missing_arguments: candidate\n'; exit 2; }

find_root "$root"
valid_id "$id" || fail 2 usage "invalid id: $id"
target=$(target_path "$id" "$rel") || exit $?

if [ -z "$del" ]; then
    [ -f "$cand" ] || fail 1 no_candidate "candidate not found: $cand"
    cdir=$(cd "$(dirname "$cand")" && pwd -P)
    [ "$cdir" = "$(cd "$SS_DIR/_tmp/$id" 2>/dev/null && pwd -P)" ] ||
        fail 2 usage "candidate must live in sunstack/_tmp/$id/"
    has_conflict_markers "$cand" && fail 1 conflict "candidate contains merge conflict markers"
fi

lock "$id"
require_token "$id" "$token"
has_conflict_markers "$target" && fail 1 conflict "merge conflict markers in $rel; resolve them first"
cur=$(checksum "$target")
if [ "$cur" != "$sum" ]; then
    mkdir -p "$SS_DIR/_tmp/$id"
    snap=$SS_DIR/_tmp/$id/snap.$(new_token)
    if [ -e "$target" ]; then cp "$target" "$snap"; fi
    printf 'checksum: %s\nsnapshot: %s\n----- content -----\n' "$(checksum "$snap")" "$snap"
    [ -e "$snap" ] && cat "$snap"
    fail 3 mismatch "$rel changed since the snapshot; merge onto the content above and commit again"
fi

if [ -n "$del" ]; then
    rm -f "$target"
else
    mkdir -p "$(dirname "$target")"
    tmp=$(dirname "$target")/.${target##*/}.tmp.$$
    if ! { cp "$cand" "$tmp" && mv -f "$tmp" "$target"; }; then
        rm -f "$tmp"
        fail 1 write_failed "could not write $rel"
    fi
    rm -f "$cand"
fi
live_write "$id" "$token" "$(live_get "$id" claimed)" "$(live_get "$id" tool)" "$(live_get "$id" tmux_pane)"
unlock
printf 'sunstack: committed %s/%s\n' "$id" "$rel"
