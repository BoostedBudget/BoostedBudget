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
elif [[ "$captured" == *"corrupt.go"* ]]; then
  # Bash pattern match on the already-captured string, not
  # `echo ... | grep -q` -- that pipeline is racy under `pipefail`: `grep -q`
  # exits on its first match and SIGPIPEs the writer, which this repo has
  # been bitten by before.
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

# ── Test 5 (falsifiability of defect #1 -- empty scan root must NOT pass): ──
# no tracked .go files at all under the scan root -> exit 1, not 0.
# This USED to assert exit 0 here, enshrining the vacuous-pass bug into the
# contract: a renamed/moved "backend" directory, or a wrong argument, would
# make FILE_LIST empty and the old script printed "nothing to scan" and
# exited 0 -- a scan of zero files "passing" silently. It must fail instead.
echo "Test 5: scan root with zero tracked .go files must FAIL, not pass"
init_repo
mkdir -p "$SANDBOX/repo/backend"
touch "$SANDBOX/repo/backend/.gitkeep"
git -C "$SANDBOX/repo" add backend/.gitkeep
git -C "$SANDBOX/repo" commit -q -m "empty backend fixture"

captured=$(cd "$SANDBOX/repo" && bash "$SCRIPT" backend 2>&1) && rc=0 || rc=$?
if [ "$rc" -eq 0 ]; then
  fail "expected non-zero exit when there are zero tracked .go files under the scan root (a vacuous pass), got 0. Output: $captured"
elif [[ "$captured" == *"no tracked .go files"* ]]; then
  pass "exits non-zero and explains why when there are zero tracked .go files to scan"
else
  fail "exited non-zero but did not explain the empty-scan-root failure. Output: $captured"
fi

# ── Test 6 (falsifiability of defect #3 -- core.quotePath hiding non-ASCII ──
# paths): a tracked .go file whose FILENAME contains a non-ASCII byte must
# still be scanned and still be caught when its content has U+201D. Without
# `-c core.quotePath=false`, `git ls-files` prints this path C-quoted as
# `"backend/.../café.go"` (with a literal trailing `"` byte), which the
# `\.go$` filter never matches -- so the file is silently dropped from the
# scan and a real corruption inside it would never be reported.
echo "Test 6: tracked .go file with a non-ASCII filename containing U+201D is still caught (core.quotePath)"
init_repo
NONASCII_REL="backend/internal/repository/café.go"
{
  printf 'package repository\n\n'
  printf '// Query selects rows where deleted_at is null: `deleted_at <> %s`.\n' "$RIGHT_DQUOTE"
  printf 'func Query() string {\n'
  printf '\treturn "SELECT 1 WHERE x <> \x27\x27"\n'
  printf '}\n'
} > "$SANDBOX/repo/$NONASCII_REL"
git -C "$SANDBOX/repo" add "$NONASCII_REL"
git -C "$SANDBOX/repo" commit -q -m "non-ascii filename fixture"

# Control: confirm git's default (quotePath=true) actually C-quotes this
# path -- otherwise this fixture would not exercise the bug at all and a
# pass here would prove nothing. A passing control validates the PATTERN
# (that plain `git ls-files` quotes this path); the test below validates the
# guard against that confirmed input.
DEFAULT_LISTING="$(git -C "$SANDBOX/repo" ls-files -- backend)"
if [[ "$DEFAULT_LISTING" != *'"'* ]]; then
  fail "fixture control failed: git's default core.quotePath did not quote the non-ASCII filename, so this test would not exercise defect #3. Listing: $DEFAULT_LISTING"
else
  captured=$(cd "$SANDBOX/repo" && bash "$SCRIPT" backend 2>&1) && rc=0 || rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "expected non-zero exit for U+201D inside a non-ASCII-named tracked file, got 0 (core.quotePath hid the file from the scan). Output: $captured"
  elif [[ "$captured" == *"café.go"* ]]; then
    pass "exits non-zero and names the offending non-ASCII-named file"
  else
    fail "exited non-zero but did not name café.go. Output: $captured"
  fi
fi

# ── Test 7 (falsifiability of defect #2 -- missing grep fails open): if ──
# grep cannot run at all, the guard must fail loudly (and distinctly from a
# real violation), never report "PASS -- 0 occurrences". There is no way to
# make the real /usr/bin/grep vanish from this sandbox, so this points the
# script's grep-binary override at a path that does not exist -- the same
# failure mode a missing/broken /usr/bin/grep would produce.
echo "Test 7: a broken/missing grep binary fails closed, not open"
init_repo
cat > "$SANDBOX/repo/backend/internal/repository/clean3.go" <<'GOEOF'
package repository

func OK() string { return "fine" }
GOEOF
git -C "$SANDBOX/repo" add backend/internal/repository/clean3.go
git -C "$SANDBOX/repo" commit -q -m "clean fixture for grep-missing test"

NONEXISTENT_GREP="$SANDBOX/no-such-grep-binary"
captured=$(cd "$SANDBOX/repo" && CHECK_TYPOGRAPHIC_QUOTES_GREP_BIN="$NONEXISTENT_GREP" bash "$SCRIPT" backend 2>&1) && rc=0 || rc=$?
if [ "$rc" -eq 0 ]; then
  fail "expected non-zero exit when grep cannot run, got 0 (fail-open on a missing grep binary, reporting a false PASS). Output: $captured"
elif [ "$rc" -ge 2 ]; then
  pass "exits with a distinct error code ($rc) when grep cannot run -- not the same exit code as a real violation (1)"
else
  fail "exited $rc when grep could not run (expected >=2, distinct from the found-violation exit of 1). Output: $captured"
fi

# ── Test 8 (sibling, mandatory: the fixes above must not turn the guard ──
# into something that always fails): the real repository's real backend/
# tree must still pass, and must not be a near-vacuous scan itself -- if the
# reported file count collapsed to something tiny, that is exactly the class
# of "scope silently narrowed" bug defect #1 exists to catch, just showing up
# as a low count instead of zero. 200 is a floor chosen to be far below the
# real count (~770 tracked backend/**/*.go files today) so ordinary code
# churn never trips it, and far above the single-digit range a broken scope
# would actually produce -- not a hard-coded exact count, which would rot as
# the codebase grows or shrinks.
echo "Test 8: the real backend/ tree still passes and is not a near-vacuous scan"
REPO_ROOT="$(cd "$(dirname "$SCRIPT")/.." && pwd)"
captured=$(cd "$REPO_ROOT" && bash "$SCRIPT" backend 2>&1) && rc=0 || rc=$?
MIN_EXPECTED_FILES=200
if [ "$rc" -ne 0 ]; then
  fail "expected the real backend/ tree to pass, got exit $rc. Output: $captured"
else
  SCANNED_COUNT="$(printf '%s\n' "$captured" | /usr/bin/grep -oE 'across [0-9]+ tracked' | /usr/bin/grep -oE '[0-9]+' || true)"
  if [ -z "$SCANNED_COUNT" ]; then
    fail "could not parse the scanned-file count out of the PASS output: $captured"
  elif [ "$SCANNED_COUNT" -lt "$MIN_EXPECTED_FILES" ]; then
    fail "real backend/ tree only reported $SCANNED_COUNT scanned .go files (< $MIN_EXPECTED_FILES) -- looks like the scan scope silently narrowed. Output: $captured"
  else
    pass "real backend/ tree passes and reports $SCANNED_COUNT scanned .go files (>= $MIN_EXPECTED_FILES floor)"
  fi
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
