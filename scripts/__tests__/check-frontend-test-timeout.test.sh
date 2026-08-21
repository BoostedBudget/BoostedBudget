#!/usr/bin/env bash
# Guard: the frontend test runners must pass an EXPLICIT --timeout, because bunfig.toml's
# `[test] timeout` does not work.
#
# WHY THIS EXISTS (#1537). apps/web/bunfig.toml has declared `[test] timeout = 30000` since it was
# written, and it was not taking effect. Two independent pieces of evidence:
#
#   - locally (bun 1.4.0): a test that sleeps 6s fails at exactly 5005ms — bun's built-in default —
#     under bunfig alone, and passes when `--timeout 30000` is given on the command line;
#   - in CI (bun 1.3.14, pinned at .github/workflows/test.yml): the Frontend Tests job reported
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
# CONTRACT, as of round 9:
#   - every `bun test` invocation in apps/web/scripts/**.sh passes at least one --timeout (test 1);
#   - the scan is non-vacuous AND its two independent counters agree (test 2);
#   - every invocation passes EXACTLY one --timeout whose literal value is >= 15000 (test 3);
#   - the smallest such value demonstrably survives a 5.6s probe (test 3);
#   - a missing `bun` binary is a hard FAIL, never a skip;
#   - no scanned runner delegates its test run to a `bun run test...` package.json script, whose
#     flags this guard cannot see (test 4).
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
#
# KNOWN SCOPE LIMIT (round 9, D-3): apps/web/package.json's `test:all`, `test:watch` and
# `test:integration` scripts each run `bun test` directly, outside apps/web/scripts, so they are NOT
# covered here and run at bun's 5s default. No CI job and no Makefile target invokes any of the
# three (`make test-fast`/`test-all`/CI all go through `bun run test`/`test:ci`, which shell out to
# the two scanned runners), so they cannot break the build today. Extending the scan to package.json
# means parsing JSON out of a shell guard for paths nothing runs; the deliberate choice is to leave
# them out and say so here rather than to widen the guard silently. What IS closed is the route by
# which they could become load-bearing: test 4 fails if a scanned runner delegates to one of them.

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

# ---------------------------------------------------------------------------------------------
# ONE source of truth for WHICH files are scanned (round 9, D-2). Before this, test 1 globbed
# apps/web/scripts while test 3 read two HARDCODED paths - so a third runner script
# (apps/web/scripts/test-extra.sh with `--timeout 100`) satisfied test 1's presence check and was
# never value-checked at all. Demonstrated: the whole guard stayed green at 4 passed / 0 failed.
# Every test below reads SCAN_FILES; adding a runner script cannot escape the value check.
# ---------------------------------------------------------------------------------------------
SCAN_FILES=()
while IFS= read -r f; do
    [ -n "$f" ] && SCAN_FILES+=("$f")
done < <(find "$WEB_DIR/scripts" -type f -name '*.sh' 2>/dev/null | sort)

if [ "${#SCAN_FILES[@]}" -eq 0 ]; then
    bad "no *.sh files found under $WEB_DIR/scripts - the file discovery is broken, so every test below would pass having read nothing"
    echo
    echo "=== Results: $pass passed, $fail failed ==="
    exit 1
fi

# ---------------------------------------------------------------------------------------------
# ONE source of truth for HOW a command is extracted.
#
# awk walks each scanned file and emits:
#   BLOCK  <file:line> <joined command text>   - one record per `bun test` invocation, where an
#                                                invocation is the matching line plus every
#                                                following line while the previous one ends in `\`;
#   COUNTS <bun-test-lines> <--timeout-hits>   - the same two quantities counted per LINE, with no
#                                                block structure involved at all.
#
# The COUNTS record exists to be compared against what the BLOCK loop actually inspected (test 2).
# The diagnostic that motivated it: test 2 once printed "found 4 'bun test' invocation(s)" while
# test 3 printed "all 3 bun invocation(s)" - the guard's own two counters disagreed and nothing
# compared them, so a whole unscanned script was reported as inspected.
#
# COMMENT STRIPPING (round 9, D-1 - a demonstrated FALSE PASS). This used to be
# `sub(/#.*/, "", line)`, which is where the worst hole in nine rounds lived: stripping from `#` to
# end of line ALSO strips a trailing line-continuation backslash, which ends the block scan early
# and hides every flag on the following lines. With
#
#     bun test \
#         --timeout 30000 \
#         --test-name-pattern 'regression #1537' \
#         --timeout 100 \
#         $REGULAR_TESTS
#
# the guard reported 4 passed / 0 failed, while that exact command line on bun 1.4.0 kills a 5.6s
# test at 100.60ms - an effective 100ms timeout, 50x worse than the default this guard exists to
# escape. The idiomatic `$( [ ${#ARR[@]} -gt 0 ] && ... )` form was mangled the same way.
#
# ⚠ The old note here claimed mangling "can only ever REMOVE a match ... so the failure direction is
# a false alarm, never a false pass." That reasoning was valid under the pre-round-8 take-the-last-
# value semantics. It became WRONG the moment more-than-one-flag was made a failure: removing a
# match then turns a FAIL into a PASS. A stale justification is exactly why this hole survived four
# review rounds - so the current direction is stated plainly: with >1 flag being a failure, ANY lost
# text can flip FAIL->PASS, and that is why the stripper now tracks quoting instead of guessing.
#
# strip_comment() therefore walks the line character by character and cuts only at a `#` that is
# (a) unquoted and (b) at the start of a word - which is bash's own rule for what begins a comment.
# `'regression #1537'` and `${#ARR[@]}` both survive intact, so the continuation backslash survives
# with them. When a cut DOES happen, no backslash is re-appended, which is also bash's behaviour: a
# `\` inside a comment is not a line continuation and the command really does end there.
#
# Remaining limits, stated rather than left to be discovered:
#   - quote state is tracked per line, so a string opened on one line and closed on the next is
#     mis-scanned. Neither runner does that;
#   - `;#` and `|#` (a `#` beginning a word after an operator rather than after whitespace) are NOT
#     treated as comment starts, so such a comment is scanned as command text. That direction is a
#     false ALARM (a stray `--timeout` in prose could fail the guard), never a false pass;
#   - the stripper is shared by both the BLOCK and COUNTS paths, so a bug in it is not caught by
#     their cross-check. It is caught by the mutation suite, which is where it belongs: the
#     cross-check exists to falsify the BLOCK structure logic, not the stripper.
# ---------------------------------------------------------------------------------------------
TIMEOUT_FLOOR=15000
BUN_TEST_RE='(^|[ \t])bun[ \t]+test([ \t]|$)'

awk_out=$(awk '
function strip_comment(s,   i, n, c, q, out) {
    n = length(s); q = ""; out = ""
    for (i = 1; i <= n; i++) {
        c = substr(s, i, 1)
        if (q == "") {
            if (c == "#" && (i == 1 || substr(s, i - 1, 1) ~ /[ \t]/)) return out
            if (c == "\\") { out = out c; i++; if (i <= n) out = out substr(s, i, 1); continue }
            if (c == "'"'"'" || c == "\"") { q = c; out = out c; continue }
        } else if (c == q) {
            q = ""; out = out c; continue
        } else if (q == "\"" && c == "\\") {
            out = out c; i++; if (i <= n) out = out substr(s, i, 1); continue
        }
        out = out c
    }
    return out
}
{
    line = strip_comment($0)

    # --- per-line counters, deliberately blind to block structure -------------------------
    if (line ~ RE) bun_lines++
    tmp = line
    to_hits += gsub(/--timeout/, "&", tmp)

    # --- indirect invocation ----------------------------------------------------------------
    # `bun run test:integration` runs `bun test scripts/integration/` from package.json at the 5s
    # default, and carries no --timeout of its own for this guard to see. Round 9 demonstrated the
    # false pass: swapping the whole bun block in test-regular.sh for `bun run test:integration`
    # left the guard at 4 passed / 0 failed. NOTE: no apostrophes in this awk program - it is a
    # single-quoted shell string.
    if (line ~ /(^|[ \t])(bun|bunx|npm|pnpm|yarn)[ \t]+run[ \t]+test/) {
        print "DELEGATE\t" FILENAME ":" FNR "\t" line
    }

    # --- block scan ------------------------------------------------------------------------
    if (inblock) {
        buf = buf " " line
        if (line !~ /\\[ \t]*$/) { print "BLOCK\t" start "\t" buf; inblock = 0 }
        next
    }
    if (line ~ RE) {
        start = FILENAME ":" FNR
        buf = line
        if (line ~ /\\[ \t]*$/) { inblock = 1 } else { print "BLOCK\t" start "\t" buf }
    }
}
END {
    # A file whose last line is `bun test \` leaves an unterminated block. Emit it rather than
    # drop it - dropping it would understate blocks_seen and read as a clean scan.
    if (inblock) print "BLOCK\t" start "\t" buf
    print "COUNTS\t" bun_lines + 0 "\t" to_hits + 0
}
' RE="$BUN_TEST_RE" "${SCAN_FILES[@]}")

missing=""
timeout_problems=""
delegated=""
blocks_seen=0
flag_total=0
min_timeout=""
line_bun_count=0
line_timeout_count=0

while IFS= read -r rec; do
    case "$rec" in
        COUNTS*)
            rest="${rec#COUNTS$'\t'}"
            line_bun_count="${rest%%$'\t'*}"
            line_timeout_count="${rest##*$'\t'}"
            continue
            ;;
        DELEGATE*)
            rest="${rec#DELEGATE$'\t'}"
            delegated+="    ${rest%%$'\t'*}: ${rest#*$'\t'}"$'\n'
            continue
            ;;
        BLOCK*) ;;
        *) continue ;;
    esac

    rest="${rec#BLOCK$'\t'}"
    loc="${rest%%$'\t'*}"
    cmd="${rest#*$'\t'}"
    blocks_seen=$((blocks_seen + 1))

    flag_count=$(printf '%s\n' "$cmd" | grep -oE -- "--timeout" | wc -l | tr -d ' ')
    flag_total=$((flag_total + flag_count))

    if [ "$flag_count" -eq 0 ]; then
        missing+="    $loc: no --timeout, so this invocation runs at bun's 5s default"$'\n'
        timeout_problems+="    $loc: no --timeout at all (see test 1)"$'\n'
        continue
    fi
    if [ "$flag_count" -gt 1 ]; then
        # Do NOT silently resolve this to the last value. bun's precedence is real but invisible to a
        # reader, and the observed failure mode was a large value masking a tiny one.
        timeout_problems+="    $loc: $flag_count --timeout flags in one command. bun honours the LAST, so the intent is ambiguous and a large leading value can mask a tiny trailing one. Pass exactly one."$'\n'
        continue
    fi
    # `[[:space:]=]` accepts both `--timeout 30000` and `--timeout=30000`; bun accepts both, and an
    # earlier regex demanding whitespace reported the `=` form as passing no flag at all (round 8).
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
done <<< "$awk_out"

echo "Test 1: every 'bun test' invocation in apps/web/scripts passes an explicit --timeout"
# Read from the extracted command blocks, not from a fixed sed window around the match. The old
# `sed -n "${lineno},$((lineno + 3))p"` window was wrong in both directions: a flag on the 5th
# continuation line read as missing, and a flag belonging to the NEXT command read as present.
if [ -z "$missing" ]; then
    ok "all 'bun test' invocations carry --timeout"
else
    bad "these 'bun test' invocations have no --timeout, so they run at bun's 5s default:"
    printf '%s' "$missing"
fi

echo "Test 2: the scan is not vacuous, and its two independent counters agree"
if [ "$line_bun_count" -lt 2 ]; then
    bad "found only $line_bun_count 'bun test' invocation(s) across ${#SCAN_FILES[@]} script(s) - the scan is broken, and tests 1 and 3 would pass vacuously"
elif [ "$blocks_seen" -ne "$line_bun_count" ]; then
    bad "counter divergence: the per-line scan sees $line_bun_count 'bun test' invocation(s) but the block scan inspected $blocks_seen. One invocation is being swallowed or double-counted, so something is running unchecked."
elif [ "$flag_total" -ne "$line_timeout_count" ]; then
    # This is the array/variable passthrough hole (round 9, D-2). `GROUP_A_PRELOAD_ARGS=(--timeout
    # 100)` reaches bun as "$@" and really does win over the literal `--timeout 30000` on the
    # command line, but it appears in no `bun test` block, so the value check never sees it.
    # Demonstrated: 4 passed / 0 failed with an effective 100ms timeout in test-conflict-files.sh.
    bad "counter divergence: $line_timeout_count '--timeout' occurrence(s) in command text, but only $flag_total inside a 'bun test' invocation. $((line_timeout_count - flag_total)) flag(s) sit somewhere this guard cannot value-check - typically an array or variable that reaches bun as \"\$@\", where a small value silently overrides the literal one. Pass --timeout literally on the bun command line."
else
    ok "found $line_bun_count 'bun test' invocation(s) across ${#SCAN_FILES[@]} script(s); block scan inspected $blocks_seen and accounted for all $line_timeout_count '--timeout' occurrence(s)"
fi

echo "Test 3: EVERY bun invocation in EVERY scanned runner sets an unambiguous timeout above the floor"
# Rebuilt (#1537 round 6, extended rounds 8 and 9). The first version wrote its probe into
# apps/web/src and invoked scripts/test-regular.sh, which was wrong three ways, all mine:
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
#
# Round 8 fixed three further defects, each demonstrated first: the value was checked on ONE
# hardcoded file; the FIRST flag was taken while bun honours the LAST; and `--timeout=30000` parsed
# to empty. Round 9 fixed the comment stripper eating continuations (see strip_comment above), the
# hardcoded file list (see SCAN_FILES), and the unaccounted-flag hole (see test 2).
if [ "$blocks_seen" -lt 2 ]; then
    bad "found only $blocks_seen bun invocation block(s) across the scanned runners - the block scan is broken, so this test would pass having checked nothing"
elif [ -n "$timeout_problems" ]; then
    bad "bun invocations with an unusable timeout:"
    printf '%s' "$timeout_problems"
else
    ok "all $blocks_seen bun invocation(s) across ${#SCAN_FILES[@]} scanned script(s) pass exactly one --timeout, each >= $TIMEOUT_FLOOR (smallest: $min_timeout, covering the observed 9910ms case)"

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

echo "Test 4: no scanned runner hands the job to a package.json script this guard cannot see"
# Round 9, defect #11 - a demonstrated FALSE PASS, and the same class as the D-3 scope limit above.
# Tests 1-3 only ever look at literal `bun test` text. A runner that instead calls
# `bun run test:integration` (package.json: "bun test scripts/integration/", no --timeout) runs at
# bun's 5s default with nothing to inspect: with test-regular.sh's bun block replaced by that one
# line, the guard reported 4 passed / 0 failed while the 362-file suite no longer had any timeout at
# all. Rather than resolve package.json scripts from a shell guard, refuse the indirection: run bun
# directly from the runner, where the flag is visible and checkable.
if [ -z "$delegated" ]; then
    ok "no scanned runner delegates its test run to a 'bun run test...' script"
else
    bad "these scanned runners delegate to a package.json test script, whose --timeout this guard cannot see (package.json's test:all/test:watch/test:integration all run bun test at the 5s default). Invoke bun directly with an explicit --timeout instead:"
    printf '%s' "$delegated"
fi

echo
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
