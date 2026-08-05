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
#   * dropping the `api` OR                   -> an api/**-only change wrongly skips both
#   * an unmodelled `if:` construct           -> guard must error, never assume a default
#   * an output name the filters don't define -> same
#   * deleting a detect-changes `outputs:` export, or pointing one at a misspelt filter -> same
#     (this is the dorny/paths-filter mistake that is SILENT at runtime: the output resolves to the
#     empty string, every `== 'true'` goes false, and the gated jobs just stop running)
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

# mutate <expected-site-count> <old-substring> <new-substring> -> writes $SANDBOX/mutated.yml.
#
# The site count is asserted, not merely reported. Two failure modes it closes: a mutation that
# matched nothing would make the following assert_rejected vacuous, AND — the reason the count is
# per-case rather than "at least one" — the two MCP jobs' conditions are currently identical, so a
# condition mutation must hit BOTH (2). If they ever diverge, a 2-site mutation silently becoming a
# 1-site mutation would leave half the jobs unfalsified while the suite still passed green.
mutate() {
  MUT_SRC="$WORKFLOW" MUT_DST="$SANDBOX/mutated.yml" MUT_WANT="$1" MUT_OLD="$2" MUT_NEW="$3" python3 - <<'PY'
import os
import sys

src, dst = os.environ["MUT_SRC"], os.environ["MUT_DST"]
old, new, want = os.environ["MUT_OLD"], os.environ["MUT_NEW"], int(os.environ["MUT_WANT"])
text = open(src).read()
count = text.count(old)
if count != want:
    sys.exit("mutation target found at %d site(s) in %s, expected exactly %d: %r"
             % (count, src, want, old))
open(dst, "w").write(text.replace(old, new))
print("  (mutation applied at %d site(s), as expected)" % count)
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
mutate 2 "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||" \
       "      (needs.detect-changes.outputs.mcp == 'true' ||
       needs.detect-changes.outputs.api == 'true' ||
       needs.detect-changes.outputs.backend == 'true' ||"
assert_rejected "backend-or-restored" "scripts/foo.sh"
echo ""

echo "Case 3: dropping 'packages/**' from the mcp filter must make the api-client row go red"
mutate 1 "              - 'api/openapi.yaml'
              - 'packages/**'" \
       "              - 'api/openapi.yaml'"
assert_rejected "packages-glob-removed" "packages/api-client/src/x.ts"
echo ""

echo "Case 4: an if: construct the guard does not model must be an error, not a default"
mutate 2 "      always() &&
      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      always() && needs.openapi-validate.result == 'success' &&
      (needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "unmodelled-construct" "does not model"
echo ""

echo "Case 5: referencing an output the paths-filter step does not define must be an error"
mutate 2 "      (needs.detect-changes.outputs.mcp == 'true' ||" \
       "      (needs.detect-changes.outputs.nosuchfilter == 'true' ||
       needs.detect-changes.outputs.mcp == 'true' ||"
assert_rejected "undefined-output" "does not define"
echo ""

echo "Case 6: dropping the 'api' OR must make the api-only row go red"
# The api OR is the one the dropped-\`backend\` safety argument rests on (openapi-validate lands
# every committed contract change as an api/openapi.yaml diff). Because \`mcp\` lists
# api/openapi.yaml too, and api/ currently holds exactly that one file, only a path reachable ONLY
# through api/** can prove this OR is load-bearing — without that row, deleting it as "redundant"
# would keep the whole table green.
mutate 2 "       needs.detect-changes.outputs.api == 'true' ||
" ""
assert_rejected "api-or-removed" "api/schemas/foo.yaml"
echo ""

echo "Case 7: deleting the detect-changes 'mcp' output export must be an error"
# The classic dorny/paths-filter mistake, and silent at runtime: with this line gone both MCP
# conditions collapse to (deps || workflow), so packages/api-client and apps/mcp-server changes stop
# running either job — exactly the regression this task fixed.
mutate 1 "      mcp: \${{ steps.filter.outputs.mcp }}
" ""
# Caught by the "computed but never exported" check, which fires before condition resolution — an
# unexported filter is unreachable for EVERY consumer, not just these two jobs.
assert_rejected "export-deleted" "never exported"
echo ""

echo "Case 8: an output export pointing at a misspelt filter name must be an error"
mutate 1 "      mcp: \${{ steps.filter.outputs.mcp }}" \
         "      mcp: \${{ steps.filter.outputs.mcpp }}"
assert_rejected "export-typo" "defines no filter named"
echo ""

echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
