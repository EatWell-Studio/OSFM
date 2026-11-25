#!/usr/bin/env bash
#
# Installs the repository's git hooks (bin/hooks/*) into the common git directory, shared by every worktree.
#
#   bin/install-hooks.sh            install, refusing to overwrite a different hook that is not ours
#   bin/install-hooks.sh --check    exit 0 when every hook is installed and up to date, 1 otherwise
#
# Run once after cloning, and again whenever a file in bin/hooks/ changes.

set -u
repo="$(cd "$(dirname "$0")/.." && pwd)"
common="$(git -C "$repo" rev-parse --git-common-dir)" || exit 2
case "$common" in
	/*) ;;
	*) common="$repo/$common" ;;
esac
hooks="$common/hooks"
mode=${1:-install}

status=0
for src in "$repo"/bin/hooks/*; do
	name="$(basename "$src")"
	dst="$hooks/$name"
	if [ -f "$dst" ] && cmp -s "$src" "$dst"; then
		echo "$name: up to date"
		continue
	fi
	if [ "$mode" = "--check" ]; then
		echo "$name: not installed or out of date"
		status=1
		continue
	fi
	if [ -f "$dst" ] && ! grep -q 'bin/install-hooks.sh' "$dst"; then
		echo "$name: refused, $dst is someone else's hook; merge it by hand" >&2
		status=1
		continue
	fi
	mkdir -p "$hooks" && cp "$src" "$dst" && chmod +x "$dst" && echo "$name: installed" || status=1
done
exit "$status"
