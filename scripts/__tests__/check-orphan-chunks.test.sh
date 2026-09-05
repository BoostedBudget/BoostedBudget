#!/usr/bin/env bash
# Negative-case test for scripts/check-orphan-chunks.sh
# Run: bash scripts/__tests__/check-orphan-chunks.test.sh
# Exit 0 on all assertions passing, non-zero on any failure.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/check-orphan-chunks.sh"
PASS=0
FAIL=0

pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

# All tests run inside an mktemp sandbox so they never clobber real dist/ or .size-limit.json
SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

setup_sandbox() {
  rm -rf "$SANDBOX/apps" "$SANDBOX/.size-limit.json"
  mkdir -p "$SANDBOX/apps/web/dist/assets"
}

# Stub .size-limit.json with only vendor-*.js budgeted
write_size_limit() {
  cat > "$SANDBOX/.size-limit.json" <<'JSON'
[
  { "name": "vendor", "path": "apps/web/dist/assets/vendor-*.js", "limit": "900 KB", "gzip": true },
  { "name": "css",    "path": "apps/web/dist/assets/*.css",       "limit": "70 KB",  "gzip": true }
]
JSON
}

echo "=== check-orphan-chunks.sh test suite ==="
echo ""

# ── Test 1: orphan chunk detected (non-zero exit + "ORPHAN CHUNK" in output) ──
echo "Test 1: orphan chunk detected"
setup_sandbox
write_size_limit
touch "$SANDBOX/apps/web/dist/assets/vendor-abc12345.js"
touch "$SANDBOX/apps/web/dist/assets/mystery-def67890.js"  # not in .size-limit.json

captured=$(cd "$SANDBOX" && bash "$SCRIPT" 2>&1 || true)
if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  fail "expected non-zero exit for orphan chunk"
elif echo "$captured" | grep -q "mystery"; then
  pass "exits non-zero and mentions orphan filename"
else
  fail "exits non-zero but orphan filename not in output (got: $captured)"
fi

# ── Test 2: no orphans → exit 0 ──
echo "Test 2: no orphan chunks (clean case)"
setup_sandbox
write_size_limit
touch "$SANDBOX/apps/web/dist/assets/vendor-abc12345.js"
# No other JS files

if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  pass "exits 0 when all JS chunks are budgeted"
else
  fail "expected exit 0 but got non-zero"
fi

# ── Test 3: empty dist directory → non-zero exit (the chunk floor) ──
# ⚠ #1569 r17: this case used to assert exit 0, which pinned the guard's one unconditional pass.
# A present-but-empty assets directory is the worst build outcome this script can observe — the
# build emitted nothing — and it was the single input for which the script could not fail. The
# floor turns it into a failure; Tests 3b/3c pin the floor itself in both directions.
echo "Test 3: empty dist directory (no .js files) fails the chunk floor"
setup_sandbox
write_size_limit
# Leave dist/assets empty (no .js files)

captured=$(cd "$SANDBOX" && bash "$SCRIPT" 2>&1 || true)
if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  fail "expected non-zero exit for an empty dist — a build that emitted nothing must not pass"
elif echo "$captured" | grep -q "below the floor"; then
  pass "exits non-zero and names the chunk floor for an empty dist"
else
  fail "exits non-zero but did not mention the floor (got: $captured)"
fi

# ── Test 3b: an explicit floor above the real chunk count → non-zero exit ──
echo "Test 3b: MIN_JS_CHUNKS floor above the actual chunk count fails"
setup_sandbox
write_size_limit
touch "$SANDBOX/apps/web/dist/assets/vendor-abc12345.js"

captured=$(cd "$SANDBOX" && MIN_JS_CHUNKS=3 bash "$SCRIPT" 2>&1 || true)
if cd "$SANDBOX" && MIN_JS_CHUNKS=3 bash "$SCRIPT" >/dev/null 2>&1; then
  fail "expected non-zero exit when 1 chunk is below MIN_JS_CHUNKS=3"
elif echo "$captured" | grep -q "Only 1 JS chunk"; then
  pass "honours an explicit MIN_JS_CHUNKS floor and reports the real count"
else
  fail "exits non-zero but did not report the count (got: $captured)"
fi

# ── Test 3c: a floor that IS met still passes — the floor must not fail everything ──
echo "Test 3c: MIN_JS_CHUNKS floor that is met still passes"
setup_sandbox
write_size_limit
touch "$SANDBOX/apps/web/dist/assets/vendor-abc12345.js"

if cd "$SANDBOX" && MIN_JS_CHUNKS=1 bash "$SCRIPT" >/dev/null 2>&1; then
  pass "exits 0 when the chunk count meets the floor"
else
  fail "expected exit 0 when the chunk count meets the floor"
fi

# ── Test 4: missing dist directory → non-zero exit ──
echo "Test 4: missing dist directory"
setup_sandbox
write_size_limit
rm -rf "$SANDBOX/apps"  # remove the dist entirely

if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  fail "expected non-zero exit for missing dist"
else
  pass "exits non-zero when dist directory is absent"
fi

# ── Test 5: multiple orphans all reported ──
echo "Test 5: multiple orphan chunks all reported"
setup_sandbox
write_size_limit
touch "$SANDBOX/apps/web/dist/assets/vendor-abc12345.js"
touch "$SANDBOX/apps/web/dist/assets/orphanA-def67890.js"
touch "$SANDBOX/apps/web/dist/assets/orphanB-ghi11111.js"

captured=$(cd "$SANDBOX" && bash "$SCRIPT" 2>&1 || true)
if echo "$captured" | grep -q "orphanA" && echo "$captured" | grep -q "orphanB"; then
  pass "both orphan filenames appear in output"
else
  fail "expected both orphanA and orphanB in output (got: $captured)"
fi

# ── Test 6: chunk with dash in name (e.g. auto-categorization-HASH.js) ──
echo "Test 6: chunk name with dash (auto-categorization-*.js)"
setup_sandbox
cat > "$SANDBOX/.size-limit.json" <<'JSON'
[
  { "name": "auto-categorization", "path": "apps/web/dist/assets/auto-categorization-*.js", "limit": "15 KB", "gzip": true }
]
JSON
touch "$SANDBOX/apps/web/dist/assets/auto-categorization-Bl13G5I.js"

if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  pass "chunk with dash in name correctly matched (not treated as orphan)"
else
  fail "chunk with dash in name incorrectly flagged as orphan"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
