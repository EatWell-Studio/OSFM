#!/usr/bin/env bash
#
# Fails when web/src/api/types.gen.ts, the TypeScript types of the API, is not what the Go types of internal/api
# generate (architecture §3.2, tygo.yaml). It generates the file again in place and compares it with the index (the
# committed file in a CI checkout; the staged one locally), so a change to the Go types needs the regenerated file in
# the same commit:
#
#   go tool tygo generate && git add web/src/api/types.gen.ts
#
# Run by bin/gate.sh and CI. Exit status: 0 when the file is up to date, 1 when it is not, 2 when the check could not run.

set -u
repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo" || exit 2
file=web/src/api/types.gen.ts

go tool tygo generate || exit 2
if ! git ls-files --error-unmatch "$file" >/dev/null 2>&1; then
	echo "check-api-types: $file is not in the index: git add it" >&2
	exit 1
fi
if ! git diff --exit-code -- "$file"; then
	echo "check-api-types: $file is out of date with internal/api: run 'go tool tygo generate' and commit the result" >&2
	exit 1
fi
echo "check-api-types: $file is up to date"
