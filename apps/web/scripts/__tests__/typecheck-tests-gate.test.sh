#!/usr/bin/env bash
# Regression suite for apps/web/scripts/typecheck-tests.sh (#1569, Codex finding 5).
# Run: bash apps/web/scripts/__tests__/typecheck-tests-gate.test.sh
#
# WHY THIS EXISTS. typecheck-tests.sh is a GATE, and the thing a gate must never do is report a
# pass when it did not run. It had three ways to do exactly that, and two of them survived a first
# hardening pass because they were reasoned about rather than constructed:
#
#   1. tsc exits 1 BOTH for "type errors found" (the normal case here — 2,093 of them) and for
#      "could not run at all". The first version did not check the status at all.
#   2. Checking "non-zero AND zero errors reported" closed only the crash-before-any-error case.
#      A crash AFTER emitting one error still looked like a normal error run. That was documented
#      as a residual; "documented" is not "closed".
#   3. `mktemp` failure was unchecked, so in a read-only environment the script exited 0 having
#      type-checked nothing.
#
# A FOURTH, added later and just as much a fail-open: the error TOTAL was computed, printed, and
# gated on by nothing. A genuinely wrong new test simply became error 2,094 and CI stayed green
# (constructed against the real compiler: the count moved 2,093 -> 2,097 and the gate exited 0).
# Cases 9-13 pin the ratchet that closed it, INCLUDING the direction that is easy to leave open —
# a baseline sitting above the true count is slack, so a decrease must fail too — and the case
# that matters most for a gate: a missing baseline file must not mean "no ceiling".
#
# HOW IT IS TESTED. Every case below runs a COPY of the real script inside a sandbox whose
# `node_modules/.bin/tsc` is a stub. That is what makes the failure modes constructible: a real tsc
# cannot be asked to crash at a chosen moment. The stub reproduces the two shapes tsc actually
# produces — a `--listFiles` listing, and a diagnostics run — and its exit status and output are
# chosen per case. Nothing here touches the real compiler or the real tsconfig.
#
# ⚠ Case "healthy" is not decoration: a gate that fails closed on everything would pass cases 2-5
# while breaking the build. It pins that the ordinary 2,093-error run still exits 0.
set -uo pipefail

REPO_WEB="$(cd "$(dirname "$0")/../.." && pwd)"
REAL_SCRIPT="$REPO_WEB/scripts/typecheck-tests.sh"

PASS=0
FAIL=0
pass() { echo "  ✅ PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  ❌ FAIL: $1"; FAIL=$((FAIL + 1)); }

SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT
SANDBOX_WEB="$SANDBOX/apps/web"

# Builds a sandbox that looks enough like apps/web for the script's `cd "$(dirname "$0")/.."` and
# its relative ./node_modules/.bin/tsc to resolve.
build_sandbox() {
  local mode="$1" listcount="${2:-400}"
  rm -rf "$SANDBOX_WEB"
  mkdir -p "$SANDBOX_WEB/scripts" "$SANDBOX_WEB/node_modules/.bin"
  cp "$REAL_SCRIPT" "$SANDBOX_WEB/scripts/typecheck-tests.sh"
  echo '{}' > "$SANDBOX_WEB/tsconfig.test.json"
  # The ratchet's ceiling. Defaults to the stub's own error count so the "healthy" case is a
  # no-change run; individual cases below rewrite it to construct an increase or a decrease.
  printf '# sandbox baseline\n%s\n' "${3:-2000}" > "$SANDBOX_WEB/scripts/typecheck-tests-baseline.txt"
  cat > "$SANDBOX_WEB/node_modules/.bin/tsc" <<STUB
#!/usr/bin/env bash
# Stub tsc. MODE decides what the diagnostics run does; the --listFiles run is always healthy
# unless LISTCOUNT is lowered, because the file-count floor is a separate concern with its own case.
MODE="$mode"
LISTCOUNT="$listcount"
for arg in "\$@"; do
  if [ "\$arg" = "--listFiles" ]; then
    for i in \$(seq 1 "\$LISTCOUNT"); do echo "/sandbox/src/f\$i.test.ts"; done
    exit 1   # real tsc exits 1 here too: the listing run also reports the type errors
  fi
done
emit_errors() {
  local n="\$1" i
  for i in \$(seq 1 "\$n"); do echo "src/f\$i.test.ts(1,1): error TS2322: Type 'a' is not assignable to type 'b'."; done
}
sentinel() {
  echo "Files:              400"
  echo "Total time:       1.75s"
}
case "\$MODE" in
  healthy)            emit_errors 2000; sentinel; exit 1 ;;
  stale)              emit_errors 3; echo "src/f1.test.ts(9,3): error TS2578: Unused '@ts-expect-error' directive."; sentinel; exit 1 ;;
  crash_after_error)  echo "src/f1.test.ts(1,1): error TS9999: compiler aborted after emitting an error"; exit 7 ;;
  crash_no_error)     exit 7 ;;
  odd_status)         emit_errors 2000; sentinel; exit 7 ;;
  clean)              sentinel; exit 0 ;;
esac
STUB
  chmod +x "$SANDBOX_WEB/node_modules/.bin/tsc"
}

run_gate() {
  set +e
  GATE_OUTPUT=$(cd "$SANDBOX_WEB" && bash scripts/typecheck-tests.sh 2>&1)
  GATE_STATUS=$?
  set -e
}

echo "=== typecheck-tests.sh gate suite ==="
echo ""

# ── Case 1: the ordinary run (thousands of errors, no stale directives) still passes ──
echo "Case 1: a normal 2,000-error run exits 0"
build_sandbox healthy
run_gate
if [ "$GATE_STATUS" -ne 0 ]; then
  fail "the normal error-laden run was rejected (exit $GATE_STATUS) — output: $GATE_OUTPUT"
elif ! echo "$GATE_OUTPUT" | grep -qE "2000 type error\(s\).*baseline 2000"; then
  # Load-bearing facts are the NUMBER and the BASELINE, not the prose between them — that prose is
  # incidental (it was corrected once already, from "in test files" to "across tsconfig.test.json's
  # compilation", and stayed true both times). Asserting the exact sentence here re-breaks on every
  # future honest wording fix; asserting the two numbers survives them.
  fail "passed but did not report the error count and baseline — output: $GATE_OUTPUT"
else
  pass "a normal run still exits 0 and reports its error count"
fi

# ── Case 2: a crash AFTER emitting one error must NOT pass (Codex finding 5.1) ──
# This is the exact simulation Codex ran: status=7 with a single `error TS` line. The old
# predicate ("non-zero AND zero errors") accepted it, because one error had been emitted.
echo "Case 2: a crash after emitting one error is rejected"
build_sandbox crash_after_error
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a compiler that aborted after one error was reported as a PASS — output: $GATE_OUTPUT"
else
  pass "a crash after emitting an error fails closed (exit $GATE_STATUS)"
fi

# ── Case 3: a crash before emitting anything must NOT pass ──
echo "Case 3: a crash before emitting anything is rejected"
build_sandbox crash_no_error
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a compiler that emitted nothing was reported as a PASS — output: $GATE_OUTPUT"
else
  pass "a crash with no output fails closed (exit $GATE_STATUS)"
fi

# ── Case 4: mktemp failure must NOT pass (Codex finding 5.2) ──
# Codex ran `bun run typecheck` in a read-only environment where mktemp failed and the script
# still exited 0. Constructed here with a failing mktemp earlier on PATH than the real one.
echo "Case 4: an unusable mktemp is rejected"
build_sandbox healthy
mkdir -p "$SANDBOX/badbin"
printf '#!/usr/bin/env bash\nexit 1\n' > "$SANDBOX/badbin/mktemp"
chmod +x "$SANDBOX/badbin/mktemp"
set +e
GATE_OUTPUT=$(cd "$SANDBOX_WEB" && PATH="$SANDBOX/badbin:$PATH" bash scripts/typecheck-tests.sh 2>&1)
GATE_STATUS=$?
set -e
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a failed mktemp still produced a PASS — output: $GATE_OUTPUT"
else
  pass "a failed mktemp fails closed (exit $GATE_STATUS)"
fi

# ── Case 5: a missing tsc binary must NOT pass ──
echo "Case 5: a missing tsc binary is rejected"
build_sandbox healthy
rm -f "$SANDBOX_WEB/node_modules/.bin/tsc"
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a missing compiler still produced a PASS — output: $GATE_OUTPUT"
else
  pass "a missing tsc fails closed (exit $GATE_STATUS)"
fi

# ── Case 6: the file-count floor still bites ──
echo "Case 6: a config resolving too few test files is rejected"
build_sandbox healthy 12
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a config resolving 12 test files produced a PASS — output: $GATE_OUTPUT"
elif ! echo "$GATE_OUTPUT" | grep -q "resolved only 12"; then
  fail "rejected, but not by the file-count floor — output: $GATE_OUTPUT"
else
  pass "the file-count floor rejects a near-empty config"
fi

# ── Case 7: the hard check (stale @ts-expect-error) still fires ──
# Guards against a hardening pass that fails closed so eagerly it never reaches its own purpose.
echo "Case 7: a stale @ts-expect-error is still the hard failure"
build_sandbox stale
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a stale directive passed — output: $GATE_OUTPUT"
elif ! echo "$GATE_OUTPUT" | grep -q "stale '@ts-expect-error'"; then
  fail "rejected, but not for the stale directive — output: $GATE_OUTPUT"
else
  pass "TS2578 is still reported as the stale-directive failure"
fi

# ── Case 8: an exit status outside tsc's documented set is rejected ──
# Belt and braces beside the completion sentinel: even a run that printed a full diagnostics
# trailer is not trusted if it then exited with a status tsc never uses.
echo "Case 8: an undocumented exit status is rejected even with a complete trailer"
build_sandbox odd_status
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "exit status 7 was accepted because the trailer looked complete — output: $GATE_OUTPUT"
else
  pass "an undocumented exit status fails closed (exit $GATE_STATUS)"
fi

# ── Case 9: MORE errors than the baseline is rejected (the ratchet's whole point) ──
# This is the defect in constructed form: a wrong new test becomes error N+1. Before the ratchet
# the gate printed the higher number and exited 0.
echo "Case 9: an error count above the baseline is rejected"
build_sandbox healthy 400 1999
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "2000 errors against a baseline of 1999 was reported as a PASS — output: $GATE_OUTPUT"
elif ! echo "$GATE_OUTPUT" | grep -q "1 NEW type error"; then
  fail "rejected, but not by the ratchet — output: $GATE_OUTPUT"
else
  pass "an increase over the baseline fails (exit $GATE_STATUS)"
fi

# ── Case 10: FEWER errors than the baseline is also rejected ──
# A baseline left above the true count is exactly the slack the next regression hides in.
echo "Case 10: an error count below the baseline is rejected, with the fix instruction"
build_sandbox healthy 400 2500
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a stale (too-high) baseline was accepted — output: $GATE_OUTPUT"
elif ! echo "$GATE_OUTPUT" | grep -q -- "--update-baseline"; then
  fail "rejected, but without telling anyone how to lower it — output: $GATE_OUTPUT"
else
  pass "a decrease fails and names the fix (exit $GATE_STATUS)"
fi

# ── Case 11: a MISSING baseline must not mean "no ceiling" ──
echo "Case 11: a missing baseline file is rejected"
build_sandbox healthy
rm -f "$SANDBOX_WEB/scripts/typecheck-tests-baseline.txt"
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a missing baseline produced a PASS — the ceiling silently evaporated — output: $GATE_OUTPUT"
else
  pass "a missing baseline fails closed (exit $GATE_STATUS)"
fi

# ── Case 12: a non-numeric baseline must not be guessed at ──
echo "Case 12: a non-numeric baseline is rejected"
build_sandbox healthy
printf '# nothing but a comment\n' > "$SANDBOX_WEB/scripts/typecheck-tests-baseline.txt"
run_gate
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "a baseline with no number produced a PASS — output: $GATE_OUTPUT"
else
  pass "a non-numeric baseline fails closed (exit $GATE_STATUS)"
fi

# ── Case 13: --update-baseline records the count and KEEPS the explanatory header ──
# The header is the only place the ratchet's contract is written down for whoever trips it next;
# a writer that clobbers it turns the next failure into a bare number with no instructions.
echo "Case 13: --update-baseline rewrites the number and preserves the header"
build_sandbox healthy 400 1
BL="$SANDBOX_WEB/scripts/typecheck-tests-baseline.txt"
printf '# CONTRACT LINE\n1\n' > "$BL"
set +e
GATE_OUTPUT=$(cd "$SANDBOX_WEB" && bash scripts/typecheck-tests.sh --update-baseline 2>&1)
GATE_STATUS=$?
set -e
if [ "$GATE_STATUS" -ne 0 ]; then
  fail "--update-baseline exited $GATE_STATUS — output: $GATE_OUTPUT"
elif ! grep -q '^2000$' "$BL"; then
  fail "--update-baseline did not record the count; file is: $(cat "$BL")"
elif ! grep -q 'CONTRACT LINE' "$BL"; then
  fail "--update-baseline clobbered the explanatory header; file is: $(cat "$BL")"
else
  pass "--update-baseline records 2000 and keeps the header"
fi

# ── Case 14: after --update-baseline the very next run passes (the ratchet is escapable ONLY here) ──
echo "Case 14: the run after --update-baseline passes"
run_gate
if [ "$GATE_STATUS" -ne 0 ]; then
  fail "the run after --update-baseline still failed — output: $GATE_OUTPUT"
else
  pass "the updated baseline is honoured on the next run"
fi

# ── Case 15: an unknown argument is rejected rather than silently ignored ──
# A typo'd flag that is ignored is how "--update-baseline" would become a no-op nobody notices.
echo "Case 15: an unknown argument is rejected"
build_sandbox healthy
set +e
GATE_OUTPUT=$(cd "$SANDBOX_WEB" && bash scripts/typecheck-tests.sh --updatebaseline 2>&1)
GATE_STATUS=$?
set -e
if [ "$GATE_STATUS" -eq 0 ]; then
  fail "an unknown flag was silently ignored — output: $GATE_OUTPUT"
else
  pass "an unknown argument fails closed (exit $GATE_STATUS)"
fi

# ── Case 16: Bun's hoisted linker places the compiler at the repository root ──
echo "Case 16: a repository-root compiler supports Bun's hoisted linker"
build_sandbox healthy
mkdir -p "$SANDBOX/node_modules/.bin"
mv "$SANDBOX_WEB/node_modules/.bin/tsc" "$SANDBOX/node_modules/.bin/tsc"
run_gate
if [ "$GATE_STATUS" -ne 0 ]; then
  fail "a healthy root compiler was rejected (exit $GATE_STATUS) — output: $GATE_OUTPUT"
else
  pass "the hoisted root compiler is used when no workspace-local compiler exists"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ]
