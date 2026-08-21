#!/usr/bin/env bash
# Guard: the frontend test runners must pass an EXPLICIT --timeout, because bunfig.toml's
# `[test] timeout` does not work.
#
# WHY THIS EXISTS (#1537). apps/web/bunfig.toml has declared `[test] timeout = 30000` since it was
# written, and it was not taking effect. Two independent pieces of evidence:
#
#   - locally (bun 1.4.0): a test that sleeps 6s fails at exactly 5005ms — bun's built-in default —
#     under bunfig alone, and passes when `--timeout 30000` is given on the command line;
#   - in CI (bun 1.3.14, pinned at .github/workflows/test.yml:1535): the Frontend Tests job reported
#     "timed out after 5000ms", again the default, not the declared 30000.
#
# ⚠ An earlier version of this comment blamed "bun 1.4.0 ignores that key". Codex review showed that
# is overbroad: bun 1.3.14's own docs DO document `[test] timeout`, and 1.3.14 is what CI pins — yet
# CI still hit the 5s default. So the honest statement is that the key was not in effect in EITHER
# environment; why it is not (version behaviour, config discovery, a preload interaction) is
# unestablished. The CLI flag is the fix either way, and this guard checks the effect rather than the
# cause. Worth recording that I proved a local fact on 1.4.0 and generalised it to the repo without
# checking which bun CI runs.
#
# The consequence was not theoretical. `scripts/test-conflict-files.sh` already passed the flag on
# both of its invocations, so someone had found this before; it simply was never applied to
# `scripts/test-regular.sh`, the 362-file suite that does the bulk of the work. That suite therefore
# ran at 5s while its small sibling got 30s, and CI failed on #1537 when CategoryEditor's "should
# close creation form and dropdown after successful creation" — 441ms locally — took 9910ms on a
# loaded runner.
#
# This guard checks BOTH the flag's presence AND, in test 3, that the effective timeout really does
# exceed bun's default. The second part is what makes it more than a grep: if a future bun release
# starts honouring bunfig (or stops honouring the flag), test 3 is what notices.
#
# Test 3 covers EVERY bun invocation in BOTH runner scripts, not just test-regular.sh. package.json's
# `test`/`test:ci` scripts run both, and a value floor on one of two is half a floor - especially for
# a guard whose whole premise is that these two siblings drifted apart.
#
# The floor is 15000ms, not "anything above bun's 5000ms default" (Codex review, #1537 round 6).
# A `--timeout 6000` would clear both a >5000 check and the 5.6s probe, yet the CI failure that
# started all of this took 9910ms - so the guard would have passed a configuration that still breaks
# the build. The floor must cover the observed duration with headroom.
#
# Two or more `--timeout` flags in one command is a FAILURE, not something the guard resolves for
# you. bun honours the last one, so `--timeout 30000 --timeout 1000` really runs at 1000ms while
# reading as 30000ms to a human - and to the guard, before round 8.
#
# Still NOT asserted: the exact value 30000. Pinning it would fail a legitimate tuning change.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/apps/web"

pass=0
fail=0

ok() {
    echo "  PASS: $1"
    pass=$((pass + 1))
}
bad() {
    echo "  FAIL: $1"
    fail=$((fail + 1))
}

echo "=== check-frontend-test-timeout.test.sh ==="
echo

echo "Test 1: every 'bun test' invocation in apps/web/scripts passes an explicit --timeout"
missing=""
while IFS= read -r line; do
    file="${line%%:*}"
    rest="${line#*:}"
    lineno="${rest%%:*}"
    # Read the invocation plus the next 3 lines, since the flag may sit on a continuation.
    if ! sed -n "${lineno},$((lineno + 3))p" "$file" | grep -q -- "--timeout"; then
        missing+="    $file:$lineno"$'\n'
    fi
done < <(grep -rn "bun test" "$WEB_DIR/scripts" --include="*.sh" 2>/dev/null || true)

if [ -z "$missing" ]; then
    ok "all 'bun test' invocations carry --timeout"
else
    bad "these 'bun test' invocations have no --timeout, so they run at bun's 5s default:"
    printf '%s' "$missing"
fi

echo "Test 2: the scan is not vacuous — it found invocations to check"
found=$(grep -rc "bun test" "$WEB_DIR/scripts" --include="*.sh" 2>/dev/null | awk -F: '{s+=$2} END {print s+0}')
if [ "${found:-0}" -ge 2 ]; then
    ok "found $found 'bun test' invocation(s) to inspect"
else
    bad "found only ${found:-0} 'bun test' invocations — the scan is broken, and test 1 would pass vacuously"
fi

echo "Test 3: EVERY bun invocation in BOTH runner scripts sets an unambiguous timeout above the floor"
# Rebuilt (#1537 round 6, extended round 8). The first version wrote its probe into apps/web/src and
# invoked scripts/test-regular.sh, which was wrong three ways, all mine:
#   1. no cleanup trap - an interrupted run left the 5.6s probe in src/, where test-regular.sh's
#      `find src` collects it into EVERY later run;
#   2. it ran all 362 files to check one property, because test-regular.sh parses args only for
#      --coverage and always rebuilds its list from `find src` - the path argument was ignored;
#   3. its exit code covered 6400 tests, so any unrelated failure made this guard claim the timeout
#      was broken. A guard that cries wolf gets worked around.
# Now: read the value(s) each runner actually passes, assert every one is above the floor, and prove
# the smallest of them works on a probe ALONE, outside src/, with cleanup on any trappable signal
# (SIGKILL excepted - see the trap).
#
# Read the value from the INVOCATION, not from anywhere in the file. The first version grepped the
# whole script and matched the explanatory comment above the command - so with the flag deleted from
# `bun test` it still reported "the runner passes --timeout 30000". Caught by the axis-1 mutation.
# awk extracts each `bun test` continuation block (a run of lines ending in backslash) and stops at
# the first line that does not, so only real command text is searched.
# Comments are stripped FIRST, then the command is matched - order matters and an earlier version of
# this comment described it backwards. Codex found that: the match ran before the strip, so a comment
# line merely mentioning `bun test --timeout 30000` could start the block scan at the wrong place.
# The match also tolerates indentation and an `env` prefix, which `^bun test` did not.
#
# THREE defects fixed in round 8, each demonstrated before it was fixed:
#   - It read only test-regular.sh. Test 1 checks flag PRESENCE across all of apps/web/scripts, but
#     the VALUE was checked on one hardcoded file, so test-conflict-files.sh could drop to
#     `--timeout 100` with the whole guard green. package.json's `test` script runs BOTH, so both
#     matter - and "the two sibling scripts drifted apart" is this guard's entire premise.
#   - It took the FIRST `--timeout` (`head -1`) while bun honours the LAST. Demonstrated: with
#     `--timeout 30000 --timeout 1000` the guard reported 30000 and passed, while the same command
#     line kills a 5.6s test at 1003ms - an effective 1000ms, five times worse than the default this
#     guard exists to escape. Rather than silently taking the last, MORE THAN ONE `--timeout` in a
#     single command block is now itself a failure: ambiguous configuration is the defect, and a
#     maintainer should be told rather than have the guard quietly resolve it.
#   - The regex demanded whitespace, so `--timeout=30000` - which bun accepts, verified - parsed to
#     empty and the guard reported "passes no --timeout ... runs at bun's 5s default", which was
#     simply false. `[[:space:]=]` now accepts both forms.
#
# LIMIT, stated rather than left to be discovered: `sub(/#.*/, "")` is a comment stripper, not a shell
# parser. A `${VAR#pattern}` expansion inside a command block would be mangled. None exists in either
# script today, and mangling can only ever REMOVE a match (it cannot fabricate a `--timeout N`), so
# the failure direction is a false alarm, never a false pass.
TIMEOUT_FLOOR=15000
blocks=$(awk '
    { line = $0; sub(/#.*/, "", line) }
    inblock {
        buf = buf " " line
        if (line !~ /\\[[:space:]]*$/) { print start "\t" buf; inblock = 0 }
        next
    }
    line ~ /(^|[[:space:]])bun[[:space:]]+test([[:space:]]|$)/ {
        start = FILENAME ":" FNR
        buf = line
        if (line ~ /\\[[:space:]]*$/) { inblock = 1 } else { print start "\t" buf }
        next
    }
' "$WEB_DIR/scripts/test-regular.sh" "$WEB_DIR/scripts/test-conflict-files.sh")

timeout_problems=""
blocks_seen=0
min_timeout=""
while IFS= read -r block; do
    [ -n "$block" ] || continue
    blocks_seen=$((blocks_seen + 1))
    loc="${block%%$'\t'*}"
    cmd="${block#*$'\t'}"
    flag_count=$(printf '%s\n' "$cmd" | grep -oE -- "--timeout" | wc -l | tr -d ' ')
    if [ "$flag_count" -eq 0 ]; then
        timeout_problems+="    $loc: no --timeout at all, so this invocation runs at bun's 5s default"$'\n'
        continue
    fi
    if [ "$flag_count" -gt 1 ]; then
        # Do NOT silently resolve this to the last value. bun's precedence is real but invisible to a
        # reader, and the observed failure mode was a large value masking a tiny one.
        timeout_problems+="    $loc: $flag_count --timeout flags in one command. bun honours the LAST, so the intent is ambiguous and a large leading value can mask a tiny trailing one. Pass exactly one."$'\n'
        continue
    fi
    value=$(printf '%s\n' "$cmd" | grep -oE -- "--timeout[[:space:]=]+[0-9]+" | grep -oE "[0-9]+$")
    if [ -z "${value:-}" ]; then
        timeout_problems+="    $loc: --timeout is present but its value is not a literal number this guard can check (a variable or expansion?). Use a literal so the floor is verifiable."$'\n'
        continue
    fi
    if [ "$value" -lt "$TIMEOUT_FLOOR" ]; then
        timeout_problems+="    $loc: --timeout $value. That may clear bun's 5000ms default but not the observed slow case: the CI failure this guard exists for took 9910ms. Use at least $TIMEOUT_FLOOR."$'\n'
        continue
    fi
    if [ -z "${min_timeout:-}" ] || [ "$value" -lt "$min_timeout" ]; then
        min_timeout="$value"
    fi
done <<< "$blocks"

if [ "$blocks_seen" -lt 2 ]; then
    bad "found only $blocks_seen bun invocation block(s) across both runner scripts - the block scan is broken, so this test would pass having checked nothing"
elif [ -n "$timeout_problems" ]; then
    bad "bun invocations with an unusable timeout:"
    printf '%s' "$timeout_problems"
else
    ok "all $blocks_seen bun invocation(s) in both runner scripts pass exactly one --timeout, each >= $TIMEOUT_FLOOR (smallest: $min_timeout, covering the observed 9910ms case)"

    if ! command -v bun >/dev/null 2>&1; then
        # Not a SKIP. The static check alone can pass while the flag has no effect, which is the
        # entire failure this guard exists to catch - and a SKIP left `fail` at zero, so the suite
        # reported success having verified nothing (Codex review).
        bad "bun is not on PATH, so the EFFECT of --timeout was never verified. This repo requires bun; a green run here would be meaningless."
    else
        # Trap FIRST, then create: installing it afterwards leaves a window where a signal
        # arriving between the two lines orphans the directory (Codex review). The guard is
        # "no residue on any signal bash can handle" - SIGKILL is still unhandleable, so this is
        # not literally guaranteed, and the earlier comment claiming it was is corrected here.
        tmp=""
        # EXIT cleans up; INT/TERM clean up AND exit non-zero. Cleaning up without exiting was a real
        # defect (Codex): bash resumes after a trap handler, so a Ctrl-C mid-probe would tidy the temp
        # dir and then fall through to the success check below and report a pass.
        cleanup_probe() { if [ -n "${tmp:-}" ]; then rm -rf "$tmp"; fi; }
        trap cleanup_probe EXIT
        trap 'cleanup_probe; exit 130' INT
        trap 'cleanup_probe; exit 143' TERM
        tmp=$(mktemp -d)
        cat > "$tmp/effective_timeout_probe.test.ts" <<'PROBE'
import { test, expect } from 'bun:test'

// Sleeps just past bun's 5000ms built-in default. Passes only if a larger timeout is in effect.
test('effective timeout probe', async () => {
  await new Promise((r) => setTimeout(r, 5600))
  expect(true).toBe(true)
})
PROBE
        # Probed with the SMALLEST value any invocation uses: if the weakest link has real effect,
        # every other invocation does too.
        if (cd "$WEB_DIR" && bun test --timeout "$min_timeout" "$tmp/effective_timeout_probe.test.ts" >/dev/null 2>&1); then
            ok "a 5.6s test survives --timeout $min_timeout, so the value has real effect"
        else
            bad "a 5.6s test timed out under --timeout $min_timeout - the flag is not taking effect"
        fi
    fi
fi

echo
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
