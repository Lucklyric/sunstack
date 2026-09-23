#!/bin/sh
# Install the sunstack CLI from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/Lucklyric/sunstack/main/install.sh | sh
#
# Environment: SUNSTACK_BIN_DIR (default ~/.local/bin), SUNSTACK_VERSION (default latest).
# Then run `sunstack install` to add the plugin to Claude Code and Codex.

set -eu

repo=Lucklyric/sunstack
bin_dir=${SUNSTACK_BIN_DIR:-$HOME/.local/bin}

case $(uname -s) in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) echo "sunstack: unsupported OS $(uname -s); on Windows download the zip from https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) echo "sunstack: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

if [ -n "${SUNSTACK_VERSION:-}" ]; then
    base=https://github.com/$repo/releases/download/v${SUNSTACK_VERSION#v}
else
    base=https://github.com/$repo/releases/latest/download
fi
name=sunstack_${os}_${arch}.tar.gz

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "downloading $name"
curl -fsSL "$base/$name" -o "$tmp/$name"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

want=$(awk -v n="$name" '$2 == n { print $1 }' "$tmp/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$tmp/$name" | awk '{ print $1 }')
else
    got=$(shasum -a 256 "$tmp/$name" | awk '{ print $1 }')
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
    echo "sunstack: checksum mismatch for $name" >&2
    exit 1
fi

tar -xzf "$tmp/$name" -C "$tmp" sunstack
mkdir -p "$bin_dir"
# Stage inside the destination so the final rename is atomic on one filesystem.
staged="$bin_dir/.sunstack-new-$$"
cp "$tmp/sunstack" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$bin_dir/sunstack"
echo "installed $("$bin_dir/sunstack" version | head -n 1) to $bin_dir/sunstack"

case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "note: $bin_dir is not on your PATH; add it to your shell profile" ;;
esac
echo "next: run 'sunstack install' to add the plugin to Claude Code and Codex"
