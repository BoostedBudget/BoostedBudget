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

# ── Test 3: empty dist directory → exit 0 (guard skips gracefully) ──
echo "Test 3: empty dist directory (no .js files)"
setup_sandbox
write_size_limit
# Leave dist/assets empty (no .js files)

if cd "$SANDBOX" && bash "$SCRIPT" >/dev/null 2>&1; then
  pass "exits 0 for empty dist (no files to check)"
else
  fail "expected exit 0 for empty dist"
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
