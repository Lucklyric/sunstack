#!/bin/sh
# snapshot.sh [--root DIR] <id> <context.md|threads/<topic>.md> --token T
#
# Copy the target into _tmp/<id>/, checksum that copy, print both. Design §8 step 1.

# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

root='' id='' rel='' token=''
while [ $# -gt 0 ]; do
    case $1 in
        --root) root=$2; shift 2 ;;
        --token) token=$2; shift 2 ;;
        -*) fail 2 usage "unknown option: $1" ;;
        *) if [ -z "$id" ]; then id=$1; else rel=$1; fi; shift ;;
    esac
done
[ -n "$id" ] && [ -n "$rel" ] || { printf 'missing_arguments: id target\n'; exit 2; }
find_root "$root"
valid_id "$id" || fail 2 usage "invalid id: $id"
target=$(target_path "$id" "$rel") || exit $?
[ -n "$token" ] || fail 4 token "missing --token"
[ "$(live_get "$id" token)" = "$token" ] || fail 4 token "token does not match the current claim on $id"
has_conflict_markers "$target" && fail 1 conflict "merge conflict markers in $rel; resolve them first"

mkdir -p "$SS_DIR/_tmp/$id"
snap=$SS_DIR/_tmp/$id/snap.$(new_token)
if [ -e "$target" ]; then cp "$target" "$snap"; fi
sum=$(checksum "$snap")
printf 'checksum: %s\nsnapshot: %s\ncandidate_dir: %s\n----- content -----\n' "$sum" "$snap" "$SS_DIR/_tmp/$id"
[ -e "$snap" ] && cat "$snap"
exit 0
