#!/usr/bin/env bash
# Guard: every `bun test` the frontend runners ACTUALLY EXECUTE must pass exactly one explicit
# --timeout of at least 15000ms, because apps/web/bunfig.toml's `[test] timeout` is not in effect.
#
# WHY THIS EXISTS (#1537). apps/web/bunfig.toml has declared `[test] timeout = 30000` since it was
# written, and it was not taking effect. Two independent pieces of evidence:
#
#   - locally (bun 1.4.0): a test that sleeps 6s fails at exactly 5005ms — bun's built-in default —
#     under bunfig alone, and passes when `--timeout 30000` is given on the command line;
#   - in CI (bun 1.3.14, pinned at .github/workflows/test.yml): the Frontend Tests job reported
#     "timed out after 5000ms", again the default, not the declared 30000.
#
# Why it is not in effect (version behaviour, config discovery, a preload interaction) is
# unestablished; the CLI flag is the fix either way, so this guard checks the effect, not the cause.
# The consequence was not theoretical: test-conflict-files.sh passed the flag, test-regular.sh (the
# 362-file suite) did not, and CI failed when CategoryEditor's "should close creation form and
# dropdown after successful creation" — 441ms locally — took 9910ms on a loaded runner.
#
# ═══ HOW THIS GUARD WORKS, AND WHY IT NO LONGER PARSES SHELL ═══════════════════════════════════
#
# READ THIS BEFORE "IMPROVING" IT. Rounds 1-9 of this guard read the runner scripts as TEXT: an awk
# program that stripped comments, tracked quote state, joined `\` continuations and regex-matched
# `--timeout`. That approach accumulated TWELVE defects across six review rounds, three of them
# demonstrated FALSE PASSES — a green guard over a command line that really ran at 100ms:
#
#   - `sub(/#.*/,"",line)` also ate the continuation backslash, ending the block scan early and
#     hiding every flag on the following lines;
#   - the character-walking replacement tracked quote state PER LINE, so a multiline double-quoted
#     argument whose second line began with `#` (e.g. a --test-name-pattern spanning two lines)
#     reset the quote state and let the walker treat real command text as a comment. Verified:
#     `--timeout 30000 ... "harmless\n# --timeout 100" --timeout 100` reported 5 passed / 0 failed
#     while bash honoured the LAST flag, 100ms;
#   - the "independent counter cross-check" did not catch either one, because both counting paths
#     were fed by the same stripper: it cheerfully "accounted for all 3 --timeout occurrences"
#     after having already hidden two of them.
#
# The root cause of all twelve was the approach, not the individual bugs: a shell parser written in
# awk regexes will always be behind bash. So the guard was rewritten to STOP PARSING and START
# OBSERVING:
#
#   1. a recording stub named `bun` (plus bunx/npm/npx/pnpm/yarn) is placed FIRST on PATH. It writes
#      its own argv, NUL-delimited, to a fresh file and exits 0 without running anything;
#   2. each frontend test runner is then really EXECUTED with that PATH;
#   3. the guard reads the argv records. Whatever bash handed to `bun` IS the truth.
#
# What this design KNOWS, for free and forever: comments, quoting of every kind, multiline strings,
# `\` continuations, `${VAR#pattern}`, arrays expanded as "$@", values that arrive from variables,
# helper functions called five times (test-conflict-files.sh's run_group), and indirect invocation —
# `bun run test:integration` shows up as the literal argv `run test:integration`, no package.json
# reading required. None of that needs a single regex.
#
# What this design does NOT know, stated plainly rather than left to be discovered:
#   - only the code paths TAKEN in the modes exercised below (no args, and --coverage) are observed.
#     A branch gated on some other condition (`if [ "$CI" = true ]`) is invisible. Mitigation: every
#     runner must produce at least one observed `bun test` in EVERY mode, or the guard hard-fails, so
#     a runner cannot go quiet without being noticed;
#   - it sees the RUNTIME value of --timeout, so it cannot tell a literal 30000 from a variable that
#     happened to hold 30000 — and does not care, since the runtime value is what bun obeys. The
#     old "is it a literal?" check existed only because a text scan could not resolve variables;
#   - a `bun` invoked by ABSOLUTE path bypasses the PATH stub. That direction fails closed: the run
#     produces zero argv records and the vacuity test fails;
#   - the stubs make every bun/npm/npx/pnpm/yarn call a no-op exit 0. If a runner ever needs one of
#     those to do real work, the run misbehaves LOUDLY (non-zero exit or zero records = FAIL), never
#     silently green.
#
# CONTRACT:
#   - test 1: the runner list is derived from apps/web/package.json (not hardcoded) and is non-empty;
#   - test 2: every runner, in every mode, exits 0 AND actually reached `bun` (vacuity, fails closed);
#   - test 3: every observed `bun test` passes EXACTLY one --timeout (`--timeout N` or `--timeout=N`),
#     an integer >= 15000. Two flags is a FAILURE, not something the guard resolves: bun honours the
#     last, so `--timeout 30000 --timeout 100` reads as 30s to a human and runs at 100ms;
#   - test 4: no observed invocation delegates to a package.json script (`bun run test:*` and
#     friends), whose flags live somewhere this run cannot see;
#   - test 5: the SMALLEST observed value demonstrably survives a 5.6s test under real bun. A missing
#     `bun` binary is a hard FAIL, never a skip;
#   - test 6: no *.sh under apps/web/scripts that is NOT executed here contains a `bun test` at all —
#     that is the only place a text scan survives, and only as a "you added an unverifiable runner"
#     tripwire whose failure direction is a false ALARM, never a false pass.
#
# The floor is 15000, not "above bun's 5000 default": `--timeout 6000` would clear both a >5000 check
# and the 5.6s probe, yet the CI failure that started this took 9910ms. The exact value 30000 is
# deliberately NOT pinned — that would fail a legitimate tuning change.
#
# SCOPE LIMIT: apps/web/package.json's `test:all`, `test:watch` and `test:integration` run `bun test`
# directly, outside apps/web/scripts, and carry no --timeout. Nothing runs them: `make test-fast`,
# `make test-all` and CI all go through `bun run test` / `bun run test:ci` (.github/workflows/test.yml
# "Run frontend tests with coverage"), which shell out to the runners checked here. (The workflow's
# other `bun run test:integration` step is apps/mcp-server's, not apps/web's — verified.) Test 4
# closes the route by which they could become load-bearing.
#
# ⚠ THAT PARENTHETICAL ABOUT mcp-server WAS A DISMISSAL, AND IT COST A RED BUILD (#1537 round 17).
# Having noticed apps/mcp-server, this comment filed it under "not apps/web's problem" instead of
# asking whether it had the same defect. It did: the workflows ran `bun test src/` for it with NO
# --timeout, so that 283-test suite ran at bun's 5s default — and on a loaded host `MCP Server
# Validation` went red with a beforeEach hook timing out at 6665ms in src/spec/index-builder.test.ts.
# The same raw invocation was in deploy-production.yml, so a loaded production deploy could have
# failed identically.
#
# Test 7 now scans every .github/workflows/*.yml for raw `bun test` invocations — the scope this file
# should have had from the start. The defect is "a bun suite running at the default", not "an
# apps/web script missing a flag".

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/apps/web"
TIMEOUT_FLOOR=15000

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

WORK=""
cleanup() { if [ -n "${WORK:-}" ]; then rm -rf "$WORK"; fi; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
WORK=$(mktemp -d)

finish() {
    echo
    echo "=== Results: $pass passed, $fail failed ==="
    [ "$fail" -eq 0 ]
    exit $?
}

# ---------------------------------------------------------------------------------------------
# bun is required for everything below: to parse package.json (test 1) and to prove the observed
# timeout has real effect (test 5). Absence is a hard FAIL, never a skip.
# ---------------------------------------------------------------------------------------------
if ! command -v bun >/dev/null 2>&1; then
    bad "bun is not on PATH. This repo requires bun; without it neither the runner discovery nor the effect probe can run, and a green result would be meaningless."
    finish
fi

# ---------------------------------------------------------------------------------------------
# Test 1: ONE source of truth for WHICH runners are checked.
#
# The list comes from apps/web/package.json's own `test*` script entries (minus the e2e ones,
# which start servers and browsers and cannot be executed from a unit-test guard). That is the
# same list CI runs: `bun run test:ci` -> `bash scripts/test-regular.sh --coverage && bash
# scripts/test-conflict-files.sh --coverage`. Adding a runner to package.json therefore puts it
# under this guard automatically; test 6 catches a runner added WITHOUT going through package.json.
# JSON is parsed by bun, not by a shell regex.
# ---------------------------------------------------------------------------------------------
echo "Test 1: the runner list is discovered from apps/web/package.json, not hardcoded"

RUNNERS=()
while IFS= read -r rel; do
    [ -n "$rel" ] && RUNNERS+=("$rel")
done < <(cd "$WEB_DIR" && bun -e '
const pkg = require("./package.json");
const out = new Set();
for (const [key, val] of Object.entries(pkg.scripts || {})) {
  if (!key.startsWith("test")) continue;
  if (key.includes("e2e")) continue;
  const m = String(val).match(/scripts\/[A-Za-z0-9_.\/-]+\.sh/g) || [];
  for (const p of m) out.add(p);
}
for (const p of [...out].sort()) console.log(p);
' 2>/dev/null)

if [ "${#RUNNERS[@]}" -eq 0 ]; then
    bad "no runner scripts discovered from $WEB_DIR/package.json test* scripts - discovery is broken, so every test below would pass having executed nothing"
    finish
fi

missing_files=""
for rel in "${RUNNERS[@]}"; do
    if [ ! -f "$WEB_DIR/$rel" ]; then
        missing_files+="    $rel (referenced by package.json, not on disk)"$'\n'
    fi
done
if [ -n "$missing_files" ]; then
    bad "package.json references runner scripts that do not exist:"
    printf '%s' "$missing_files"
    finish
fi
ok "discovered ${#RUNNERS[@]} runner script(s) from package.json: ${RUNNERS[*]}"

# ---------------------------------------------------------------------------------------------
# The recording stub. It logs its own argv NUL-delimited (so an argument containing spaces or
# newlines cannot corrupt the record), one file per invocation, then exits 0. Sequence numbers come
# from a counter file: the runners are executed serially, so this is deterministic.
# ---------------------------------------------------------------------------------------------
SHIM_DIR="$WORK/shim"
mkdir -p "$SHIM_DIR"
cat > "$SHIM_DIR/.record" <<'STUB'
#!/usr/bin/env bash
set -u
_name=$(basename "$0")
_seq=$(( $(cat "$BB_ARGV_DIR/.seq" 2>/dev/null || echo 0) + 1 ))
echo "$_seq" > "$BB_ARGV_DIR/.seq"
printf '%s\0' "$_name" "$@" > "$BB_ARGV_DIR/$(printf 'rec.%04d' "$_seq")"
exit 0
STUB
chmod +x "$SHIM_DIR/.record"
for c in bun bunx npm npx pnpm yarn; do
    ln -s "$SHIM_DIR/.record" "$SHIM_DIR/$c"
done

# Modes to exercise. `bun run test` passes no args; `bun run test:ci` passes --coverage. Both are
# real code paths in both runners (test-conflict-files.sh has a separate bun command line per mode).
MODE_ARGS=("" "--coverage")
MODE_NAMES=("no args" "--coverage")

vacuity_problems=""
timeout_problems=""
delegated=""
other_invocations=""
test_invocations=0
min_timeout=""

# check_record <record-file> <label>
check_record() {
    local rec="$1" label="$2"
    local args=() a
    while IFS= read -r -d '' a; do
        args+=("$a")
    done < "$rec"

    local n="${#args[@]}"
    [ "$n" -eq 0 ] && return 0

    local cmd="${args[0]}"
    local sub="" target=""
    [ "$n" -ge 2 ] && sub="${args[1]}"
    [ "$n" -ge 3 ] && target="${args[2]}"

    # Delegation to a package.json script: the flags then live somewhere this run cannot observe.
    if [ "$sub" = "run" ]; then
        case "$target" in
            test*)
                delegated+="    $label: $cmd run $target - the timeout for this run is defined in package.json, where this guard cannot verify it. Invoke bun test directly with an explicit --timeout."$'\n'
                return 0
                ;;
        esac
        other_invocations+="    $label: $cmd run $target (not a test script - ignored)"$'\n'
        return 0
    fi

    if [ "$cmd" != "bun" ] || [ "$sub" != "test" ]; then
        other_invocations+="    $label: $cmd $sub ... (not a 'bun test' - ignored)"$'\n'
        return 0
    fi

    test_invocations=$((test_invocations + 1))

    local i tcount=0 tval="" tval_set=0
    for (( i = 2; i < n; i++ )); do
        case "${args[i]}" in
            --timeout)
                tcount=$((tcount + 1))
                if [ $((i + 1)) -lt "$n" ]; then tval="${args[i + 1]}"; tval_set=1; else tval=""; tval_set=0; fi
                ;;
            --timeout=*)
                tcount=$((tcount + 1))
                tval="${args[i]#--timeout=}"
                tval_set=1
                ;;
        esac
    done

    if [ "$tcount" -eq 0 ]; then
        timeout_problems+="    $label: bun test was executed with NO --timeout, so it runs at bun's 5000ms default (the CI failure this guard exists for took 9910ms)."$'\n'
        return 0
    fi
    if [ "$tcount" -gt 1 ]; then
        timeout_problems+="    $label: $tcount --timeout flags reached bun in one command. bun honours the LAST, so a large leading value can mask a tiny trailing one. Pass exactly one."$'\n'
        return 0
    fi
    if [ "$tval_set" -ne 1 ] || [ -z "$tval" ]; then
        timeout_problems+="    $label: --timeout was passed with no value after it."$'\n'
        return 0
    fi
    case "$tval" in
        ''|*[!0-9]*)
            timeout_problems+="    $label: --timeout '$tval' is not an integer number of milliseconds."$'\n'
            return 0
            ;;
    esac
    if [ "$tval" -lt "$TIMEOUT_FLOOR" ]; then
        timeout_problems+="    $label: --timeout $tval. That may clear bun's 5000ms default but not the observed slow case: the CI failure this guard exists for took 9910ms. Use at least $TIMEOUT_FLOOR."$'\n'
        return 0
    fi
    if [ -z "${min_timeout:-}" ] || [ "$tval" -lt "$min_timeout" ]; then
        min_timeout="$tval"
    fi
}

# ---------------------------------------------------------------------------------------------
# Test 2: really execute each runner in each mode, with the stub first on PATH.
# ---------------------------------------------------------------------------------------------
echo "Test 2: each runner really executes under a recording 'bun', exits 0, and reaches bun at all"

run_index=0
for rel in "${RUNNERS[@]}"; do
    mode_i=0
    while [ "$mode_i" -lt "${#MODE_ARGS[@]}" ]; do
        mode_args="${MODE_ARGS[$mode_i]}"
        mode_name="${MODE_NAMES[$mode_i]}"
        mode_i=$((mode_i + 1))
        run_index=$((run_index + 1))

        run_dir="$WORK/run.$run_index"
        mkdir -p "$run_dir"
        log="$run_dir/output.log"

        if [ -n "$mode_args" ]; then
            BB_ARGV_DIR="$run_dir" PATH="$SHIM_DIR:$PATH" \
                bash "$WEB_DIR/$rel" "$mode_args" >"$log" 2>&1
        else
            BB_ARGV_DIR="$run_dir" PATH="$SHIM_DIR:$PATH" \
                bash "$WEB_DIR/$rel" >"$log" 2>&1
        fi
        rc=$?

        recs=()
        while IFS= read -r r; do
            [ -n "$r" ] && recs+=("$r")
        done < <(find "$run_dir" -maxdepth 1 -type f -name 'rec.*' 2>/dev/null | sort)

        if [ "$rc" -ne 0 ]; then
            vacuity_problems+="    $rel ($mode_name): exited $rc under the recording stub, so its observation cannot be trusted. Last output: $(tail -3 "$log" | tr '\n' ' ')"$'\n'
        fi
        if [ "${#recs[@]}" -eq 0 ]; then
            vacuity_problems+="    $rel ($mode_name): never invoked bun/bunx/npm/npx/pnpm/yarn at all - zero argv records. Either the runner no longer runs bun, or it calls it by absolute path where PATH cannot intercept it. Tests 3-5 would pass having observed nothing."$'\n'
        else
            k=0
            for r in "${recs[@]}"; do
                k=$((k + 1))
                check_record "$r" "$rel ($mode_name) invocation #$k"
            done
        fi
        mode_args=""
    done
done

if [ -n "$vacuity_problems" ]; then
    bad "runner executions that cannot be trusted:"
    printf '%s' "$vacuity_problems"
elif [ "$test_invocations" -lt 2 ]; then
    bad "only $test_invocations 'bun test' invocation(s) observed across $run_index runner execution(s) - both runners are expected to run bun test, so this is a broken observation, not a clean one"
else
    ok "$run_index runner execution(s) all exited 0 and reached bun; $test_invocations 'bun test' invocation(s) observed"
fi

# ---------------------------------------------------------------------------------------------
# Test 3: the actual argv of every observed bun test.
# ---------------------------------------------------------------------------------------------
echo "Test 3: every observed 'bun test' passes exactly one --timeout >= $TIMEOUT_FLOOR"
if [ "$test_invocations" -eq 0 ]; then
    bad "no 'bun test' invocation was observed, so there is nothing to check - treated as a failure, not a pass"
elif [ -n "$timeout_problems" ]; then
    bad "bun test invocations with an unusable timeout:"
    printf '%s' "$timeout_problems"
else
    ok "all $test_invocations observed 'bun test' invocation(s) pass exactly one --timeout >= $TIMEOUT_FLOOR (smallest: $min_timeout, covering the observed 9910ms case)"
fi

# ---------------------------------------------------------------------------------------------
# Test 4: delegation to a package.json script.
# ---------------------------------------------------------------------------------------------
echo "Test 4: no runner hands the job to a package.json script whose flags this run cannot see"
if [ -z "$delegated" ]; then
    ok "no observed invocation delegates to a 'run test...' package.json script"
else
    bad "observed delegations to package.json test scripts (test:all/test:watch/test:integration all run bun test at the 5s default):"
    printf '%s' "$delegated"
fi

# ---------------------------------------------------------------------------------------------
# Test 5: the observed value has real effect under real bun.
# ---------------------------------------------------------------------------------------------
echo "Test 5: the smallest observed --timeout demonstrably survives a 5.6s test under real bun"
if [ -z "${min_timeout:-}" ]; then
    bad "no usable --timeout value was observed, so the EFFECT of the flag was never verified"
else
    probe_dir="$WORK/probe"
    mkdir -p "$probe_dir"
    cat > "$probe_dir/effective_timeout_probe.test.ts" <<'PROBE'
import { test, expect } from 'bun:test'

// Sleeps just past bun's 5000ms built-in default. Passes only if a larger timeout is in effect.
test('effective timeout probe', async () => {
  await new Promise((r) => setTimeout(r, 5600))
  expect(true).toBe(true)
})
PROBE
    if (cd "$WEB_DIR" && bun test --timeout "$min_timeout" "$probe_dir/effective_timeout_probe.test.ts" >/dev/null 2>&1); then
        ok "a 5.6s test survives --timeout $min_timeout, so the value has real effect"
    else
        bad "a 5.6s test timed out under --timeout $min_timeout - the flag is not taking effect"
    fi
fi

# ---------------------------------------------------------------------------------------------
# Test 6: tripwire for a runner that never gets executed here.
#
# This is the one text check left, and it is deliberately the dumbest possible one: a literal
# substring search, no comment stripping, no quoting rules. It cannot produce a false pass — at
# worst it fires on the words "bun test" inside a comment, which is a false ALARM someone fixes by
# rewording or by putting the script in package.json where it can be observed properly.
# ---------------------------------------------------------------------------------------------
echo "Test 7: no workflow runs a raw 'bun test' without a --timeout at or above the floor"
# #1537 round 17. Tests 1-6 scan apps/web/scripts. They never saw the WORKFLOWS, which invoke
# `bun test src/` directly for apps/mcp-server — and this guard's own SCOPE LIMIT note mentioned
# mcp-server only to EXCLUDE it ("not apps/web's, verified"). That dismissal is what failed: on
# 2026-08-21 `MCP Server Validation` went red with
#     src/spec/index-builder.test.ts (fail) (unnamed) [6665.15ms]
#       ^ a beforeEach/afterEach hook timed out for this test
# — a 6.7s hook against bun's 5s default, on a loaded host, in a suite that passes when the host is
# idle. Exactly the defect this file was created for, one directory over. The same raw invocation is
# in deploy-production.yml, so a loaded PRODUCTION DEPLOY can fail the same way.
wf_bad=0
while IFS= read -r wf; do
  while IFS=: read -r lineno content; do
    case "$content" in
      *"--timeout"*)
        val=$(printf '%s\n' "$content" | sed -n 's/.*--timeout[ =]\{1,\}\([0-9]\{1,\}\).*/\1/p')
        if [ -z "$val" ] || [ "$val" -lt "$TIMEOUT_FLOOR" ]; then
          bad "$wf:$lineno runs 'bun test' with --timeout '${val:-<unparsed>}', below the ${TIMEOUT_FLOOR}ms floor"
          wf_bad=$((wf_bad+1))
        fi
        ;;
      *)
        bad "$wf:$lineno runs a raw 'bun test' with NO --timeout, so that suite runs at bun's 5s default: $(printf '%s' "$content" | sed 's/^ *//')"
        wf_bad=$((wf_bad+1))
        ;;
    esac
  done < <(/usr/bin/grep -nE '(^|[^a-z-])bun[[:space:]]+test([[:space:]]|$)' "$wf" 2>/dev/null | /usr/bin/grep -v '^[0-9]*: *#')
done < <(find "$REPO_ROOT/.github/workflows" -name '*.yml' 2>/dev/null | sort)
[ "$wf_bad" -eq 0 ] && ok "no workflow invokes a raw 'bun test' below the ${TIMEOUT_FLOOR}ms floor"

echo "Test 6: no unexecuted script under apps/web/scripts invokes 'bun test'"
unverified=""
while IFS= read -r f; do
    [ -n "$f" ] || continue
    rel="${f#"$WEB_DIR"/}"
    is_run=0
    for r in "${RUNNERS[@]}"; do
        [ "$rel" = "$r" ] && is_run=1
    done
    [ "$is_run" -eq 1 ] && continue
    body=$(cat "$f")
    case "$body" in
        *"bun test"*|*"bun run test"*|*"bunx test"*)
            unverified+="    $rel: mentions 'bun test' but is not reachable from package.json's test* scripts, so this guard never executes it and cannot check its timeout. Add it to package.json (so it is observed) or remove the invocation."$'\n'
            ;;
    esac
done < <(find "$WEB_DIR/scripts" -type f -name '*.sh' 2>/dev/null | sort)

if [ -z "$unverified" ]; then
    ok "every *.sh under apps/web/scripts that invokes 'bun test' is executed and observed above"
else
    bad "scripts invoking 'bun test' outside the observed set:"
    printf '%s' "$unverified"
fi

if [ -n "$other_invocations" ]; then
    echo
    echo "  (informational) non-'bun test' invocations observed:"
    printf '%s' "$other_invocations"
fi

finish
