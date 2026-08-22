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
# * The lists are OBSERVED, not text-parsed. The guard SOURCES each runner script with RECORDING
#   stubs (`bun`, `bunx`, `npm`, ...) first on PATH, then reads the resulting bash arrays. Same
#   rationale as check-frontend-test-timeout.test.sh, which accumulated twelve defects and three
#   demonstrated FALSE PASSES while it tried to parse shell as text: bash is the only correct
#   parser of bash. Sourcing handles comments, quoting, `\` continuations, values arriving from
#   variables, and array expansion for free.
#
#   The stubs APPEND every invocation (name + argv) to a log and exit 0, and the guard then
#   ASSERTS that `bun` appears in that log. That assertion is not decoration: a stub on PATH is
#   only reached for a BARE command name, so a runner that invoked bun by absolute path — or via
#   $(which bun), or from a node_modules/.bin path — would bypass the stub entirely, really run
#   the suite, and the guard could still report a pass over whatever it happened to observe. If
#   that assertion ever fires, either the invocation was moved behind a `[ "${BASH_SOURCE[0]}" =
#   "$0" ]` main-guard (in which case source-time observation of the arrays is still fine and this
#   assertion is what needs relaxing) or the stub is being bypassed (in which case fix the runner).
#
# * SOURCING EXECUTES THE RUNNER'S TOP-LEVEL CODE. This is a real constraint on what those two
#   scripts may contain, not an implementation detail: `source` runs everything at file scope,
#   including test-conflict-files.sh's five trailing `run_group` calls. It is harmless TODAY only
#   because every command those lines reach is stubbed out. A future runner that did filesystem or
#   network work at top level — `rm -rf coverage/`, a `curl` to an artifact store, a `git` write —
#   would have that work EXECUTED inside CI by this guard. The stub set below therefore covers the
#   package managers plus the obvious side-effecting commands (`curl`, `wget`, `rm`, `git`), which
#   neutralises the common cases cheaply but is NOT a sandbox. Keep side effects in these runners
#   behind a function that only a direct `bash runner.sh` invocation calls.
#
# * The conflict-side group variables are DISCOVERED via `compgen -A variable` matching
#   GROUP_<x>_FILES, not enumerated A..E. A new Group F is picked up with no edit here. The child
#   UNSETS every matching name BEFORE sourcing: `compgen -A variable` lists exported environment
#   variables too, so an unrelated `GROUP_CACHE_FILES=...` in the CI environment would otherwise be
#   mistaken for a group and fail the existence check — a false alarm sourced from the environment
#   rather than from the repo. Verified: `GROUP_CACHE_FILES=x bash -c 'compgen -A variable | grep
#   -E "^GROUP_[A-Za-z0-9]+_FILES$"'` prints GROUP_CACHE_FILES.
#
# * FAIL CLOSED everywhere: an empty list, a missing array, or a source that produces no paths is
#   an ERROR, not a pass. A guard that silently verifies nothing is the failure mode it exists to
#   prevent.
#
# * A SYNTAX ERROR is caught by `bash -n` BEFORE sourcing, and the source status is captured
#   afterwards rather than being swallowed by `|| true`. Both halves are load-bearing, because
#   sourcing cannot detect a parse error reliably. MEASURED on bash 3.2: without `set -e` a syntax
#   error does NOT abort the sourcing shell — `source` returns non-zero and execution CONTINUES, so
#   a runner that assigns all of its arrays and only THEN hits a syntax error hands back fully
#   populated arrays (the old `|| true` turned exactly that into rc=0 and the guard passed); WITH
#   `set -e`, which both runners set, the shell does exit, but `$?` in the EXIT trap is the status
#   of the last SUCCESSFUL command — observed 0 — so the trap cannot see the parse error either.
#   Outcomes are reported as explicit markers: __SOURCE_FAILED__:<rc> (the runner exited non-zero),
#   __NO_GROUP_ARRAYS__ / __NO_EXCLUDE_ARRAY__ (sourced cleanly but defines nothing),
#   __EMPTY_ARRAY__:<name>, or a list of paths.
#
# * A runner that legitimately ends in `exit 0` must NOT read as "no arrays found". `exit` inside a
#   sourced file terminates the SOURCING shell, so any post-source `compgen` never runs. The child
#   therefore emits from an EXIT trap, which fires either way, and reports the exit status it saw.
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

# ── Recording stub PATH so sourcing the runners executes no tests ──────────────────────────────
# Each stub APPENDS "<name>|<argv>" to $BB_STUB_LOG and exits 0, and the guard asserts below that
# `bun` shows up there. See the header for why that assertion matters (a stub is only reached for a
# BARE command name) and for the side-effect constraint that makes `curl`/`wget`/`rm`/`git` worth
# stubbing as well: sourcing EXECUTES the runners' top-level code.
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
STUB_DIR="$WORK_DIR/stubs"
mkdir -p "$STUB_DIR"
for cmd in bun bunx npm npx pnpm yarn node deno curl wget git rm; do
  cat > "$STUB_DIR/$cmd" <<STUB
#!/usr/bin/env bash
printf '%s|%s\n' "$cmd" "\$*" >> "\$BB_STUB_LOG"
exit 0
STUB
  chmod +x "$STUB_DIR/$cmd"
done

# ── The extractor child ────────────────────────────────────────────────────────────────────────
# Runs in its own bash so the runners' `set -e`, `cd`, `shift` and variables cannot touch this
# shell. Emits one path per line on stdout, or a __MARKER__ line, and ALWAYS exits 0 — so it is a
# marker, never an exit code, that the parent interprets. Parse errors are NOT its job: the caller
# runs `bash -n` first, for the reasons given in the header. What it does have to get right is a
# runner that finishes by calling `exit`, which terminates this child before any post-source
# `compgen` could run; emission therefore happens from an EXIT trap, on fd 3.
cat > "$WORK_DIR/extract.sh" <<'EXTRACT'
#!/usr/bin/env bash
# argv: <runner-script> <mode: conflict|regular> <stderr-capture-file>
set -uo pipefail

RUNNER="$1"; MODE="$2"; ERRFILE="$3"

# `compgen -A variable` lists INHERITED environment variables too, so drop any pre-existing name
# that would masquerade as a runner-defined array before sourcing anything.
for v in $(compgen -A variable | grep -E '^(GROUP_[A-Za-z0-9]+_FILES|EXCLUDE_PATTERNS)$' || true); do
  unset "$v" 2>/dev/null || true
done

emit() {
  local rc="$1" vars v
  if [ "$rc" -ne 0 ]; then
    # Covers BOTH a syntax error and a runner that exited non-zero. Neither is trustworthy, even
    # when the arrays it left behind look complete.
    echo "__SOURCE_FAILED__:$rc"
    return
  fi
  if [ "$MODE" = conflict ]; then
    vars="$(compgen -A variable | grep -E '^GROUP_[A-Za-z0-9]+_FILES$' | sort || true)"
    if [ -z "$vars" ]; then
      echo "__NO_GROUP_ARRAYS__"
      return
    fi
    for v in $vars; do
      eval 'arr=( ${'"$v"'[@]+"${'"$v"'[@]}"} )'
      if [ "${#arr[@]}" -eq 0 ]; then
        echo "__EMPTY_ARRAY__:$v"
        continue
      fi
      printf '%s\n' "${arr[@]}"
    done
  else
    if ! declare -p EXCLUDE_PATTERNS >/dev/null 2>&1; then
      echo "__NO_EXCLUDE_ARRAY__"
      return
    fi
    arr=( ${EXCLUDE_PATTERNS[@]+"${EXCLUDE_PATTERNS[@]}"} )
    if [ "${#arr[@]}" -eq 0 ]; then
      echo "__EMPTY_ARRAY__:EXCLUDE_PATTERNS"
      return
    fi
    printf '%s\n' "${arr[@]}"
  fi
}

# Emit on fd 3, a dup of the ORIGINAL stdout. Not cosmetic: a runner ending in `exit 0` — which is
# legitimate — makes the EXIT trap fire while `source`'s own `>/dev/null` is still the active
# stdout. MEASURED: the emission was swallowed and the parent saw "no output at all", i.e. a false
# alarm on a perfectly good script. fd 3 is immune to that redirection.
#
# The trap is also what makes `exit 0` work at all: it terminates THIS shell, so no post-source
# `compgen` would ever run. A non-zero status here means the runner failed under its own `set -e`
# or exited non-zero; parse errors are already ruled out by the caller's `bash -n`.
exec 3>&1
trap 'emit "$?" >&3; exit 0' EXIT
# shellcheck disable=SC1090
source "$RUNNER" >/dev/null 2>"$ERRFILE"
SRC_RC=$?
exit "$SRC_RC"
EXTRACT

observe() { # <runner> <mode>; writes the invocation log to $WORK_DIR/invocations-<mode>.log
  : > "$WORK_DIR/invocations-$2.log"
  : > "$WORK_DIR/stderr-$2.txt"
  BB_STUB_LOG="$WORK_DIR/invocations-$2.log" PATH="$STUB_DIR:$PATH" \
    bash "$WORK_DIR/extract.sh" "$1" "$2" "$WORK_DIR/stderr-$2.txt"
}

# ── Syntax check BEFORE sourcing anything ──────────────────────────────────────────────────────
# `bash -n` parses the WHOLE file and executes nothing, which is the only reliable way to catch
# this. Sourcing is not: MEASURED on bash 3.2 —
#
#   * a syntax error in a sourced file does NOT abort a shell without `set -e`; `source` returns
#     non-zero and execution CONTINUES, so a runner that assigns every array and only THEN hits a
#     syntax error hands back fully populated, untrustworthy arrays (the old `|| true` here turned
#     that into rc=0 and the guard passed);
#   * with `set -e` active — and BOTH runners set it — the shell does exit, but `$?` inside the
#     EXIT trap is the status of the last SUCCESSFUL command (observed: 0), so the trap cannot
#     tell a fatal parse error from a clean finish either.
#
# Hence: parse first, loudly, and only then observe.
for s in "$CONFLICT_SCRIPT" "$REGULAR_SCRIPT"; do
  if ! bash -n "$s" 2>"$WORK_DIR/parse-error.txt"; then
    echo "::error::$s does not PARSE (bash -n failed), so its path arrays cannot be read at all. This guard is deliberately refusing to pass; fix the script."
    echo "--- bash -n output ---"
    head -20 "$WORK_DIR/parse-error.txt"
    exit 1
  fi
done

# The extractor exits 0 on every outcome it understands, so a non-zero here means the child itself
# could not run. Fail loudly: a silent non-zero is indistinguishable from a crashed guard.
CONFLICT_RAW="$(observe "$CONFLICT_SCRIPT" conflict)" || {
  echo "::error::the extractor child failed to run against $CONFLICT_SCRIPT (exit $?). This guard is deliberately refusing to pass."
  exit 1
}
REGULAR_RAW="$(observe "$REGULAR_SCRIPT" regular)" || {
  echo "::error::the extractor child failed to run against $REGULAR_SCRIPT (exit $?). This guard is deliberately refusing to pass."
  exit 1
}

# ── Fail closed on anything that would make the comparison vacuous ─────────────────────────────
check_extraction() { # <label> <runner-path> <raw-output> <mode>
  local label="$1" script="$2" raw="$3" mode="$4" rc

  if [ -z "$raw" ]; then
    echo "::error::observing $script produced NO output at all. The extractor emits from an EXIT trap, so this means the trap was replaced (does the runner install its own EXIT trap?) or bash died outright. Nothing was verified; fix this rather than deleting the guard."
    exit 1
  fi

  case "$raw" in
    *__SOURCE_FAILED__*)
      rc="$(printf '%s\n' "$raw" | sed -n 's/.*__SOURCE_FAILED__:\([0-9]*\).*/\1/p' | head -1)"
      echo "::error::sourcing $script FAILED (exit ${rc:-?}) — a syntax error, or the script exited non-zero. Its $label cannot be trusted: bash keeps executing after a syntax error in a sourced file, so any arrays assigned BEFORE the error look perfectly populated. This guard is refusing to compare them."
      if [ -s "$WORK_DIR/stderr-$mode.txt" ]; then
        echo "--- stderr from sourcing $script ---"
        head -20 "$WORK_DIR/stderr-$mode.txt"
      fi
      exit 1 ;;
    *__NO_GROUP_ARRAYS__*)
      echo "::error::no GROUP_<x>_FILES arrays found in $script, although it sourced cleanly. Either the arrays were renamed or they are no longer defined at file scope; this guard would otherwise compare an empty list and pass. Fix the guard, do not delete it."
      exit 1 ;;
    *__NO_EXCLUDE_ARRAY__*)
      echo "::error::EXCLUDE_PATTERNS is not defined in $script, although it sourced cleanly. Either it was renamed or it is no longer defined at file scope; this guard would otherwise compare an empty list and pass. Fix the guard, do not delete it."
      exit 1 ;;
    *__EMPTY_ARRAY__*)
      echo "::error::an expected path array is empty: $(printf '%s\n' "$raw" | grep '__EMPTY_ARRAY__' | tr '\n' ' ')"
      exit 1 ;;
  esac

  # A stub is consulted only for a BARE command name. If `bun` never reached the stub, either the
  # runner invoked it by absolute path — in which case sourcing REALLY RAN the suite and this
  # guard's "nothing is executed" promise is void — or it no longer invokes bun at file scope.
  if ! grep -q '^bun|' "$WORK_DIR/invocations-$mode.log" 2>/dev/null; then
    echo "::error::sourcing $script never invoked the stubbed \`bun\`. Commands observed: $(tr '\n' ' ' < "$WORK_DIR/invocations-$mode.log" 2>/dev/null || echo none). Either bun is now called by an absolute path / node_modules/.bin path (which BYPASSES the stub, so sourcing really executes the test suite — fix the runner), or the invocation moved behind a main-guard (in which case relax this assertion deliberately, with a note)."
    exit 1
  fi
}

check_extraction "path arrays"      "$CONFLICT_SCRIPT" "$CONFLICT_RAW" conflict
check_extraction "EXCLUDE_PATTERNS" "$REGULAR_SCRIPT"  "$REGULAR_RAW"  regular

# Paths are normalised to be relative to apps/web: test-conflict-files.sh spells them
# "$FRONTEND_DIR/src/...", test-regular.sh spells them "src/...".
normalise() {
  sed -e "s|^$FRONTEND_DIR/||" -e 's|^\./||'
}

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
