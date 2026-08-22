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
# Paths are extracted TEXTUALLY: DOUBLE-QUOTED `src/....test.ts(x)` literals, with comments —
# whole-line AND trailing — dropped first so prose that names a test file cannot be mistaken for a
# list entry. (A trailing comment used to manufacture a phantom: `FOO=("src/real/A.test.tsx")  #
# see also "src/ghost/B.test.tsx"` extracted the ghost and then failed its own existence check.)
#
# Requiring quotes is DELIBERATE and is not relaxed: both runners contain prose that names test
# files, so matching unquoted text would manufacture phantom entries. But a shell array does not
# NEED quotes for these paths, so an unquoted entry — or one built from a variable,
# `"src/$DIR/X.test.tsx"` — is a plausible edit that the quoted-literal reader simply cannot see.
# Silently dropping it is the worst possible outcome: BOTH invariants below are computed from the
# extracted set, so an invisible entry is neither existence-checked NOR compared, and the guard
# prints its success message while the lists genuinely disagree. So there is a THIRD invariant:
#   (0) every token on a non-comment line that LOOKS like a test path must be a readable
#       double-quoted literal — otherwise the guard fails and names the line.
# An unreadable entry is a hard error, never an omission.
#
# An earlier
# version sourced both runners under stub binaries to observe the arrays; that machinery was ~370
# lines and carried its own defect class (sentinel strings colliding with real paths, a
# "bun was invoked" probe satisfiable by `bun --version`). Do NOT reintroduce it.
#
# Do NOT replace either list with a glob: the grouping in test-conflict-files.sh encodes which
# process-global mock.module() calls may not share a bun process.
set -euo pipefail

FRONTEND_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../apps/web" && pwd)"

# A test-path-shaped token, matched LOOSELY: anything up to whitespace or a double quote. Wider
# than the strict class below on purpose — it must still see `src/$DIR/X.test.tsx`, whose `$` the
# strict class rejects, or a variable-built entry would stay invisible to the readability check.
LOOSE_TOKEN='src/[^[:space:]"]*\.test\.tsx?'
# A readable entry: a DOUBLE-QUOTED literal with no shell expansion in the path.
QUOTED_LITERAL='"[^"]*src/[A-Za-z0-9_./-]+\.test\.tsx?"'
STRICT_TOKEN='src/[A-Za-z0-9_./-]+\.test\.tsx?'

# Drop comments, whole-line AND trailing. `#` only starts a comment at the start of a word in sh,
# so anchoring on start-of-line-or-whitespace leaves `${#ARR[@]}` and `#1234` inside a path alone.
uncomment() { sed -E 's/(^|[[:space:]])#.*$//'; }

extract() { # <runner>; prints one apps/web-relative test path per line, sorted, deduped
  # `|| true`: a no-match grep exits 1, which under `set -o pipefail` would abort this script
  # before the empty-list check below could name the problem. Emptiness is REPORTED, not crashed on.
  { uncomment < "$1" \
    | grep -oE "$QUOTED_LITERAL" \
    | grep -oE "$STRICT_TOKEN" | sort -u; } || true
}

check_readable() { # <runner>; 0 if every test-path-shaped token on the line is a quoted literal
  local rc=0 lineno=0 line stripped quoted tok hint
  while IFS= read -r line || [ -n "$line" ]; do
    lineno=$((lineno + 1))
    stripped="$(printf '%s\n' "$line" | uncomment)"
    # Readable entries ON THIS LINE. Compared per-line, not against the whole file: the same path
    # being quoted somewhere else must not excuse an unreadable occurrence here.
    quoted="$(printf '%s\n' "$stripped" | grep -oE "$QUOTED_LITERAL" | grep -oE "$STRICT_TOKEN" || true)"
    while IFS= read -r tok; do
      [ -n "$tok" ] || continue
      printf '%s\n' "$quoted" | grep -qxF "$tok" && continue
      case "$tok" in
        *'$'*) hint='it is built from a shell variable, so its real value is not knowable by reading the file' ;;
        *)     hint='it is not inside double quotes' ;;
      esac
      echo "::error::$(basename "$1"):$lineno names a test path this guard CANNOT READ ($hint): $tok"
      echo "  This guard reads double-quoted \"src/....test.ts(x)\" literals only — quote it as a plain"
      echo "  literal, or the guard cannot see it, and an entry it cannot see is neither existence-checked"
      echo "  nor compared against the other runner. Line: ${line#"${line%%[![:space:]]*}"}"
      rc=1
    done < <(printf '%s\n' "$stripped" | grep -oE "$LOOSE_TOKEN" || true)
  done < "$1"
  return "$rc"
}

FAIL=0
for runner in test-conflict-files.sh test-regular.sh; do
  path="$FRONTEND_DIR/scripts/$runner"
  [ -f "$path" ] || { echo "::error::runner not found: $path — fix the path rather than deleting this guard."; exit 1; }
  # Invariant (0) first: an entry the extractor cannot see would make the two checks below
  # vacuously true FOR THAT ENTRY, so report it before trusting anything derived from the set.
  check_readable "$path" || FAIL=1
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

[ "$FAIL" -eq 0 ] || { echo "❌ Keep both lists identical, every entry a readable quoted literal naming a file that exists."; exit 1; }
echo "✅ $(echo "$CONFLICT" | wc -l | tr -d ' ') process-isolated test paths, in sync in both runners, all present on disk:"
echo "$CONFLICT" | sed 's/^/  /'
