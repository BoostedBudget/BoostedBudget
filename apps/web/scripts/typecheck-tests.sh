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
[ -x "$TSC" ] || die "compiler not found or not executable at $TSC — run 'bun install'. Refusing to report a pass without a compiler."
for required in mktemp grep sed wc; do
  command -v "$required" > /dev/null 2>&1 || die "required command '$required' is not available; this gate cannot measure anything without it."
done

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
echo "typecheck-tests: $TOTAL other type error(s) remain in test files (not gated — see the header"
echo "                 of this script; they live in src/test/mocks/** and are tracked, not hidden)."
