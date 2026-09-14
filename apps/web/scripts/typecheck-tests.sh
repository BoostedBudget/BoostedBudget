#!/usr/bin/env bash
# Type-check the TEST files (#1591 follow-up: "the directives that were never evaluated").
#
# WHY THIS EXISTS. apps/web/tsconfig.json excludes `**/*.test.ts`, `**/*.test.tsx` and `**/test/**`,
# so `bun run typecheck` never looked at a single test file. Two things followed, and the second is
# the reason this is a gate rather than a note:
#
#   1. 24 `@ts-expect-error` directives sat in test files, unevaluated. 17 of them were STALE — the
#      error they claimed to suppress no longer existed — and nothing said so. A stale
#      `@ts-expect-error` is worse than none: it is a standing licence for the next real error on
#      that line to pass silently.
#   2. A type-level assertion written INSIDE a test file proves nothing at all. That is not
#      hypothetical: the #1569 copy round's first draft of a type-level pin was written in a test,
#      passed, and was proving nothing; it had to be moved into a source file to become real.
#
# WHAT IS ENFORCED. `tsconfig.test.json` really does check all ~390 test files (see the file-count
# floor below, which exists because the FIRST version of that config was itself vacuous — `extends`
# inherits `exclude`, `include` does not override it, and it checked zero test files while exiting
# 0). Turning the whole thing green is a ~2,100-error project, out of scope here and deliberately
# NOT suppressed.
#
# ⚠ DO NOT REPEAT THE LOCATION CLAIM THIS HEADER USED TO MAKE. It said those errors "live in
# `src/test/mocks/**`". Measured: 15 of 2,093 (0.7%) are in `src/test/mocks/`. They span 175 files
# across every feature — `features/admin/components` 381, `features/dashboard/widgets` 152,
# `features/billing/components` 91 — with `admin/components/DeadLetterPage.test.tsx` (91) and
# `test/pii-coverage.test.tsx` (70) the worst single files, and TS2322/TS2345/TS2740 (1,732 of
# 2,093) the dominant codes. The CAUSE is shared mock infrastructure — a mock factory returning a
# loosely-typed object gets assigned to a precise API type at every call site — but the LOCATION is
# everywhere, and anyone triaging from the old message opened the wrong directory. The run now
# prints the real top offenders from its own output rather than restating a constant, so this
# cannot go stale again.
#
# There are three HARD failures:
#   * no stale `@ts-expect-error` (TS2578) — exact, cheap, cannot be satisfied by a neighbouring file;
#   * the error total must not exceed the recorded baseline (the RATCHET, see below);
#   * anything that means tsc did not actually finish.
#
# ⚠ THE RATCHET EXISTS BECAUSE A PRINTED NUMBER IS NOT A GATE. Before it, TOTAL was computed and
# printed and nothing was required to look at it: a genuinely wrong new test — one asserting a field
# an API response no longer has — simply became error 2,094 and CI stayed green. Constructed and
# confirmed: adding such a file moved the count 2,093 -> 2,097 and the gate still exited 0.
# The baseline lives in scripts/typecheck-tests-baseline.txt and moves in ONE direction on its own:
# an increase fails, and so does a decrease — because a baseline left above the true count is
# exactly the slack a later regression slips through. Fixing errors is a one-line follow-up:
# `bash scripts/typecheck-tests.sh --update-baseline`.
#
# ⚠ SEPARATELY, this gate must fail closed whenever tsc did not actually finish. Every way it
# could pass without running — an unchecked mktemp, a missing compiler, a crash mid-check — is
# constructed and pinned in scripts/__tests__/typecheck-tests-gate.test.sh, which drives a COPY of
# this script against a stub compiler. Reasoning about those paths is not enough: two of the three
# survived a hardening pass that reasoned about them.

set -uo pipefail
cd "$(dirname "$0")/.." || { echo "typecheck-tests: cannot cd to apps/web" >&2; exit 1; }

die() { echo "typecheck-tests: $1" >&2; exit 1; }

# ── Everything this gate depends on, checked explicitly before anything is measured ──
#
# ⚠ A GATE MUST NEVER PASS BECAUSE ITS OWN TOOLING FAILED. Codex ran `bun run typecheck` in a
# read-only environment where `mktemp` failed; `OUT="$(mktemp)"` was unchecked, every later
# redirection into "$OUT" wrote to the empty string, and the script exited 0 having type-checked
# nothing at all. The same is true of a missing `grep` or a missing compiler: each would leave a
# variable empty and a comparison trivially satisfied. So each is asserted, by name, up front.
TSC=./node_modules/.bin/tsc
if [ ! -x "$TSC" ]; then
  TSC=../../node_modules/.bin/tsc
fi
[ -x "$TSC" ] || die "compiler not found or not executable in the workspace or repository root — run 'bun install'. Refusing to report a pass without a compiler."
for required in mktemp grep sed wc; do
  command -v "$required" > /dev/null 2>&1 || die "required command '$required' is not available; this gate cannot measure anything without it."
done

UPDATE_BASELINE=0
for arg in "$@"; do
  case "$arg" in
    --update-baseline) UPDATE_BASELINE=1 ;;
    *) die "unknown argument '$arg' (only --update-baseline is understood)." ;;
  esac
done

# ⚠ The baseline is part of this gate's tooling, so it is asserted with the same suspicion as the
# compiler: a missing, unreadable or non-numeric baseline must FAIL, never default to "no ceiling".
# A gate whose ceiling silently evaporates is the defect this was written to close.
BASELINE_FILE=scripts/typecheck-tests-baseline.txt
BASELINE=''
if [ "$UPDATE_BASELINE" -eq 0 ]; then
  [ -r "$BASELINE_FILE" ] || die "baseline file '$BASELINE_FILE' is missing or unreadable. It is the ceiling on test-file type errors; without it this gate has none. Restore it, or regenerate it with: bash scripts/typecheck-tests.sh --update-baseline"
  BASELINE=$(sed -e 's/#.*$//' -e '/^[[:space:]]*$/d' "$BASELINE_FILE" | tr -d '[:space:]')
  case "$BASELINE" in
    '' | *[!0-9]*) die "baseline file '$BASELINE_FILE' does not contain a plain integer (read: '$BASELINE'). Refusing to guess a ceiling." ;;
  esac
fi

OUT="$(mktemp)" || die "mktemp failed — no scratch file, so nothing can be measured. Refusing to report a pass."
[ -n "$OUT" ] && [ -f "$OUT" ] && [ -w "$OUT" ] || die "mktemp produced an unusable path ('$OUT'). Refusing to report a pass."
trap 'rm -f "$OUT"' EXIT

# Fail closed: if the config stops seeing test files, this gate would report success while
# checking nothing — the exact failure it exists to prevent, applied to itself.
CHECKED=$("$TSC" -p tsconfig.test.json --noEmit --listFiles 2>/dev/null | grep -cE '\.test\.tsx?$' || true)
if [ "$CHECKED" -lt 300 ]; then
  echo "typecheck-tests: tsconfig.test.json resolved only $CHECKED test file(s); 387 were present" >&2
  echo "  when this gate was written, so the config is broken and passing means nothing." >&2
  echo "  ⚠ Most likely cause: \`exclude\` was dropped. \`extends\` INHERITS the base tsconfig's" >&2
  echo "  \`exclude\`, and overriding \`include\` does not override it — tsconfig.test.json must" >&2
  echo "  keep its own explicit \"exclude\": []." >&2
  exit 1
fi

# ⚠ --diagnostics IS THE COMPLETION SENTINEL, and it is the whole reason this run is not a plain
# `tsc --noEmit`. Measured: tsc prints NO "Found N errors" summary in this mode, so there is
# otherwise nothing in its output that distinguishes "finished, and here are the errors" from
# "died partway through, and here are the errors it had got to". --diagnostics appends a timing
# trailer ("Files: ... / Total time: 1.75s") that is emitted only after the check completes.
#
# It does not change what is parsed below: measured before and after, the real run reports the
# same file count, 0 stale directives and the same 2,093 errors with it on.
"$TSC" -p tsconfig.test.json --noEmit --diagnostics > "$OUT" 2>&1
TSC_STATUS=$?
TOTAL=$(grep -cE 'error TS[0-9]+' "$OUT" || true)
COMPLETED=$(grep -cE '^Total time:[[:space:]]+[0-9]' "$OUT" || true)

# ⚠ THE GATE MUST NOT PASS WHEN tsc NEVER FINISHED. tsc exits 1 BOTH for "type errors found" —
# the normal case here, 2,093 of them — AND for "could not run at all" (bad config, OOM, a
# missing binary). The two are indistinguishable by exit code alone, which is why the first
# version of this script did not check it, and why the second version's discriminator ("non-zero
# AND zero errors reported") was still not enough: Codex simulated status=7 with a single
# `error TS9999: compiler aborted after emitting an error` line, and that version accepted it.
# One emitted error is not evidence of a completed check. The trailer is.
if [ "$COMPLETED" -eq 0 ]; then
  echo "typecheck-tests: tsc exited $TSC_STATUS without printing its --diagnostics trailer." >&2
  echo "  The trailer is emitted only after the check completes, so its absence means the" >&2
  echo "  compiler died partway through — a crash, an OOM, an unreadable config — NOT that the" >&2
  echo "  test files are clean. Refusing to report a pass. Output was:" >&2
  sed 's/^/    /' "$OUT" >&2
  exit 1
fi

# Belt and braces beside the sentinel. tsc's documented exit statuses are 0 (clean), 1 and 2
# (diagnostics present). Anything else — a signal, a node-level abort, a shim gone wrong — is not
# a compiler result this gate knows how to read, no matter how complete the output looked.
case "$TSC_STATUS" in
  0|1|2) ;;
  *)
    echo "typecheck-tests: tsc exited $TSC_STATUS, which is not one of its result statuses (0/1/2)." >&2
    echo "  Refusing to report a pass on output this gate cannot vouch for. Output was:" >&2
    sed 's/^/    /' "$OUT" >&2
    exit 1
    ;;
esac

# A completed run that reported nothing at all, yet exited non-zero, is still incoherent.
if [ "$TSC_STATUS" -ne 0 ] && [ "$TOTAL" -eq 0 ]; then
  echo "typecheck-tests: tsc exited $TSC_STATUS but reported no type errors at all." >&2
  echo "  Refusing to report a pass. Output was:" >&2
  sed 's/^/    /' "$OUT" >&2
  exit 1
fi

STALE=$(grep -E 'error TS2578' "$OUT" || true)
if [ -n "$STALE" ]; then
  COUNT=$(printf '%s\n' "$STALE" | wc -l | tr -d ' ')
  echo "typecheck-tests: $COUNT stale '@ts-expect-error' directive(s) in test files." >&2
  echo "  The error each one claims to suppress no longer exists, so the directive now suppresses" >&2
  echo "  whatever real error lands on that line next. Delete it, or fix the line it guards:" >&2
  printf '%s\n' "$STALE" | sed 's/^/    /' >&2
  exit 1
fi

echo "typecheck-tests: $CHECKED test files checked, 0 stale @ts-expect-error directives."
# ⚠ #1569 R6B/C4: TOTAL is `error TS[0-9]+` lines from tsc's WHOLE tsconfig.test.json compilation
# (test files plus everything they import), not a count scoped to test files — only CHECKED above
# is test-file-specific. Wording corrected here; the number and the gate it feeds are UNCHANGED.
if [ "$UPDATE_BASELINE" -eq 1 ]; then
  echo "typecheck-tests: $TOTAL type error(s) across tsconfig.test.json's compilation (recording as the new baseline)."
else
  echo "typecheck-tests: $TOTAL type error(s) across tsconfig.test.json's compilation (baseline $BASELINE)."
fi

# Where the errors actually are, derived from THIS run. Printed instead of asserted in a comment,
# because the constant this replaced ("src/test/mocks/**") was 0.7% accurate and nobody noticed.
# ⚠ Every pipeline here ends in `head`, which closes the pipe and SIGPIPEs its upstream. Under
# `set -o pipefail` that is status 141, and as the script's LAST command it became the script's
# exit status — a gate failing 141 on a perfectly healthy run. Caught by case 1 of the guard suite,
# not by reading the code. Hence the trailing `return 0`, and the explicit `exit 0` at the end.
report_hotspots() {
  local pairs
  pairs=$(grep -oE '^[^(]+\([0-9]+,[0-9]+\): error TS[0-9]+' "$OUT" | sed 's/(.*error /|/')
  [ -n "$pairs" ] || return 0
  echo "  files with errors: $(printf '%s\n' "$pairs" | cut -d'|' -f1 | sort -u | wc -l | tr -d ' ')"
  echo "  worst directories:"
  printf '%s\n' "$pairs" | cut -d'|' -f1 | sed 's#/[^/]*$##' | sort | uniq -c | sort -rn | head -5 | sed 's/^/    /'
  echo "  worst files:"
  printf '%s\n' "$pairs" | cut -d'|' -f1 | sort | uniq -c | sort -rn | head -5 | sed 's/^/    /'
  return 0
}

if [ "$UPDATE_BASELINE" -eq 1 ]; then
  # Preserve the file's explanatory header — it is the only place the ratchet's contract is
  # written down for whoever trips it next. Only the bare integer line is rewritten.
  if [ -r "$BASELINE_FILE" ] && grep -q '^#' "$BASELINE_FILE"; then
    { grep '^#' "$BASELINE_FILE"; echo; printf '%s\n' "$TOTAL"; } > "$BASELINE_FILE.tmp" \
      && mv "$BASELINE_FILE.tmp" "$BASELINE_FILE" || die "could not write '$BASELINE_FILE'."
  else
    printf '%s\n' "$TOTAL" > "$BASELINE_FILE" || die "could not write '$BASELINE_FILE'."
  fi
  echo "typecheck-tests: baseline updated to $TOTAL. Commit $BASELINE_FILE."
  exit 0
fi

if [ "$TOTAL" -gt "$BASELINE" ]; then
  echo "typecheck-tests: $((TOTAL - BASELINE)) NEW type error(s) across tsconfig.test.json's compilation ($TOTAL > baseline $BASELINE)." >&2
  echo "  A type error here is usually a test that is now WRONG — asserting a field or a" >&2
  echo "  shape the code no longer has — and such a test can still pass at runtime. Fix it." >&2
  echo "  Raising the baseline is a deliberate, reviewable act, not a way past this message." >&2
  report_hotspots >&2
  exit 1
fi

if [ "$TOTAL" -lt "$BASELINE" ]; then
  echo "typecheck-tests: $((BASELINE - TOTAL)) error(s) fixed — the baseline is now stale ($TOTAL < $BASELINE)." >&2
  echo "  Lower it, or the slack you just created is where the next regression hides:" >&2
  echo "    bash apps/web/scripts/typecheck-tests.sh --update-baseline" >&2
  exit 1
fi

report_hotspots
exit 0
