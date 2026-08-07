#!/usr/bin/env bash
# Test suite for scripts/check-typographic-quotes.sh (#1487 prevention).
#
# Builds a throwaway git repo under mktemp so the guard can exercise its real
# `git ls-files` scoping (tracked-vs-untracked, path-scoped) without touching
# this repository's own tree. Run: bash scripts/__tests__/check-typographic-quotes.test.sh
# Exit 0 if every assertion passes, non-zero otherwise.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/check-typographic-quotes.sh"
PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT

# U+201C / U+201D as literal UTF-8 bytes, built the same way the guard itself
# builds them -- avoids depending on this test file's own encoding surviving
# any tool in the write path.
LEFT_DQUOTE=$'\xe2\x80\x9c'
RIGHT_DQUOTE=$'\xe2\x80\x9d'

init_repo() {
  rm -rf "$SANDBOX/repo"
  mkdir -p "$SANDBOX/repo/backend/internal/repository"
  git -C "$SANDBOX/repo" init -q
  git -C "$SANDBOX/repo" config user.email "test@example.com"
  git -C "$SANDBOX/repo" config user.name "Test"
}

echo "=== check-typographic-quotes.sh test suite ==="
echo ""

# ── Test 1 (negative fixture): clean tracked .go file -> exit 0 ──
echo "Test 1: clean tracked .go file (no typographic quotes)"
init_repo
cat > "$SANDBOX/repo/backend/internal/repository/clean.go" <<'GOEOF'
package repository

// Query selects rows where deleted_at is null: `deleted_at <> ''`.
func Query() string {
	return "SELECT 1 WHERE x <> ''"
}
GOEOF
git -C "$SANDBOX/repo" add backend/internal/repository/clean.go
git -C "$SANDBOX/repo" commit -q -m "clean fixture"

if (cd "$SANDBOX/repo" && bash "$SCRIPT" backend) >/tmp/out1.$$ 2>&1; then
  pass "exits 0 on a clean tracked .go file"
else
  fail "expected exit 0 on clean fixture, got non-zero. Output:"
  cat /tmp/out1.$$
fi
rm -f /tmp/out1.$$

# ── Test 2 (positive fixture): tracked .go file WITH U+201D -> exit 1 ──
echo "Test 2: tracked .go file containing U+201D (the corruption)"
init_repo
{
  printf 'package repository\n\n'
  printf '// Query selects rows where deleted_at is null: `deleted_at <> %s`.\n' "$RIGHT_DQUOTE"
  printf 'func Query() string {\n'
  printf '\treturn "SELECT 1 WHERE x <> \x27\x27"\n'
  printf '}\n'
} > "$SANDBOX/repo/backend/internal/repository/corrupt.go"
git -C "$SANDBOX/repo" add backend/internal/repository/corrupt.go
git -C "$SANDBOX/repo" commit -q -m "corrupt fixture"

captured=$(cd "$SANDBOX/repo" && bash "$SCRIPT" backend 2>&1) && rc=0 || rc=$?
if [ "$rc" -eq 0 ]; then
  fail "expected non-zero exit for U+201D fixture, got 0. Output: $captured"
elif echo "$captured" | /usr/bin/grep -q "corrupt.go"; then
  pass "exits non-zero and names the offending file"
else
  fail "exited non-zero but did not name corrupt.go. Output: $captured"
fi

# ── Test 3 (scoping — falsifiability of git-ls-files-only design): an ──
# UNTRACKED file containing U+201D must NOT fail the guard.
echo "Test 3: untracked .go file containing U+201D is ignored (tracked-only scope)"
init_repo
cat > "$SANDBOX/repo/backend/internal/repository/clean2.go" <<'GOEOF'
package repository

func OK() string { return "fine" }
GOEOF
git -C "$SANDBOX/repo" add backend/internal/repository/clean2.go
git -C "$SANDBOX/repo" commit -q -m "clean fixture 2"

printf 'package repository\n\n// scratch note %s\nfunc Untracked() {}\n' "$RIGHT_DQUOTE" \
  > "$SANDBOX/repo/backend/internal/repository/untracked_scratch.go"
# deliberately not `git add`-ed

if (cd "$SANDBOX/repo" && bash "$SCRIPT" backend) >/tmp/out3.$$ 2>&1; then
  pass "exits 0 -- untracked file with U+201D is correctly ignored"
else
  fail "expected exit 0 (untracked file should be invisible to git ls-files), got non-zero. Output:"
  cat /tmp/out3.$$
fi
rm -f /tmp/out3.$$

# ── Test 4: tracked non-.go file containing U+201D under backend/ is ignored ──
echo "Test 4: tracked non-.go file (e.g. .md) containing U+201D under backend/ is ignored"
init_repo
git -C "$SANDBOX/repo" add backend/internal/repository/clean.go 2>/dev/null || true
cat > "$SANDBOX/repo/backend/internal/repository/clean.go" <<'GOEOF'
package repository

func OK() string { return "fine" }
GOEOF
printf '# notes\n\nSome prose with a typographic quote: %s%s\n' "$LEFT_DQUOTE" "$RIGHT_DQUOTE" \
  > "$SANDBOX/repo/backend/NOTES.md"
git -C "$SANDBOX/repo" add backend/internal/repository/clean.go backend/NOTES.md
git -C "$SANDBOX/repo" commit -q -m "go + markdown fixture"

if (cd "$SANDBOX/repo" && bash "$SCRIPT" backend) >/tmp/out4.$$ 2>&1; then
  pass "exits 0 -- a tracked .md file with typographic quotes under backend/ is correctly out of scope"
else
  fail "expected exit 0 (.go-only scope should ignore .md), got non-zero. Output:"
  cat /tmp/out4.$$
fi
rm -f /tmp/out4.$$

# ── Test 5: no tracked .go files at all under the scan root -> exit 0 ──
echo "Test 5: scan root with zero tracked .go files"
init_repo
mkdir -p "$SANDBOX/repo/backend"
touch "$SANDBOX/repo/backend/.gitkeep"
git -C "$SANDBOX/repo" add backend/.gitkeep
git -C "$SANDBOX/repo" commit -q -m "empty backend fixture"

if (cd "$SANDBOX/repo" && bash "$SCRIPT" backend) >/tmp/out5.$$ 2>&1; then
  pass "exits 0 when there are no tracked .go files to scan"
else
  fail "expected exit 0 on empty scan root, got non-zero. Output:"
  cat /tmp/out5.$$
fi
rm -f /tmp/out5.$$

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
