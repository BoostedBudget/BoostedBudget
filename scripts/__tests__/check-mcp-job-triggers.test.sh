#!/usr/bin/env bash
# Falsification suite for scripts/check-mcp-job-triggers.py — the executable truth table that gates
# the two MCP jobs' `if:` conditions in .github/workflows/test.yml (#1464 U1a).
#
# A guard that has never rejected anything is not a guard. Each mutation case below writes a
# THROWAWAY COPY of the real workflow with one deliberate regression applied, runs the same script
# CI runs against that copy, and asserts it fails naming the specific row that should go red:
#
#   * restoring the `backend` OR              -> scripts/**-only PR wrongly launches both MCP jobs
#   * dropping 'packages/**' from `mcp`       -> packages/api-client change wrongly skips both
#   * an unmodelled `if:` construct           -> guard must error, never assume a default
#   * an output name the filters don't define -> same
#
# Case 1 runs against the real, unmutated workflow and must PASS, so the suite proves the guard
# discriminates rather than rejecting everything. The real workflow is never modified.
#
# Run: bash scripts/__tests__/check-mcp-job-triggers.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/check-mcp-job-triggers.py"
WORKFLOW="$ROOT/.github/workflows/test.yml"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# mutate <old-substring> <new-substring> -> writes $SANDBOX/mutated.yml, fails loudly if the
# substring was not found exactly once (a mutation that did not apply would produce a vacuous PASS).
mutate() {
  MUT_SRC="$WORKFLOW" MUT_DST="$SANDBOX/mutated.yml" MUT_OLD="$1" MUT_NEW="$2" python3 - <<'PY'
import os
import sys

src, dst = os.environ["MUT_SRC"], os.environ["MUT_DST"]
old, new = os.environ["MUT_OLD"], os.environ["MUT_NEW"]
text = open(src).read()
count = text.count(old)
if count == 0:
    sys.exit("mutation target not found in %s: %r" % (src, old))
open(dst, "w").write(text.replace(old, new))
print("  (mutation applied at %d site(s))" % count)
PY
}

# assert_rejected <case-name> <expected-substring>  (operates on $SANDBOX/mutated.yml)
assert_rejected() {
  local name="$1" expect="$2" output
  if output=$(python3 "$SCRIPT" "$SANDBOX/mutated.yml" 2>&1); then
    fail "$name: expected non-zero exit, got 0"
    echo "$output" | sed 's/^/      /'
    return
  fi
  if echo "$output" | grep -q "$expect"; then
    pass "$name: rejected, naming '$expect'"
  else
    fail "$name: rejected but output did not mention '$expect'"
    echo "$output" | sed 's/^/      /'
  fi
}

echo "=== check-mcp-job-triggers.py falsification suite ==="
echo ""

echo "Case 1: the real workflow MUST pass"
if output=$(python3 "$SCRIPT" "$WORKFLOW" 2>&1); then
  if echo "$output" | grep -q "rows match for both MCP jobs"; then
    pass "real workflow accepted"
  else
    fail "real workflow exited 0 but summary line missing (got: $output)"
  fi
else
  fail "real workflow was rejected"
  echo "$output" | sed 's/^/      /'
fi
echo ""

echo "Case 2: restoring the 'backend' OR must make the scripts-only row go red"
mutate "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||" \
       "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||
       needs.detect-changes.outputs.backend == 'true' ||"
assert_rejected "backend-or-restored" "scripts/foo.sh"
echo ""

echo "Case 3: dropping 'packages/**' from the mcp filter must make the api-client row go red"
mutate "              - 'api/openapi.yaml'
              - 'packages/**'" \
       "              - 'api/openapi.yaml'"
assert_rejected "packages-glob-removed" "packages/api-client/src/x.ts"
echo ""

echo "Case 4: an if: construct the guard does not model must be an error, not a default"
mutate "      always() &&
      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      always() && needs.openapi-validate.result == 'success' &&
      (needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "unmodelled-construct" "does not model"
echo ""

echo "Case 5: referencing an output the paths-filter step does not define must be an error"
mutate "      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      (needs.detect-changes.outputs.nosuchfilter == 'true' ||
       needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "undefined-output" "does not define"
echo ""

echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
