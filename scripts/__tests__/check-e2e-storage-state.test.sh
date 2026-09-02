#!/usr/bin/env bash
# Negative + positive fixture test for scripts/check-e2e-storage-state.py — the E2E storage-state
# guard extracted from .github/workflows/test.yml (#1464 batch T2, G5-13).
#
# Before this test existed, CI only ever fed the guard the real, valid Playwright storage-state
# file — none of the malformed shapes it was written to reject had ever actually been rejected by
# a test run (an unfalsified guard is the same shape as ticket #1476). Each case below feeds a
# fixture through the SAME script the workflow calls and asserts the observed exit code / message,
# plus one fixture that must PASS so the suite proves the guard discriminates rather than
# rejecting everything.
#
# Run: bash scripts/__tests__/check-e2e-storage-state.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -euo pipefail

# PROSE-ASSERTION AUDIT (#1569 task 7.1). "E2E auth session verified" below matches a stable
# PREFIX of a templated f-string (check-e2e-storage-state.py interpolates cookie counts/names
# after it) — the dynamic part is correctly left unasserted. Decision: CONTRACT for the prefix;
# it names the mechanism (session verified), not a scope claim about what a count measures, so
# it does not carry the typecheck-gate failure mode. Left unchanged.

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/check-e2e-storage-state.py"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# assert_rejected <fixture-json> <case-name> <expected-error-substring>
assert_rejected() {
  local json="$1" name="$2" expect="$3"
  local file="$SANDBOX/fixture.json"
  printf '%s' "$json" > "$file"

  local output
  if output=$(python3 "$SCRIPT" "$file" 2>&1); then
    fail "$name: expected non-zero exit, got 0 (output: $output)"
    return
  fi
  if echo "$output" | grep -q "$expect"; then
    pass "$name: rejected with expected message"
  else
    fail "$name: rejected but message missing '$expect' (got: $output)"
  fi
}

echo "=== check-e2e-storage-state.py test suite ==="
echo ""

echo "Case 1: cookies is a string, not a list"
assert_rejected '{"cookies": "x"}' "cookies-is-string" "not a JSON array"

echo "Case 2: cookies is a list whose entry is not a dict"
assert_rejected '{"cookies": [null]}' "cookies-entry-not-dict" "none are objects with non-empty"

echo "Case 3: cookies is a dict, not a list"
assert_rejected '{"cookies": {"k": 1}}' "cookies-is-dict" "not a JSON array"

echo "Case 4: root is not an object (a bare list)"
assert_rejected '[]' "root-not-object" "root is not a JSON object"

echo "Case 5: cookies is an empty list"
assert_rejected '{"cookies": []}' "cookies-empty" "zero cookies"

echo "Case 6: structurally valid cookies, but no Clerk session cookie"
assert_rejected '{"cookies": [{"name": "other", "value": "v"}]}' "no-session-cookie" "the Clerk session cookie is missing"

echo "Case 7: a valid storage-state file MUST pass"
file="$SANDBOX/valid.json"
cat > "$file" <<'JSON'
{
  "cookies": [
    {"name": "__session_JB79cyGe", "value": "eyJabc123", "domain": "example.com"},
    {"name": "__client_uat", "value": "1700000000", "domain": "example.com"}
  ],
  "origins": [
    {"origin": "https://example.com", "localStorage": [{"name": "some-key", "value": "some-value"}]}
  ]
}
JSON
if output=$(python3 "$SCRIPT" "$file" 2>&1); then
  if echo "$output" | grep -q "E2E auth session verified"; then
    pass "valid storage-state file is accepted"
  else
    fail "valid storage-state file exited 0 but message unexpected (got: $output)"
  fi
else
  fail "valid storage-state file was rejected (got: $output)"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
