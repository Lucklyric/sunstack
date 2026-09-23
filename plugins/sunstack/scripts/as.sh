#!/bin/sh
# as.sh [--root DIR] [--tool claude|codex] <id|title> [--token T] [--takeover --expect OLD]
#
# Claim (or resume, or take over) an agent ID, then print the identity bundle.
# Design §6 "身份与 context" and §7.

# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

root='' tool=unknown arg='' token='' takeover='' expect=''
while [ $# -gt 0 ]; do
    case $1 in
        --root) root=$2; shift 2 ;;
        --tool) tool=$2; shift 2 ;;
        --token) token=$2; shift 2 ;;
        --takeover) takeover=1; shift ;;
        --expect) expect=$2; shift 2 ;;
        -*) fail 2 usage "unknown option: $1" ;;
        *) arg=$1; shift ;;
    esac
done

find_root "$root"

roster() {
    for d in "$SS_DIR"/*/; do
        [ -f "$d/AGENT.md" ] || continue
        i=${d%/}; i=${i##*/}
        o=$(live_get "$i" token)
        if [ -n "$o" ]; then printf '%s\tclaimed\n' "$i"; else printf '%s\tfree\n' "$i"; fi
    done
}

if [ -z "$arg" ]; then
    r=$(roster)
    [ -n "$r" ] || fail 1 empty_team "no agents hired yet; run hire first"
    printf 'missing_arguments: id\n%s\n' "$r"
    exit 2
fi

# Resolve: exact ID first, then a title with exactly one instance.
if [ -f "$SS_DIR/$arg/AGENT.md" ]; then
    id=$arg
else
    valid_part "$arg" || fail 2 usage "invalid id or title: $arg"
    m=$(for d in "$SS_DIR/$arg".*/; do [ -f "$d/AGENT.md" ] && { i=${d%/}; printf '%s\n' "${i##*/}"; }; done)
    n=$(printf '%s' "$m" | grep -c .)
    case $n in
        0) fail 1 not_found "no agent $arg; hire it first" ;;
        1) id=$m ;;
        *) printf 'missing_arguments: id\n'; roster | grep "^$arg\."; exit 2 ;;
    esac
fi
valid_id "$id" || fail 2 usage "invalid id: $id"

for f in "$SS_DIR/PILLARS.md" "$SS_DIR/$id/AGENT.md" "$SS_DIR/$id/pillars.md" "$SS_DIR/$id/context.md"; do
    has_conflict_markers "$f" && fail 1 conflict "merge conflict markers in ${f#"$SS_ROOT"/}; resolve them first"
done

lock "$id"
cur=$(live_get "$id" token)
pane=${TMUX_PANE:-}
if [ -z "$cur" ]; then
    new=$(new_token); claimed=$(now); mode=claimed
elif [ -n "$token" ]; then
    [ "$token" = "$cur" ] || fail 4 token "token does not match the current claim on $id (taken over?)"
    new=$cur; claimed=$(live_get "$id" claimed); mode=resumed
elif [ -n "$takeover" ]; then
    [ -n "$expect" ] || fail 2 usage "--takeover needs --expect <token shown in the refusal>"
    [ "$expect" = "$cur" ] || fail 4 occupied "the claim on $id changed since it was shown; run as again"
    new=$(new_token); claimed=$(now); mode=taken_over
else
    ref="claim=$cur tool=$(live_get "$id" tool) host=$(live_get "$id" host) pane=$(live_get "$id" tmux_pane) last_contact=$(live_get "$id" last_contact)"
    if [ -n "$pane" ] && [ "$(live_get "$id" tmux_pane)" = "$pane" ]; then
        printf '%s\n' "$ref"
        fail 4 occupied_same_pane "$id is claimed from this same tmux pane; confirm with the user, then rerun with --takeover --expect $cur"
    fi
    printf '%s\n' "$ref"
    fail 4 occupied "$id is claimed by another session; only take over (--takeover --expect $cur) if the user confirms no other session should continue"
fi
live_write "$id" "$new" "$claimed" "$tool" "$pane"
unlock

section() {
    [ -f "$2" ] || return 0
    printf '\n===== %s =====\n' "$1"
    cat "$2"
}

printf 'sunstack: %s %s\n' "$mode" "$id"
section "PROTOCOL.md" "$SS_DIR/PROTOCOL.md"
section "PILLARS.md (team)" "$SS_DIR/PILLARS.md"
section "$id/AGENT.md" "$SS_DIR/$id/AGENT.md"
section "$id/pillars.md" "$SS_DIR/$id/pillars.md"
section "$id/context.md" "$SS_DIR/$id/context.md"
printf '\n===== threads =====\n'
for f in "$SS_DIR/$id/threads"/*.md; do [ -f "$f" ] && { f=${f##*/}; printf '%s\n' "${f%.md}"; }; done
printf '\n===== inbox (listed only; act on it only if spawned for it or the user asks) =====\n'
for f in "$SS_DIR/_inbox/$id"/*.md; do [ -f "$f" ] && printf '%s\n' "${f##*/}"; done
cat <<EOF

===== session state =====
root: $SS_ROOT
id: $id
token: $new
Pass --root, the id and --token explicitly on every later save, check, send and release.
Run save and release before ending or switching identity.
EOF
