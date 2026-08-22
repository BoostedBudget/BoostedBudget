#!/usr/bin/env bash
# Guard: the hardcoded list of process-isolated frontend test files must stay TRUE and IN SYNC.
#
# Run: bash scripts/__tests__/check-isolated-test-lists.test.sh
# Exit 0 if every assertion passes, non-zero (naming the offending path) otherwise.
#
# ═══ WHY THIS EXISTS ══════════════════════════════════════════════════════════════════════════
#
# The same 22 test-file paths are hand-maintained in TWO places:
#
#   apps/web/scripts/test-conflict-files.sh  GROUP_A_FILES..GROUP_E_FILES — the files that must run
#                                            in their OWN bun process, because bun's mock.module()
#                                            is process-global (see
#                                            feedback_bun_test_mock_module_global.md);
#   apps/web/scripts/test-regular.sh         EXCLUDE_PATTERNS — the same paths, subtracted from the
#                                            `find src -name '*.test.ts*'` glob that feeds the big
#                                            shared-process suite.
#
# Neither script checks that a listed path exists. What `bun test` does with a path that matches
# nothing is the crux, so it is recorded here EXACTLY, measured on bun 1.3.14 — the version pinned
# in .github/workflows/test.yml and the one installed locally:
#
#   * a filter matching nothing, with NO other filter matching either -> EXIT 1 ("had no matches").
#   * a filter matching nothing alongside AT LEAST ONE filter that DOES match -> EXIT 0. bun runs
#     the files it found, reports "Ran 70 tests across 3 files", and says NOTHING about the filter
#     it could not resolve.
#
#     $ cd /private/tmp && bun test --timeout 30000 <3 real paths> <1 renamed path>
#      70 pass / 0 fail
#     Ran 70 tests across 3 files. [913.00ms]
#     $ echo $?  ->  0
#
# The second case is the live hazard, and it maps directly onto the group structure: Groups A (4
# files), B (3) and D (13) are MULTI-file, so a single stale path there is silently dropped and the
# whole script still exits 0. Groups C and E hold ONE file each, so a stale path there happens to
# fail loudly — by luck of arity, not by design. VERIFIED end to end: pointing Group A's
# toggle-group entry at a nonexistent path makes test-conflict-files.sh print
# "Ran 70 tests across 3 files" for a 4-file group and exit 0.
#
# The two directions of a rename therefore differ:
#
#   1. test-conflict-files.sh stops running the file. In a multi-file group `set -e` never fires,
#      the remaining groups run, the job is green, and the coverage is simply gone. SILENT.
#   2. test-regular.sh's exclude pattern no longer matches anything, so `find` picks the renamed
#      file up and it runs IN THE SHARED PROCESS — destroying the isolation that is the entire
#      reason test-conflict-files.sh exists. Measured, this direction is currently LOUD: dropping
#      the MerchantsTab entry turns a clean 6400-pass run into 370 failures, because its
#      process-global mock.module('@/shared/components/DataTable') stub leaks into every real
#      DataTable consumer (dropping the toggle-group entry gives 4 failures).
#
#      Do NOT conclude from that that direction 2 is self-policing. It is loud only when the leaked
#      stub happens to break some OTHER file's assertion. A file whose mocks collide with nothing
#      currently in the suite runs un-isolated and green — and the failure it will eventually cause
#      lands in an unrelated test, months later, looking like flake. The invariant is worth holding
#      on its own terms, not because today's suite happens to shout.
#
# Two invariants, therefore:
#   (1) every listed path EXISTS on disk;
#   (2) the two lists are IDENTICAL to each other.
#
# ═══ DESIGN NOTES — READ BEFORE "IMPROVING" THIS ══════════════════════════════════════════════
#
# * DO NOT "fix" the duplication by replacing the hardcoded list with a glob. It is hardcoded
#   DELIBERATELY: each file belongs to a SPECIFIC group with a specific --preload set, and the
#   grouping encodes which mock.module() calls may not share a process. A glob would defeat the
#   reason the script exists.
#
# * DO NOT copy these assertions into the two runner scripts. That would create a THIRD and FOURTH
#   hand-maintained copy of the same fact — the defect being fixed. One guard, one place.
#
# * Existence is checked with a real filesystem test (`[ -f ]`). It is NOT inferred from bun's
#   exit code, which is 0 for a stale path in any multi-file group (see the transcript above).
#   This distinction is the whole point of the guard.
#
# * The lists are OBSERVED, not text-parsed. The guard SOURCES each runner script with a recording
#   stub named `bun` first on PATH (so nothing is actually executed), then reads the resulting bash
#   arrays. Same rationale as check-frontend-test-timeout.test.sh, which accumulated twelve defects
#   and three demonstrated FALSE PASSES while it tried to parse shell as text: bash is the only
#   correct parser of bash. Sourcing handles comments, quoting, `\` continuations, values arriving
#   from variables, and array expansion for free.
#
# * The conflict-side group variables are DISCOVERED via `compgen -A variable` matching
#   GROUP_<x>_FILES, not enumerated A..E. A new Group F is picked up with no edit here.
#
# * FAIL CLOSED everywhere: an empty list, a missing array, or a source that produces no paths is
#   an ERROR, not a pass. A guard that silently verifies nothing is the failure mode it exists to
#   prevent.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FRONTEND_DIR="$REPO_ROOT/apps/web"
CONFLICT_SCRIPT="$FRONTEND_DIR/scripts/test-conflict-files.sh"
REGULAR_SCRIPT="$FRONTEND_DIR/scripts/test-regular.sh"

FAIL=0
pass() { echo "  PASS: $1"; }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

echo "=== check-isolated-test-lists: process-isolated test list must be true and in sync ==="
echo ""

for s in "$CONFLICT_SCRIPT" "$REGULAR_SCRIPT"; do
  if [ ! -f "$s" ]; then
    echo "::error::runner script not found: $s — this guard can no longer verify anything. Fix the path rather than deleting the guard."
    exit 1
  fi
done

# ── A recording stub PATH so sourcing the runners executes no tests ────────────────────────────
STUB_DIR="$(mktemp -d)"
trap 'rm -rf "$STUB_DIR"' EXIT
for cmd in bun bunx npm npx pnpm yarn; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$STUB_DIR/$cmd"
  chmod +x "$STUB_DIR/$cmd"
done

# Sourced in a child bash so the runners' own `set -e`, `cd`, and variables cannot touch this
# shell. Output is one path per line on stdout; the runners' own chatter goes to /dev/null.
extract_conflict_paths() {
  PATH="$STUB_DIR:$PATH" bash -s -- "$CONFLICT_SCRIPT" <<'INNER'
set -uo pipefail
# shellcheck disable=SC1090
source "$1" >/dev/null 2>&1 || true
vars=$(compgen -A variable | grep -E '^GROUP_[A-Za-z0-9]+_FILES$' | sort || true)
if [ -z "$vars" ]; then
  echo "__NO_GROUP_ARRAYS__"
  exit 0
fi
for v in $vars; do
  eval 'arr=( ${'"$v"'[@]+"${'"$v"'[@]}"} )'
  if [ "${#arr[@]}" -eq 0 ]; then
    echo "__EMPTY_ARRAY__:$v"
    continue
  fi
  printf '%s\n' "${arr[@]}"
done
INNER
}

extract_regular_paths() {
  PATH="$STUB_DIR:$PATH" bash -s -- "$REGULAR_SCRIPT" <<'INNER'
set -uo pipefail
# shellcheck disable=SC1090
source "$1" >/dev/null 2>&1 || true
arr=( ${EXCLUDE_PATTERNS[@]+"${EXCLUDE_PATTERNS[@]}"} )
if [ "${#arr[@]}" -eq 0 ]; then
  echo "__EMPTY_ARRAY__:EXCLUDE_PATTERNS"
  exit 0
fi
printf '%s\n' "${arr[@]}"
INNER
}

# Paths are normalised to be relative to apps/web: test-conflict-files.sh spells them
# "$FRONTEND_DIR/src/...", test-regular.sh spells them "src/...".
normalise() {
  sed -e "s|^$FRONTEND_DIR/||" -e 's|^\./||'
}

# A `source` of a file with a SYNTAX ERROR aborts the sourcing shell outright — `|| true` inside
# the helper cannot catch it, and under `set -e` the assignment below would kill this guard with a
# bare exit 2 and no message. Fail closed LOUDLY instead: an unreadable runner script is exactly
# the case where a silent non-zero is indistinguishable from a crashed guard.
CONFLICT_RAW="$(extract_conflict_paths)" || {
  echo "::error::could not source $CONFLICT_SCRIPT (exit $?) — it likely has a syntax error, so its path arrays cannot be read. Fix the script; this guard is deliberately refusing to pass."
  exit 1
}
REGULAR_RAW="$(extract_regular_paths)" || {
  echo "::error::could not source $REGULAR_SCRIPT (exit $?) — it likely has a syntax error, so EXCLUDE_PATTERNS cannot be read. Fix the script; this guard is deliberately refusing to pass."
  exit 1
}

# ── Fail closed on anything that would make the comparison vacuous ─────────────────────────────
for marker in "$CONFLICT_RAW" "$REGULAR_RAW"; do
  case "$marker" in
    *__NO_GROUP_ARRAYS__*)
      echo "::error::no GROUP_<x>_FILES arrays found in $CONFLICT_SCRIPT. Either the arrays were renamed or sourcing failed; this guard would otherwise compare an empty list and pass. Fix the guard, do not delete it."
      exit 1 ;;
    *__EMPTY_ARRAY__*)
      echo "::error::an expected path array is empty: $(printf '%s\n' "$marker" | grep '__EMPTY_ARRAY__')"
      exit 1 ;;
  esac
done

CONFLICT_PATHS="$(printf '%s\n' "$CONFLICT_RAW" | normalise | sed '/^$/d' | sort)"
REGULAR_PATHS="$(printf '%s\n' "$REGULAR_RAW" | normalise | sed '/^$/d' | sort)"

CONFLICT_COUNT=$(printf '%s\n' "$CONFLICT_PATHS" | sed '/^$/d' | wc -l | tr -d ' ')
REGULAR_COUNT=$(printf '%s\n' "$REGULAR_PATHS" | sed '/^$/d' | wc -l | tr -d ' ')

if [ "$CONFLICT_COUNT" -eq 0 ] || [ "$REGULAR_COUNT" -eq 0 ]; then
  echo "::error::extracted 0 paths (conflict=$CONFLICT_COUNT, regular=$REGULAR_COUNT). Refusing to report success over an empty comparison."
  exit 1
fi

echo "Observed $CONFLICT_COUNT isolated path(s) in test-conflict-files.sh and $REGULAR_COUNT exclude pattern(s) in test-regular.sh"
echo ""

# ── Invariant 0: no duplicates within either list ──────────────────────────────────────────────
echo "Check 1: no duplicate entries within either list"
for label in conflict regular; do
  if [ "$label" = conflict ]; then list="$CONFLICT_PATHS"; else list="$REGULAR_PATHS"; fi
  dupes="$(printf '%s\n' "$list" | uniq -d)"
  if [ -n "$dupes" ]; then
    fail "duplicate entries in the $label list (a duplicate makes the counts lie): $(printf '%s' "$dupes" | tr '\n' ' ')"
  else
    pass "no duplicates in the $label list"
  fi
done
echo ""

# ── Invariant 1: every listed path exists on disk ──────────────────────────────────────────────
# A real `[ -f ]`, deliberately NOT bun's exit code, which is 0 for a nonexistent path.
echo "Check 2: every listed test file exists on disk"
MISSING_CONFLICT=""
while IFS= read -r rel; do
  [ -n "$rel" ] || continue
  [ -f "$FRONTEND_DIR/$rel" ] || MISSING_CONFLICT="$MISSING_CONFLICT $rel"
done <<< "$CONFLICT_PATHS"
if [ -n "$MISSING_CONFLICT" ]; then
  fail "test-conflict-files.sh lists path(s) that DO NOT EXIST:$MISSING_CONFLICT"
  echo "        In a multi-file group bun exits 0 having silently skipped the unresolvable filter,"
  echo "        so these are NOT RUN and nothing goes red. test-regular.sh's matching exclude also"
  echo "        no longer protects anything. Update BOTH lists."
else
  pass "all $CONFLICT_COUNT paths in test-conflict-files.sh exist"
fi

MISSING_REGULAR=""
while IFS= read -r rel; do
  [ -n "$rel" ] || continue
  [ -f "$FRONTEND_DIR/$rel" ] || MISSING_REGULAR="$MISSING_REGULAR $rel"
done <<< "$REGULAR_PATHS"
if [ -n "$MISSING_REGULAR" ]; then
  fail "test-regular.sh EXCLUDE_PATTERNS lists path(s) that DO NOT EXIST:$MISSING_REGULAR"
  echo "        A stale exclude pattern matches nothing, so the file it named — if it was merely"
  echo "        RENAMED — is now collected by the glob and runs in the SHARED process."
else
  pass "all $REGULAR_COUNT patterns in test-regular.sh exist as files"
fi
echo ""

# ── Invariant 2: the two lists are identical ───────────────────────────────────────────────────
echo "Check 3: the two lists are identical"
ONLY_CONFLICT="$(comm -23 <(printf '%s\n' "$CONFLICT_PATHS") <(printf '%s\n' "$REGULAR_PATHS"))"
ONLY_REGULAR="$(comm -13 <(printf '%s\n' "$CONFLICT_PATHS") <(printf '%s\n' "$REGULAR_PATHS"))"

if [ -n "$ONLY_CONFLICT" ]; then
  fail "isolated in test-conflict-files.sh but NOT excluded by test-regular.sh:$(printf '%s' "$ONLY_CONFLICT" | tr '\n' ' ')"
  echo "        These files run TWICE: once isolated, and again in the shared process where their"
  echo "        process-global mock.module() stubs pollute unrelated tests."
fi
if [ -n "$ONLY_REGULAR" ]; then
  fail "excluded by test-regular.sh but NOT run by test-conflict-files.sh:$(printf '%s' "$ONLY_REGULAR" | tr '\n' ' ')"
  echo "        These files are subtracted from the shared suite and never run anywhere — total,"
  echo "        silent coverage loss."
fi
if [ -z "$ONLY_CONFLICT" ] && [ -z "$ONLY_REGULAR" ]; then
  pass "both lists contain exactly the same $CONFLICT_COUNT paths"
fi
echo ""

if [ "$FAIL" -gt 0 ]; then
  echo "❌ $FAIL check(s) failed."
  echo ""
  echo "The two lists in apps/web/scripts/test-conflict-files.sh and apps/web/scripts/test-regular.sh"
  echo "must be kept identical BY HAND, and every entry must name a file that exists. Do NOT replace"
  echo "either list with a glob: the grouping in test-conflict-files.sh encodes which process-global"
  echo "mock.module() calls may not share a bun process."
  exit 1
fi

echo "✅ Process-isolated test list is complete, in sync, and every path exists."
