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
# WHAT IS ENFORCED, and what is only reported. `tsconfig.test.json` really does check all ~387 test
# files (see the file-count floor below, which exists because the FIRST version of that config was
# itself vacuous — `extends` inherits `exclude`, `include` does not override it, and it checked
# zero test files while exiting 0). Turning the whole thing green is a ~2,100-error project in
# shared mock infrastructure (`src/test/mocks/**`), out of scope here and deliberately NOT
# suppressed: the count is printed every run so it cannot quietly grow into the background.
#
# The HARD failure is narrower and is the class this closes: no stale `@ts-expect-error` (TS2578).
# That check is exact, cheap, and cannot be satisfied by a neighbouring file.

set -uo pipefail
cd "$(dirname "$0")/.."

TSC=./node_modules/.bin/tsc
OUT="$(mktemp)"
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

"$TSC" -p tsconfig.test.json --noEmit > "$OUT" 2>&1
TSC_STATUS=$?
TOTAL=$(grep -cE 'error TS[0-9]+' "$OUT" || true)

# ⚠ THE GATE MUST NOT PASS WHEN tsc NEVER RAN. Measured: this run's exit status cannot be
# checked naively, because tsc exits 1 BOTH for "type errors found" — the normal case here,
# ~2,100 of them — AND for "could not run at all" (bad config, OOM, a missing binary). The
# two are indistinguishable by exit code alone, which is why the first version of this script
# did not check it.
#
# The discriminator is the REPORT, not the status: a run that fails non-zero while emitting
# ZERO `error TS` lines type-checked nothing. Without this, a tsc that resolved the file list
# and then died printed "0 stale @ts-expect-error directives" and exited 0 — proven by
# substituting a stub that emits 400 --listFiles paths and then exits 7, which turned the
# whole gate green. The file-count floor above does NOT cover this: it is a separate tsc
# invocation, so a binary that lists files fine and crashes mid-check sails past it.
#
# Residual, stated rather than hidden: a crash AFTER some errors are emitted still looks like
# a normal error run. Closing that needs a completion sentinel tsc does not provide.
if [ "$TSC_STATUS" -ne 0 ] && [ "$TOTAL" -eq 0 ]; then
  echo "typecheck-tests: tsc exited $TSC_STATUS but reported no type errors at all." >&2
  echo "  That means it did not type-check — a crash, an OOM, or an unreadable config —" >&2
  echo "  NOT that the test files are clean. Refusing to report a pass. Output was:" >&2
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
echo "typecheck-tests: $TOTAL other type error(s) remain in test files (not gated — see the header"
echo "                 of this script; they live in src/test/mocks/** and are tracked, not hidden)."
