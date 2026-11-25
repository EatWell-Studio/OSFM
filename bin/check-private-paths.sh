#!/usr/bin/env bash
#
# Fails when the index (what the next commit will contain, or a CI checkout) holds a path that must never be in this
# public repository: AI-assistant configuration (Claude Code and others), internal documents, secrets, instance data,
# or any symbolic link. .gitignore keeps these out of `git add`, but `git add -f` overrides it; this check does not.
#
#   bin/check-private-paths.sh          check the index of the repository this script belongs to
#
# Run by CI (.github/workflows/guard.yml) and by the pre-commit hook (bin/hooks/pre-commit, see bin/install-hooks.sh).
# Exit status: 0 when nothing is found, 1 when something is, 2 when the check itself could not run.

set -u
repo="$(cd "$(dirname "$0")/.." && pwd)"

forbidden='(^|/)CLAUDE(\.local)?\.md$'
forbidden="$forbidden"'|(^|/)\.claude(/|$)|(^|/)\.mcp\.json$|(^|/)\.claudeignore$'
forbidden="$forbidden"'|(^|/)(AGENTS(\.override)?|GEMINI)\.md$'
forbidden="$forbidden"'|(^|/)\.(gemini|codex|cursor|windsurf|continue|roo)(/|$)'
forbidden="$forbidden"'|(^|/)\.(cursorrules|cursorignore|windsurfrules|clinerules)$|(^|/)\.aider'
forbidden="$forbidden"'|^\.github/(copilot-instructions\.md$|instructions/|prompts/)'
forbidden="$forbidden"'|^docs/internal(/|$)'
forbidden="$forbidden"'|(^|/)\.env(\.[^/]*)?$'
forbidden="$forbidden"'|\.(sqlite3?|db|db-journal|db-shm|db-wal|pmtiles|mbtiles|pem|key|p12|pfx)$|\.osm\.pbf$'
forbidden="$forbidden"'|(^|/)(id_rsa|id_ed25519)[^/]*$|(^|/)(credentials|service-account)[^/]*\.json$'
allowed='(^|/)\.env\.example$'

listing="$(git -C "$repo" ls-files -s -z | tr '\0' '\n')" || { echo "check-private-paths: git ls-files failed" >&2; exit 2; }

found=0
paths="$(printf '%s\n' "$listing" | sed -n 's/^[0-7]\{6\} [0-9a-f]\{40,64\} [0-3]	//p')"
hits="$(printf '%s\n' "$paths" | grep -E "$forbidden" | grep -Ev "$allowed")"
if [ -n "$hits" ]; then
	echo "Private or sensitive paths are tracked (they belong in the private docs repository or nowhere):" >&2
	printf '%s\n' "$hits" | sed 's|^|  |' >&2
	found=1
fi
links="$(printf '%s\n' "$listing" | sed -n 's/^120000 [0-9a-f]\{40,64\} [0-3]	//p')"
if [ -n "$links" ]; then
	echo "Symbolic links are tracked (this repository has none; local links to the docs repository stay untracked):" >&2
	printf '%s\n' "$links" | sed 's|^|  |' >&2
	found=1
fi
if [ "$found" -ne 0 ]; then
	echo "Remove them from the index with: git rm --cached <path>" >&2
	exit 1
fi
echo "check-private-paths: no private paths or links tracked."
exit 0
