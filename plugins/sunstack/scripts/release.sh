#!/bin/sh
# release.sh [--root DIR] <id> --token T
#
# Drop this session's claim on <id>. Design §6.

# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

root='' id='' token=''
while [ $# -gt 0 ]; do
    case $1 in
        --root) root=$2; shift 2 ;;
        --token) token=$2; shift 2 ;;
        -*) fail 2 usage "unknown option: $1" ;;
        *) id=$1; shift ;;
    esac
done
[ -n "$id" ] || { printf 'missing_arguments: id\n'; exit 2; }
find_root "$root"
valid_id "$id" || fail 2 usage "invalid id: $id"

lock "$id"
require_token "$id" "$token"
rm -f "$SS_DIR/_live/$id.json"
rm -rf "$SS_DIR/_tmp/$id"
unlock
printf 'sunstack: released %s\n' "$id"
