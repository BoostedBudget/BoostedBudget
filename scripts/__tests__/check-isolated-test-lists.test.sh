#!/usr/bin/env bash
# Guard: the hardcoded list of process-isolated frontend test files must stay TRUE and IN SYNC.
#
# Run: bash scripts/__tests__/check-isolated-test-lists.test.sh
#
# The same 22 test-file paths are hand-maintained in TWO places:
#
#   apps/web/scripts/test-conflict-files.sh  GROUP_*_FILES — files that must run in their OWN bun
#                                            process, because bun's mock.module() is process-global
#                                            (see feedback_bun_test_mock_module_global.md);
#   apps/web/scripts/test-regular.sh         EXCLUDE_PATTERNS — the same paths, subtracted from the
#                                            `find src -name '*.test.ts*'` glob that feeds the big
#                                            shared-process suite.
#
# Drift is silent in both directions. A stale path in a MULTI-file group is dropped by bun with
# exit 0 and no message ("Ran 70 tests across 3 files" for a 4-file group — measured, bun 1.3.14),
# so the coverage just disappears. A stale EXCLUDE pattern matches nothing, so a renamed file is
# picked up by `find` and runs IN THE SHARED PROCESS — dropping the MerchantsTab entry turns a
# clean 6400-pass run into 370 failures, because its mock.module('@/shared/components/DataTable')
# stub leaks into every real DataTable consumer.
#
# Two invariants, and they are the whole value of this file:
#   (1) every listed path EXISTS on disk — a real `[ -f ]`, NOT bun's exit code, which is 0 for a
#       stale path in any multi-file group;
#   (2) the two lists are IDENTICAL.
#
# Paths are extracted TEXTUALLY: quoted `src/....test.ts(x)` literals, with whole-line comments
# dropped first so prose that names a test file cannot be mistaken for a list entry. An earlier
# version sourced both runners under stub binaries to observe the arrays; that machinery was ~370
# lines and carried its own defect class (sentinel strings colliding with real paths, a
# "bun was invoked" probe satisfiable by `bun --version`). Do NOT reintroduce it.
#
# Do NOT replace either list with a glob: the grouping in test-conflict-files.sh encodes which
# process-global mock.module() calls may not share a bun process.
set -euo pipefail

FRONTEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../apps/web" && pwd)"

extract() { # <runner>; prints one apps/web-relative test path per line, sorted, deduped
  # `|| true`: a no-match grep exits 1, which under `set -o pipefail` would abort this script
  # before the empty-list check below could name the problem. Emptiness is REPORTED, not crashed on.
  { sed 's/^[[:space:]]*#.*$//' "$1" \
    | grep -oE '"[^"]*src/[A-Za-z0-9_./-]+\.test\.tsx?"' \
    | grep -oE 'src/[A-Za-z0-9_./-]+\.test\.tsx?' | sort -u; } || true
}

FAIL=0
for runner in test-conflict-files.sh test-regular.sh; do
  path="$FRONTEND_DIR/scripts/$runner"
  [ -f "$path" ] || { echo "::error::runner not found: $path — fix the path rather than deleting this guard."; exit 1; }
  list="$(extract "$path")"
  # Fail closed: an empty extraction would make every comparison below vacuously true.
  [ -n "$list" ] || { echo "::error::extracted 0 test paths from $path. Did the list move, or stop being quoted 'src/...test.tsx' literals? Refusing to report a pass over an empty list."; exit 1; }
  while IFS= read -r rel; do
    [ -f "$FRONTEND_DIR/$rel" ] || { echo "::error::$runner lists a path that DOES NOT EXIST: $rel"; FAIL=1; }
  done <<< "$list"
  if [ "$runner" = test-conflict-files.sh ]; then CONFLICT="$list"; else REGULAR="$list"; fi
done

if [ "$CONFLICT" != "$REGULAR" ]; then
  echo "::error::the two isolated-test lists DISAGREE. Only in test-conflict-files.sh (runs isolated but is NOT excluded from the shared suite, so it runs TWICE and pollutes it):"
  comm -23 <(echo "$CONFLICT") <(echo "$REGULAR") | sed 's/^/  /'
  echo "Only in test-regular.sh EXCLUDE_PATTERNS (subtracted from the shared suite but run nowhere — silent coverage loss):"
  comm -13 <(echo "$CONFLICT") <(echo "$REGULAR") | sed 's/^/  /'
  FAIL=1
fi

[ "$FAIL" -eq 0 ] || { echo "❌ Keep both lists identical, with every entry naming a file that exists."; exit 1; }
echo "✅ $(echo "$CONFLICT" | wc -l | tr -d ' ') process-isolated test paths, in sync in both runners, all present on disk:"
echo "$CONFLICT" | sed 's/^/  /'
