#!/usr/bin/env bash
#
# The checks a change has to pass, run one after another, each with pipefail. The first step that fails ends the gate
# with that step's status, and its last lines are shown; the steps after it are not run. CI runs the same checks.
#
#   bin/gate.sh                 all steps
#   bin/gate.sh <step> ...      only these steps; each step is one shell command line run from the repository root
#
# Each step's output goes to its own file, gate-<n>.txt, in OSFM_GATE_OUTPUT (default: ../osfm-gate-output beside the
# repository). Frontend dependencies must be installed first: pnpm --dir web install.
#
# Exit status: 0 when every step passed, else the status of the step that failed.

# git's own environment is not the checks': started from a hook, a git command inside a step would otherwise act on the
# hook's repository whatever directory it runs in.
for name in $(env | sed -n 's/^\(GIT_[A-Za-z0-9_]*\)=.*/\1/p'); do
	unset "$name"
done
set -u
REPO="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OSFM_GATE_OUTPUT:-$REPO/../osfm-gate-output}"
mkdir -p "$OUT" || exit 1

if [ "$#" -eq 0 ]; then
	set -- \
		'bin/check-private-paths.sh' \
		'pnpm --dir web run format:check' \
		'pnpm --dir web run lint' \
		'pnpm --dir web run check:i18n' \
		'pnpm --dir web run typecheck' \
		'pnpm --dir web run test' \
		'pnpm --dir web run build' \
		'golangci-lint run ./...' \
		'go test ./...' \
		'go build -o "$OSFM_GATE_BIN" ./cmd/osfm'
fi
OSFM_GATE_BIN="$OUT/osfm"
export OSFM_GATE_BIN

n=0
for step in "$@"; do
	n=$((n + 1))
	(cd "$REPO" && bash -o pipefail -c "$step") >"$OUT/gate-$n.txt" 2>&1
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "gate: step $n failed with status $status: $step" >&2
		tail -n 20 "$OUT/gate-$n.txt" >&2
		exit "$status"
	fi
	echo "gate: step $n passed: $step"
done
exit 0
