#!/usr/bin/env bash
# Test suite for scripts/check-review-provenance.sh (the fail-closed adversarial-review
# provenance guard).
#
# Every case builds a throwaway git repo under mktemp with REAL commits, so the guard
# exercises real `git cat-file` / `git merge-base --is-ancestor` / `git rev-list` /
# `git diff` behaviour rather than a mocked history.
#
# The suite is organised as:
#   * positive fixtures  -> the guard must PASS (exit 0). Without these the guard could
#     be "always fail", which is just as useless as "always pass".
#   * negative fixtures  -> the guard must report a review is owed (exit 1).
#   * degenerate inputs  -> the guard must FAIL CLOSED (exit 2), distinctly from exit 1.
#   * a path -> expected-outcome truth table for the exempt-list filter, evaluated as
#     data rather than by reasoning about the filter in isolation.
#
# Run: bash scripts/__tests__/check-review-provenance.test.sh
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/check-review-provenance.sh"
REPO_ROOT="$(cd "$(dirname "$SCRIPT")/.." && pwd)"
PROV_REL="docs/review/last-adversarial-review.txt"
PASS=0
FAIL=0

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX=$(mktemp -d)
trap 'rm -rf "$SANDBOX"' EXIT
REPO="$SANDBOX/repo"

# init_repo: fresh repo with one commit touching a .go file. Exports BASE_SHA.
init_repo() {
  rm -rf "$REPO"
  mkdir -p "$REPO/backend/internal/services" "$REPO/docs/review"
  git -C "$REPO" init -q -b main
  git -C "$REPO" config user.email "test@example.com"
  git -C "$REPO" config user.name "Test"
  echo 'package services' > "$REPO/backend/internal/services/base.go"
  git -C "$REPO" add -A
  git -C "$REPO" commit -q -m "base"
  BASE_SHA="$(git -C "$REPO" rev-parse HEAD)"
}

# write_prov <sha> [reviewer] [date] [evidence]
write_prov() {
  mkdir -p "$REPO/docs/review"
  {
    echo "# provenance fixture"
    echo ""
    echo "reviewed_sha: $1"
    echo "reviewed_range: ${1}..${1}"
    echo "reviewer: ${2:-codex:codex-rescue (fixture)}"
    echo "reviewed_at: ${3:-2026-08-08}"
    echo "evidence: ${4:-fixture session 0000}"
  } > "$REPO/$PROV_REL"
}

# run_guard: runs the guard inside the sandbox repo, sets RC and OUT.
run_guard() {
  OUT="$(cd "$REPO" && bash "$SCRIPT" 2>&1)" && RC=0 || RC=$?
}

# expect_rc <expected> <label> [substring]
expect_rc() {
  if [ "$RC" -ne "$1" ]; then
    fail "$2 -- expected exit $1, got $RC. Output: $OUT"
    return
  fi
  if [ "$#" -ge 3 ] && [[ "$OUT" != *"$3"* ]]; then
    # Bash pattern match on the captured string, never `echo "$OUT" | grep -q` --
    # that pipeline is racy under `pipefail` (grep -q SIGPIPEs the writer).
    fail "$2 -- exited $1 as expected but the message did not mention '$3'. Output: $OUT"
    return
  fi
  pass "$2"
}

echo "=== check-review-provenance.sh test suite ==="
echo ""

# ═══ POSITIVE FIXTURES — the guard must be satisfiable ═══════════════════════
echo "Positive fixtures (guard must PASS):"

# 1. HEAD is exactly the reviewed commit.
init_repo
write_prov "$BASE_SHA"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "prov"
# committing the provenance file moved HEAD; re-point at that commit to model
# "HEAD == reviewed_sha" exactly.
write_prov "$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "prov2"
write_prov "$(git -C "$REPO" rev-parse HEAD)"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "prov3"
HEAD3="$(git -C "$REPO" rev-parse HEAD)"
write_prov "$HEAD3"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "prov4"
# Now HEAD is one commit past HEAD3, and the ONLY changed path is the provenance
# file itself -- the self-exemption that makes the stamp/commit workflow converge.
run_guard
expect_rc 0 "1. only the provenance file changed since the reviewed SHA (self-exemption)" "all 1 exempt"

# 2. Prose-only movement: a .md file.
init_repo
write_prov "$BASE_SHA"
echo "# readme" > "$REPO/README.md"
mkdir -p "$REPO/docs"
echo "# note" > "$REPO/docs/some-note.md"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "docs only"
run_guard
expect_rc 0 "2. docs-only (.md) movement does not demand a fresh review" "exempt"

# 3. HEAD identical to reviewed SHA (zero commits ahead).
init_repo
write_prov "$BASE_SHA"
# provenance written but NOT committed -> HEAD is still BASE_SHA
run_guard
expect_rc 0 "3. HEAD is exactly the reviewed commit (zero commits ahead)" "exactly the reviewed commit"

echo ""
echo "Negative fixtures (guard must report a review is OWED, exit 1):"

# 4. A Go source change.
init_repo
write_prov "$BASE_SHA"
echo 'func New() {}' >> "$REPO/backend/internal/services/base.go"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "behaviour change"
run_guard
expect_rc 1 "4. a .go change since the reviewed SHA demands a fresh review" "base.go"

# 5. CLAUDE.md -- the carve-out from the .md exemption.
init_repo
write_prov "$BASE_SHA"
echo "New standing instruction." > "$REPO/CLAUDE.md"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "agent instructions"
run_guard
expect_rc 1 "5. CLAUDE.md is NOT exempt despite the .md extension (it directs agent behaviour)" "CLAUDE.md"

# 6. An unheard-of file type in an unheard-of directory -- the fail-closed default
#    that an allow-list filter would have let through.
init_repo
write_prov "$BASE_SHA"
mkdir -p "$REPO/some/brand/new/component"
echo "print('hi')" > "$REPO/some/brand/new/component/thing.py"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "new component"
run_guard
expect_rc 1 "6. a path in a directory the filter never heard of requires review (exempt-list is fail-closed)" "thing.py"

# 6b. A .go file RENAMED to a .md file. With git's rename detection on,
#     `git diff --name-only` prints only the DESTINATION path -- a single exempt .md --
#     and the disappearance of the .go file is invisible. The guard passes
#     --no-renames for exactly this reason.
init_repo
write_prov "$BASE_SHA"
git -C "$REPO" mv backend/internal/services/base.go docs-note.md
git -C "$REPO" commit -q -m "rename go -> md"
# Control: confirm rename detection really does collapse this to one exempt path,
# otherwise the fixture would not exercise the bug and a pass would prove nothing.
RENAME_VIEW="$(git -C "$REPO" diff --name-only "$BASE_SHA" HEAD)"
if [[ "$RENAME_VIEW" == *"base.go"* ]]; then
  fail "6b. fixture control failed: rename detection did not hide the .go source path (view: $RENAME_VIEW), so this test does not exercise the --no-renames fix"
else
  run_guard
  expect_rc 1 "6b. a .go file renamed to .md still requires review (--no-renames)" "base.go"
fi

# 6c. Codex bypass #2: an unreviewed behaviour-changing commit, then a second commit
#     that reverts it while touching only an exempt path. The ENDPOINT diff shows one
#     exempt path and would exit 0; the per-commit union sees the .go change.
init_repo
write_prov "$BASE_SHA"
echo 'func Unreviewed() {}' >> "$REPO/backend/internal/services/base.go"
git -C "$REPO" commit -qam "unreviewed behaviour change"
git -C "$REPO" checkout -q "$BASE_SHA" -- backend/internal/services/base.go
echo "# note" > "$REPO/README.md"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "revert the change, touch only prose"
# Control: confirm the endpoint diff really does hide the .go path, otherwise this
# fixture does not exercise the per-commit-union fix and a pass would prove nothing.
ENDPOINT_VIEW="$(git -C "$REPO" diff --name-only "$BASE_SHA" HEAD)"
if [[ "$ENDPOINT_VIEW" == *"base.go"* ]]; then
  fail "6c. fixture control failed: the endpoint diff still shows base.go (view: $ENDPOINT_VIEW), so this does not exercise the per-commit union"
else
  run_guard
  expect_rc 1 "6c. a behaviour change reverted before HEAD is still caught (per-commit union, not endpoint diff)" "base.go"
fi

echo ""
echo "Degenerate inputs (guard must FAIL CLOSED, exit 2 -- distinct from exit 1):"

# 7. Provenance file missing entirely.
init_repo
run_guard
expect_rc 2 "7. missing provenance file fails closed" "does not exist"

# 8. Present but empty.
init_repo
mkdir -p "$REPO/docs/review"
: > "$REPO/$PROV_REL"
run_guard
expect_rc 2 "8. empty provenance file fails closed" "is empty"

# 8b. Comments only -- looks non-empty on disk, parses to nothing.
init_repo
printf '# nothing but a comment\n\n' > "$REPO/$PROV_REL"
run_guard
expect_rc 2 "8b. comments-only provenance file fails closed (not a vacuous pass)" "is empty"

# 9. Malformed / unparseable line.
init_repo
write_prov "$BASE_SHA"
echo "REVIEWED_SHA=$BASE_SHA" >> "$REPO/$PROV_REL"
run_guard
expect_rc 2 "9. unparseable line fails closed" "unparseable"

# 9b. A line that LOOKS like key: value but whose key is an English phrase. A loose
#     'contains a colon' parser accepts this and then silently ignores it.
init_repo
write_prov "$BASE_SHA"
echo "note to self: re-run the review before merging" >> "$REPO/$PROV_REL"
run_guard
expect_rc 2 "9b. a prose line containing a colon is not a valid key/value pair" "unparseable"

# 9c. A declared key with an empty value. Trailing whitespace is stripped before
#     parsing, so "extra_note: " reduces to "extra_note:" and fails the ': ' test --
#     an empty value is never accepted as a value.
init_repo
write_prov "$BASE_SHA"
printf 'extra_note: \n' >> "$REPO/$PROV_REL"
run_guard
expect_rc 2 "9c. a key with an empty value fails closed" "unparseable"

# 10. Missing a required key.
init_repo
write_prov "$BASE_SHA"
/usr/bin/grep -v '^evidence:' "$REPO/$PROV_REL" > "$REPO/$PROV_REL.tmp"
mv "$REPO/$PROV_REL.tmp" "$REPO/$PROV_REL"
run_guard
expect_rc 2 "10. missing required key 'evidence' fails closed" "missing required key"

# 11. A key declared twice -- a record with two answers is not a record.
init_repo
write_prov "$BASE_SHA"
echo "reviewed_sha: $BASE_SHA" >> "$REPO/$PROV_REL"
run_guard
expect_rc 2 "11. duplicate key fails closed" "times"

# 12. Abbreviated SHA.
init_repo
write_prov "${BASE_SHA:0:8}"
run_guard
expect_rc 2 "12. abbreviated (non-40-char) SHA fails closed" "40-character"

# 13. Well-formed SHA that is not a commit in this repository.
init_repo
write_prov "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
run_guard
expect_rc 2 "13. a 40-hex SHA that is not a commit fails closed" "not a commit"

# 14. A real commit that is NOT an ancestor of HEAD (rewritten/unrelated history).
init_repo
# A real, parentless commit object created directly (commit-tree over the empty tree).
# Building it this way avoids `checkout --orphan`, which trips over the working tree,
# and gives a commit that provably EXISTS (so `git cat-file -e` passes and the test
# reaches the ancestry check rather than short-circuiting on step 3).
EMPTY_TREE="$(git -C "$REPO" hash-object -t tree /dev/null)"
ORPHAN_SHA="$(git -C "$REPO" commit-tree "$EMPTY_TREE" -m "unrelated root" </dev/null)"
if ! git -C "$REPO" cat-file -e "${ORPHAN_SHA}^{commit}"; then
  fail "14. fixture control failed: the orphan commit $ORPHAN_SHA does not exist, so this would test case 13 instead"
fi
if git -C "$REPO" merge-base --is-ancestor "$ORPHAN_SHA" HEAD; then
  fail "14. fixture control failed: the orphan commit IS an ancestor of HEAD, so this fixture does not exercise the ancestry check"
fi
write_prov "$ORPHAN_SHA"
run_guard
expect_rc 2 "14. a commit that exists but is NOT an ancestor of HEAD fails closed" "NOT an ancestor"

# 15. A scan that finds nothing to check: commits exist in the range, but the endpoint
#     diff enumerates zero paths. THE Z2 LESSON -- an empty scan is not a pass.
init_repo
write_prov "$BASE_SHA"
cp "$REPO/$PROV_REL" "$SANDBOX/prov-keep"
git -C "$REPO" commit -q --allow-empty -m "empty commit 1"
git -C "$REPO" commit -q --allow-empty -m "empty commit 2"
AHEAD_CHECK="$(git -C "$REPO" rev-list --count "${BASE_SHA}..HEAD")"
DIFF_CHECK="$(git -C "$REPO" diff --name-only "$BASE_SHA" HEAD)"
if [ "$AHEAD_CHECK" -ne 2 ] || [ -n "$DIFF_CHECK" ]; then
  fail "15. fixture control failed: expected 2 commits ahead with an empty diff, got ahead=$AHEAD_CHECK diff='$DIFF_CHECK'"
else
  # Control passed: the range really does contain commits and really does diff to
  # nothing, so this fixture exercises the vacuous-scan path rather than some other one.
  run_guard
  expect_rc 2 "15. commits exist but the path scan enumerated ZERO paths -> fails closed, not a vacuous pass" "ZERO paths"
fi

# 15b. Not inside a git working tree at all: every check below needs git, so the
#      absence of git is absence of evidence, not evidence of absence.
OUT="$(cd "$SANDBOX" && bash "$SCRIPT" 2>&1)" && RC=0 || RC=$?
expect_rc 2 "15b. running outside any git working tree fails closed" "not inside a git working tree"

# 16. Placeholder attribution.
init_repo
write_prov "$BASE_SHA" "TODO"
run_guard
expect_rc 2 "16. placeholder reviewer ('TODO') fails closed" "placeholder"

# 17. Malformed date.
init_repo
write_prov "$BASE_SHA" "codex:codex-rescue (fixture)" "last tuesday"
run_guard
expect_rc 2 "17. non-ISO-8601 reviewed_at fails closed" "ISO-8601"

echo ""
echo "Truth table for the exempt-list filter (path -> expected outcome):"

# Each row: <path>|<exempt: yes|no>. Evaluated one path per fixture repo so the
# outcome is attributable to that path alone -- the filter is judged against
# concrete inputs, not by reasoning about the filter in isolation.
TRUTH_TABLE="README.md|yes
docs/security/posthog-pii-masking.md|yes
.superpowers/sdd/r7-v-batches/task-AD-report.md|yes
$PROV_REL|yes
CLAUDE.md|no
apps/web/CLAUDE.md|no
backend/internal/services/deletion_service.go|no
scripts/check-review-provenance.sh|no
config/subscription_tiers.yaml|no
docs/decisions/bundle-baseline.json|no
.github/workflows/test.yml|no
apps/web/src/App.tsx|no
backend/migrations/000123_add_table.up.sql|no
some/unheard/of/dir/thing.py|no"

TT_ROWS=0
while IFS='|' read -r tt_path tt_exempt; do
  [ -n "$tt_path" ] || continue
  TT_ROWS=$((TT_ROWS + 1))
  init_repo
  write_prov "$BASE_SHA"
  mkdir -p "$REPO/$(dirname "$tt_path")"
  echo "content for truth-table row" > "$REPO/$tt_path"
  # The provenance file row needs the file to still be valid provenance, not junk.
  if [ "$tt_path" = "$PROV_REL" ]; then
    write_prov "$BASE_SHA" "codex:codex-rescue (fixture, re-stamped)"
  fi
  git -C "$REPO" add -A && git -C "$REPO" commit -q -m "truth table: $tt_path"
  run_guard
  if [ "$tt_exempt" = "yes" ]; then
    expect_rc 0 "  $tt_path -> exempt (no fresh review)"
  else
    expect_rc 1 "  $tt_path -> requires a fresh review"
  fi
done <<< "$TRUTH_TABLE"

if [ "$TT_ROWS" -lt 10 ]; then
  fail "truth table only evaluated $TT_ROWS rows -- the table was not read (a vacuous table proves nothing)"
else
  pass "truth table evaluated $TT_ROWS rows"
fi

echo ""
echo "--stamp workflow (the guard must be satisfiable by a documented action):"

# 18. --stamp with no attribution must refuse.
init_repo
OUT="$(cd "$REPO" && bash "$SCRIPT" --stamp 2>&1)" && RC=0 || RC=$?
expect_rc 2 "18. --stamp without reviewer/evidence refuses (no unattributed stamps)" "mandatory"

# 19. --stamp then re-run: the guard goes green, and committing the stamped file
#     keeps it green. This is the documented update path; if it did not converge
#     the guard would be unsatisfiable and would simply be deleted.
init_repo
write_prov "$BASE_SHA"
echo 'func New() {}' >> "$REPO/backend/internal/services/base.go"
git -C "$REPO" add -A && git -C "$REPO" commit -q -m "behaviour change"
run_guard
if [ "$RC" -ne 1 ]; then
  fail "19. fixture control failed: expected the guard to be red before stamping, got $RC"
else
  (cd "$REPO" && bash "$SCRIPT" --stamp "feature-dev:code-reviewer (fallback, Codex capped)" "report task-AD-report.md" >/dev/null)
  run_guard
  if [ "$RC" -ne 0 ]; then
    fail "19. --stamp did not make the guard green. Output: $OUT"
  else
    git -C "$REPO" add -A && git -C "$REPO" commit -q -m "stamp review"
    run_guard
    expect_rc 0 "19. --stamp turns the guard green, and committing the stamp keeps it green (workflow converges)"
  fi
fi

echo ""
echo "Real repository (the guard must be able to parse this repo's own record):"

# 20. The real provenance file must at least PARSE. Exit 1 (a review is owed) is a
#     legitimate state mid-round; exit 2 is not -- it means the record itself is broken.
OUT="$(cd "$REPO_ROOT" && bash "$SCRIPT" 2>&1)" && RC=0 || RC=$?
if [ "$RC" -eq 2 ]; then
  fail "20. the real $PROV_REL failed to parse / validate (exit 2). Output: $OUT"
elif [ "$RC" -eq 0 ] || [ "$RC" -eq 1 ]; then
  pass "20. the real $PROV_REL parses and validates (exit $RC: $([ "$RC" -eq 0 ] && echo 'up to date' || echo 'a review is owed'))"
else
  fail "20. unexpected exit $RC against the real repository. Output: $OUT"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
