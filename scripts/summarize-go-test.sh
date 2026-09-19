#!/usr/bin/env bash
# summarize-go-test.sh — #1569 round 14 task 3.
#
# THE PROBLEM this closes: 80 tests were silently t.Skip()-ing in the standard local
# integration run (30 of them Redis-gated), and "go test ./..." read as a clean `ok` the
# whole time — nobody could tell a skip from a pass by looking at the output. One of those
# 30 skipped tests was hiding a real regression for two rounds, and "the cache test passes
# locally" was cited as evidence when the test had never actually run. Absence of a failure
# is not a pass, but `go test`'s default (non -v) output cannot distinguish the two: a
# package with 30 skipped tests and zero executed assertions prints the identical `ok
# <pkg> 0.01s` a package with 30 passing tests prints.
#
# THE FIX: run the given `go test` invocation with `-v` forced on, capture its output, and
# print a PASS/FAIL/SKIP tally (plus every skipped test's name) after the real output — so a
# skip count can never again hide inside a green run. This is the cheap half of the fix and
# addresses the root cause directly: the problem was never that tests skipped, it was that
# nobody could tell.
#
# Usage: scripts/summarize-go-test.sh <go test args...>
#   e.g. scripts/summarize-go-test.sh go test -tags integration -p 1 -count=1 ./internal/...
# `-v` is added automatically if not already present in the arguments (needed to see
# individual --- PASS/FAIL/SKIP lines; without it this script has nothing to count).
#
# THE SECOND PROBLEM this closes (G22, #1652): the tally above counts only `^--- FAIL:` lines,
# but a Go BUILD failure emits a PACKAGE-level `FAIL <pkg> [build failed]` line and never a
# `--- FAIL:` line. So a package that does not compile printed `PASS=0 FAIL=0 SKIP=0` — a
# summary that reads green — while the command exited non-zero. The counter that would have
# caught it, PKG_COUNT, was computed and never printed. The summary now prints PKG= (packages
# seen) and PKG_FAIL= (package-level FAIL lines), and a PKG_FAIL>0 run prints a loud warning
# naming the failing packages, so a build failure can never again read as a clean tally.
#
# Exit code: the underlying `go test` invocation's real exit code — this script only adds a
# summary, it never changes pass/fail.
set -uo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: scripts/summarize-go-test.sh <go test args...>" >&2
  exit 2
fi

ARGS=("$@")
HAS_V=0
for a in "${ARGS[@]:-}"; do
  if [ "$a" = "-v" ]; then
    HAS_V=1
  fi
done

if [ "$HAS_V" -eq 0 ]; then
  # Insert -v right after the leading "go test" (or "test", if the caller already omitted
  # "go") so it lands as a flag to `go test`, not a package path or a flag value.
  NEWARGS=()
  INSERTED=0
  for a in "${ARGS[@]}"; do
    NEWARGS+=("$a")
    if [ "$INSERTED" -eq 0 ] && { [ "$a" = "test" ] || [ "$a" = "go" ]; }; then
      if [ "$a" = "test" ]; then
        NEWARGS+=("-v")
        INSERTED=1
      fi
    fi
  done
  if [ "$INSERTED" -eq 1 ]; then
    ARGS=("${NEWARGS[@]}")
  else
    # Caller's command didn't look like "go test ..." (e.g. it was just "test ...args" with
    # "go" implicit, or something this heuristic doesn't recognize) — append -v rather than
    # silently running without it.
    ARGS+=("-v")
  fi
fi

OUTPUT_FILE=$(mktemp)
trap 'rm -f "$OUTPUT_FILE"' EXIT

"${ARGS[@]}" 2>&1 | tee "$OUTPUT_FILE"
GO_TEST_EXIT="${PIPESTATUS[0]}"

PASS_COUNT=$(grep -c '^--- PASS:' "$OUTPUT_FILE" || true)
FAIL_COUNT=$(grep -c '^--- FAIL:' "$OUTPUT_FILE" || true)
SKIP_COUNT=$(grep -c '^--- SKIP:' "$OUTPUT_FILE" || true)
PKG_COUNT=$(grep -cE '^(ok|FAIL|---)\s+github\.com/' "$OUTPUT_FILE" || true)
PKG_FAIL_COUNT=$(grep -cE '^FAIL\s+github\.com/' "$OUTPUT_FILE" || true)

echo ""
echo "========================================"
echo "go test summary: PASS=$PASS_COUNT FAIL=$FAIL_COUNT SKIP=$SKIP_COUNT PKG=$PKG_COUNT PKG_FAIL=$PKG_FAIL_COUNT"
echo "========================================"

if [ "$PKG_FAIL_COUNT" -gt 0 ]; then
  echo ""
  echo "⚠️  $PKG_FAIL_COUNT package(s) FAILED at the package level (e.g. a build failure)."
  echo "    These emit 'FAIL <pkg> [build failed]' and NEVER a '--- FAIL:' line, so"
  echo "    FAIL=$FAIL_COUNT above does NOT mean green. Failing packages:"
  grep -E '^FAIL\s+github\.com/' "$OUTPUT_FILE" | sed -E 's/^FAIL[[:space:]]+([^[:space:]]+).*/  - \1/'
fi

if [ "$SKIP_COUNT" -gt 0 ]; then
  echo ""
  echo "⚠️  $SKIP_COUNT test(s) SKIPPED — a skip is not a pass. Skipped test names:"
  grep '^--- SKIP:' "$OUTPUT_FILE" | sed -E 's/^--- SKIP: ([A-Za-z0-9_/]+).*/  - \1/'
  echo ""
  echo "If any of the above should have run (e.g. a Redis/DB/credential env var was expected"
  echo "to be set), see CLAUDE.md's 'Integration tests' section and"
  echo "scripts/check-ci-env-drift.sh before trusting this as a clean run."
fi

exit "$GO_TEST_EXIT"
