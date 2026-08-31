#!/usr/bin/env bash
# Regression suite for scripts/check-ungated-guard-steps.py (#1569 round 17 task 1).
#
# The guard's whole claim is "this step runs on every PR". A guard that only ever sees the real,
# currently-correct workflow is indistinguishable from one that returns 0 unconditionally — which
# is the exact defect class it was written to catch — so every case below MUTATES a copy of the
# real workflow and asserts the guard goes RED on it.
#
# Nothing here touches the tracked tree: each case writes a mutated copy into an mktemp sandbox and
# passes the copy's path as the guard's argv[1]. No `git stash`, no `git checkout`.
#
# Run: bash scripts/__tests__/check-ungated-guard-steps.test.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$ROOT/scripts/check-ungated-guard-steps.py"
WORKFLOW="$ROOT/.github/workflows/test.yml"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

echo "=== check-ungated-guard-steps.py test suite ==="
echo ""

# ── Test 1: the real workflow passes ────────────────────────────────────────────────────────────
echo "Test 1: the tracked workflow is clean"
if python3 "$GUARD" "$WORKFLOW" >/dev/null 2>&1; then
  pass "exits 0 on the real .github/workflows/test.yml"
else
  fail "the real workflow is RED: $(python3 "$GUARD" "$WORKFLOW" 2>&1 || true)"
fi

# ── Test 2: gating the host job with `needs:` + a filter `if:` must fail ────────────────────────
# This is the mutation that actually happened to five guards: the job is fine, the step is fine,
# and a `needs:` line quietly makes the step unreachable on the PRs that matter.
echo "Test 2: host job gated with needs: + a paths-filter if:"
# ⚠ The existing `if: always() && !cancelled()` is REPLACED, not supplemented: a second `if:` key
# under the same job is a duplicate mapping key, and PyYAML keeps the LAST one — so appending would
# have left the original condition in force and made this case vacuous.
python3 - "$WORKFLOW" "$SANDBOX/gated.yml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
out, seen = [], False
for line in open(src).read().split("\n"):
    if line == "  e2e-test:":
        out.append(line)
        out.append("    needs: detect-changes")
        seen = True
        continue
    if seen and line == "    if: always() && !cancelled()":
        out.append("    if: needs.detect-changes.outputs.backend == 'true'")
        seen = False
        continue
    out.append(line)
open(dst, "w").write("\n".join(out))
PY
python3 -c "
import sys, yaml
j = yaml.safe_load(open('$SANDBOX/gated.yml'))['jobs']['e2e-test']
assert j.get('needs') == 'detect-changes' and 'outputs.backend' in str(j.get('if')), (
    'mutation did not take effect: %r / %r' % (j.get('needs'), j.get('if')))
" || fail "Test 2 mutation is invalid — the assertions below would be vacuous"
captured=$(python3 "$GUARD" "$SANDBOX/gated.yml" 2>&1 || true)
if python3 "$GUARD" "$SANDBOX/gated.yml" >/dev/null 2>&1; then
  fail "expected non-zero exit when the host job gained needs: + a filter if:"
elif echo "$captured" | grep -q "Guard — whole-tree corpus guards (#1569 R17 T1)"; then
  pass "exits non-zero and names the corpus-guard step"
else
  fail "exits non-zero but did not name the affected step (got: $captured)"
fi

# ── Test 3: `needs:` alone, with no if:, must still fail ────────────────────────────────────────
# The halfway state. A job with `needs:` and no `if:` still runs, so this is not yet a live defect
# — but it is one line from being one, and the two-condition check exists to catch it there.
echo "Test 3: host job gained needs: but no if: yet"
python3 - "$WORKFLOW" "$SANDBOX/needs-only.yml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
out = []
for line in open(src).read().split("\n"):
    out.append(line)
    if line == "  e2e-test:":
        out.append("    needs: detect-changes")
open(dst, "w").write("\n".join(out))
PY
captured=$(python3 "$GUARD" "$SANDBOX/needs-only.yml" 2>&1 || true)
if python3 "$GUARD" "$SANDBOX/needs-only.yml" >/dev/null 2>&1; then
  fail "expected non-zero exit for a bare needs: on the host job"
elif echo "$captured" | grep -q "declares \`needs:"; then
  pass "exits non-zero and explains the needs: finding"
else
  fail "exits non-zero for the wrong reason (got: $captured)"
fi

# ── Test 3b: a filter `if:` with no `needs:` must fail on the if: branch alone ──────────────────
# Isolates the second condition. Without this case, tests 2 and 3 could both be satisfied by the
# `needs:` branch and the needs.*.outputs.* check would be untested dead code.
echo "Test 3b: host job if: reads a paths-filter output, no needs:"
python3 - "$WORKFLOW" "$SANDBOX/if-only.yml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
out, seen = [], False
for line in open(src).read().split("\n"):
    if line == "  e2e-test:":
        seen = True
    if seen and line == "    if: always() && !cancelled()":
        out.append("    if: needs.detect-changes.outputs.backend == 'true'")
        seen = False
        continue
    out.append(line)
open(dst, "w").write("\n".join(out))
PY
python3 -c "
import yaml
j = yaml.safe_load(open('$SANDBOX/if-only.yml'))['jobs']['e2e-test']
assert not j.get('needs') and 'outputs.backend' in str(j.get('if')), (
    'mutation did not take effect: %r / %r' % (j.get('needs'), j.get('if')))
" || fail "Test 3b mutation is invalid — the assertion below would be vacuous"
captured=$(python3 "$GUARD" "$SANDBOX/if-only.yml" 2>&1 || true)
if python3 "$GUARD" "$SANDBOX/if-only.yml" >/dev/null 2>&1; then
  fail "expected non-zero exit for an if: that reads needs.*.outputs.*"
elif echo "$captured" | grep -q "whose .if:. reads a needs"; then
  pass "exits non-zero and explains the if: finding"
else
  fail "exits non-zero for the wrong reason (got: $captured)"
fi

# ── Test 4: renaming/deleting a required step must fail ─────────────────────────────────────────
# The list is hand-written precisely so that a step vanishing is a finding rather than a silently
# smaller check.
echo "Test 4: a required guard step is renamed away"
sed 's/^      - name: Guard — whole-tree corpus guards (#1569 R17 T1)$/      - name: Something else entirely/' \
  "$WORKFLOW" > "$SANDBOX/renamed.yml"
if ! diff -q "$WORKFLOW" "$SANDBOX/renamed.yml" >/dev/null; then
  captured=$(python3 "$GUARD" "$SANDBOX/renamed.yml" 2>&1 || true)
  if python3 "$GUARD" "$SANDBOX/renamed.yml" >/dev/null 2>&1; then
    fail "expected non-zero exit when a required step no longer exists"
  elif echo "$captured" | grep -q "no step named"; then
    pass "exits non-zero and reports the missing step"
  else
    fail "exits non-zero for the wrong reason (got: $captured)"
  fi
else
  fail "mutation was a no-op — the sed anchor no longer matches the workflow, so this case is vacuous"
fi

# ── Test 5: a top-level pull_request paths: filter must fail ────────────────────────────────────
# Everything above reasons about JOB gating. A `paths:` on the trigger skips the entire workflow,
# which would make every "runs on every PR" claim false at a level no job-level check can see.
echo "Test 5: on.pull_request.paths added"
python3 - "$WORKFLOW" "$SANDBOX/trigger.yml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
out = []
for line in open(src).read().split("\n"):
    out.append(line)
    if line.strip() == "types: [opened, synchronize, reopened]":
        indent = " " * (len(line) - len(line.lstrip()))
        out.append(indent + "paths: ['backend/**']")
open(dst, "w").write("\n".join(out))
PY
captured=$(python3 "$GUARD" "$SANDBOX/trigger.yml" 2>&1 || true)
if python3 "$GUARD" "$SANDBOX/trigger.yml" >/dev/null 2>&1; then
  fail "expected non-zero exit when on.pull_request.paths is set"
elif echo "$captured" | grep -q "on.pull_request.paths"; then
  pass "exits non-zero and names the trigger-level filter"
else
  fail "exits non-zero for the wrong reason (got: $captured)"
fi

# ── Test 6: a run-time-outcome if: (always()/!cancelled()) must NOT fail ────────────────────────
# The false-positive direction. `if: always() && !cancelled()` is what the real host job carries;
# it reacts to prior job outcomes, not to changed paths, and must be accepted. Test 1 covers this
# implicitly; this makes it explicit by adding a second such condition elsewhere.
echo "Test 6: a run-time-outcome if: is accepted"
python3 - "$WORKFLOW" "$SANDBOX/runtime-if.yml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
out = []
for line in open(src).read().split("\n"):
    if line == "    if: always() && !cancelled()":
        out.append("    if: always() && !cancelled() && !failure()")
        continue
    out.append(line)
open(dst, "w").write("\n".join(out))
PY
if ! diff -q "$WORKFLOW" "$SANDBOX/runtime-if.yml" >/dev/null; then
  if python3 "$GUARD" "$SANDBOX/runtime-if.yml" >/dev/null 2>&1; then
    pass "exits 0 for an outcome-only if: (no false positive)"
  else
    fail "false positive: $(python3 "$GUARD" "$SANDBOX/runtime-if.yml" 2>&1 || true)"
  fi
else
  fail "mutation was a no-op — the if: anchor no longer matches, so this case is vacuous"
fi

echo ""
echo "=== $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
