# Shared helpers for Sunstack scripts. POSIX sh, sourced by every command.
#
# Exit codes (design §8):
#   0 done
#   1 filesystem, permission or content error
#   2 usage error, or missing_arguments (choices on stdout)
#   3 snapshot mismatch (fresh snapshot on stdout, nothing written)
#   4 claim problem: busy | token | occupied | occupied_same_pane

set -u

SS_LOCK_HELD=

# fail <exit> <reason> <message>
fail() {
    printf 'sunstack: %s: %s\n' "$2" "$3" >&2
    exit "$1"
}

now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

new_token() {
    od -An -N8 -tx1 /dev/urandom | tr -d ' \n'
}

# checksum <file>: prints "absent" for a missing file.
checksum() {
    if [ -e "$1" ]; then
        cksum < "$1" | awk '{ print $1 "-" $2 }'
    else
        printf 'absent\n'
    fi
}

valid_part() {
    printf '%s\n' "$1" | grep -Eq '^[a-z][a-z0-9-]{1,31}$'
}

# valid_id <id>: <title> or <title>.<name>
valid_id() {
    case $1 in
        *.*.*) return 1 ;;
        *.*) valid_part "${1%%.*}" && valid_part "${1#*.}" ;;
        *) valid_part "$1" ;;
    esac
}

has_conflict_markers() {
    [ -f "$1" ] && grep -Eq '^(<<<<<<<|>>>>>>>)( |$)|^=======$' "$1"
}

# find_root [dir]: nearest ancestor holding sunstack/. Sets SS_ROOT and SS_DIR.
find_root() {
    d=${1:-$PWD}
    d=$(cd "$d" 2>/dev/null && pwd -P) || fail 1 no_root "cannot enter $1"
    while :; do
        if [ -d "$d/sunstack" ]; then
            # shellcheck disable=SC2034 # used by the sourcing scripts
            SS_ROOT=$d
            SS_DIR=$d/sunstack
            return 0
        fi
        [ "$d" = / ] && fail 1 no_root "no sunstack/ found from ${1:-$PWD} upward; run init first"
        d=$(dirname "$d")
    done
}

# live_get <id> <key>: read one field from _live/<id>.json (Sunstack's own flat format).
live_get() {
    f=$SS_DIR/_live/$1.json
    [ -f "$f" ] || return 0
    sed -n "s/^  \"$2\": \"\\(.*\\)\",\\{0,1\\}\$/\\1/p" "$f"
}

# live_write <id> <token> <claimed> <tool> <tmux_pane>
live_write() {
    mkdir -p "$SS_DIR/_live"
    tmp=$SS_DIR/_live/.$1.json.$$
    cat > "$tmp" <<EOF
{
  "tool": "$4",
  "host": "$(hostname -s)",
  "token": "$2",
  "claimed": "$3",
  "last_contact": "$(now)",
  "tmux_pane": "$5"
}
EOF
    mv -f "$tmp" "$SS_DIR/_live/$1.json"
}

# lock <id>: per-ID mkdir lock under _locks/, a few tries, then exit 4 busy.
lock() {
    mkdir -p "$SS_DIR/_locks"
    l=$SS_DIR/_locks/$1
    tries=0
    until mkdir "$l" 2>/dev/null; do
        tries=$((tries + 1))
        [ "$tries" -ge 5 ] && fail 4 busy "$1 is locked by another operation ($(cat "$l/owner" 2>/dev/null || echo unknown)); retry, or run health"
        sleep 1
    done
    SS_LOCK_HELD=$l
    printf '%s pid %s %s\n' "$(now)" "$$" "${0##*/}" > "$l/owner"
    trap 'unlock' EXIT
    trap 'unlock; exit 1' HUP INT TERM
}

unlock() {
    if [ -n "$SS_LOCK_HELD" ]; then
        rm -rf "$SS_LOCK_HELD"
        SS_LOCK_HELD=
    fi
}

# require_token <id> <token>: caller must hold the lock for a write.
require_token() {
    [ -n "$2" ] || fail 4 token "missing --token; run as again (see design §7)"
    cur=$(live_get "$1" token)
    [ -n "$cur" ] || fail 4 token "$1 is not claimed; run as first"
    [ "$cur" = "$2" ] || fail 4 token "token does not match the current claim on $1 (taken over?)"
}

# target_path <id> <rel>: allow only context.md and threads/<topic>.md.
target_path() {
    case $2 in
        context.md) ;;
        threads/*.md)
            t=${2#threads/}
            t=${t%.md}
            valid_part "$t" || fail 2 usage "invalid thread topic: $t"
            ;;
        *) fail 2 usage "target must be context.md or threads/<topic>.md, got: $2" ;;
    esac
    p=$SS_DIR/$1/$2
    [ -L "$p" ] && fail 1 symlink "refusing symlinked target: $2"
    [ -L "$SS_DIR/$1/threads" ] && fail 1 symlink "refusing symlinked threads/ directory"
    printf '%s\n' "$p"
}
