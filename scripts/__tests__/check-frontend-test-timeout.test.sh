#!/usr/bin/env bash
# Guard: every `bun test` this repo runs must pass exactly one explicit --timeout of at least
# 15000ms, because apps/web/bunfig.toml's `[test] timeout` is not in effect.
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
#
# TWO REAL INCIDENTS, and this guard must keep catching both:
#   1. apps/web: test-conflict-files.sh passed the flag, test-regular.sh (the 362-file suite) did
#      not, and CI failed when CategoryEditor's "should close creation form and dropdown after
#      successful creation" — 441ms locally — took 9910ms on a loaded runner.
#   2. apps/mcp-server: the workflows ran `bun test src/` with NO --timeout, so that 283-test suite
#      ran at the 5s default and `MCP Server Validation` went red with a beforeEach hook timing out
#      at 6665ms in src/spec/index-builder.test.ts. The same raw invocation was in
#      deploy-production.yml, so a loaded production deploy could have failed identically.
#
# The floor is 15000, not "above bun's 5000 default": `--timeout 6000` would clear both a >5000
# check and the 5.6s probe below, yet incident 1 took 9910ms. The exact value 30000 is deliberately
# NOT pinned — that would fail a legitimate tuning change.
#
# ═══ HOW THIS GUARD WORKS: OBSERVE FIRST, READ TEXT ONLY AS A BACKSTOP ═════════════════════════
#
# Tests 1-6 OBSERVE what actually happens. A recording stub named `bun` (plus bunx/npm/npx/pnpm/
# yarn) is placed FIRST on PATH; it writes its own argv, NUL-delimited, and exits 0. Each frontend
# runner discovered from apps/web/package.json is then really EXECUTED with that PATH, and the guard
# reads the argv records. Whatever bash handed to `bun` IS the truth — comments, quoting of every
# kind, multiline strings, `\` continuations, `${VAR#pattern}`, arrays expanded as "$@", values that
# arrive from variables, helper functions called five times, and indirect invocation all come out
# resolved with no regex involved. Test 5 then proves the smallest observed value really survives a
# 5.6-second test under real bun, which is the only check here that would notice bun changing how it
# honours the flag.
#
# What the observing half does NOT know, stated plainly rather than left to be discovered:
#   - only the code paths TAKEN in the modes exercised below (no args, and --coverage) are observed.
#     A branch gated on some other condition (`if [ "$CI" = true ]`) is invisible. Mitigation: every
#     runner must produce at least one observed `bun test` in EVERY mode, or the guard hard-fails;
#   - it sees the RUNTIME value of --timeout, so it cannot tell a literal 30000 from a variable that
#     happened to hold 30000 — and does not care, since the runtime value is what bun obeys;
#   - a `bun` invoked by ABSOLUTE path bypasses the PATH stub. That direction fails closed: the run
#     produces zero argv records and the vacuity test fails;
#   - the stubs make every bun/npm/npx/pnpm/yarn call a no-op exit 0. If a runner ever needs one of
#     those to do real work, the run misbehaves LOUDLY (non-zero exit or zero records = FAIL).
#
# Test 7 is a TEXTUAL BACKSTOP for the surfaces that cannot be observed: a workflow `run:` block is
# executed by GitHub and a package.json script by bun's own runner, so there is no script this guard
# can invoke under a PATH shim to watch the argv. It is deliberately dumb — grep one line at a time,
# no YAML parser, no shell parser, no JS.
#
# ⚠ WHY DUMB, AND WHY THAT IS THE LESSON. Rounds 17-19 grew a 293-line embedded JS scanner here
# (Bun.YAML.parse, quote-state comment stripping, heredoc handling, delegation following, workspace
# globbing) plus 31 synthetic fixtures and a mutation harness to test it. Two independent adversarial
# reviews found ~18 defects in THAT code — 11 in one round, 7 in the next — and none, ever, in the
# observing half. It was not converging, and a false alarm in a repo-wide CI gate blocks everything.
# So it was deleted. The one lesson kept from it: FAIL LOUDLY ON ANYTHING THE TEXT CHECK CANNOT
# CONFIDENTLY READ, naming what confused it. A false alarm that explains itself is acceptable in a
# gate; a silent pass is not. Do not re-grow a parser here. If a shape is unreadable, make test 7
# say so and put the command on one readable line instead.
#
# OUT OF SCOPE for the textual check, named honestly instead of chased:
#   - a command whose `bun test` literal never appears on any line: assembled from pieces
#     (`P=test; bun $P`), or read from a file. Nothing can see it. NOTE, because it is easy to
#     over-claim here: `CMD="bun test"; $CMD src/` IS caught (verified) - the literal is right there
#     in the assignment, and this check does not care that it is inside quotes;
#   - a `bun test` split across source lines: a `\` continuation, or a YAML folded scalar that puts
#     `bun` and `test` on different lines. Test 7 reads ONE LINE AT A TIME and reports both shapes
#     as unreadable rather than guessing;
#   - `bun run <script>` in a workflow is not resolved to a manifest. It does not need to be: test 7
#     reads every workspace package.json, so the delegate's body is checked wherever it lives. A
#     package.json outside the repo (an action's own) is out of reach;
#   - workflow surfaces other than a `run:` line — a `with: args:` handed to a container action, a
#     `bun test` inside an `env:` default. Test 7 does not distinguish YAML keys at all: it reads
#     every line of the workflow, which over-reads rather than under-reads;
#   - a `bun test` inside a script file invoked by a workflow. apps/web/scripts is covered by tests
#     2-6 (executed) and test 6 (tripwire); a script under any OTHER directory is not covered here;
#   - a HEREDOC BODY is skipped: test.yml feeds python programs to `python3 - <<'PY'` and three of
#     them mention "bun test" inside an error-message f-string, which a shell-shaped read flags as a
#     false alarm (observed). So a `bun test` piped INTO a shell (`bash <<EOF ... EOF`) is invisible.
#     No such pattern exists here, and a command after the terminator is still read.
#   - A TRAILING COMMENT on a `bun test` line is REFUSED, not read: `bun test --timeout 30000 src/
#     # note` fails even though it is perfectly adequate (verified). An inline strip cannot tell that
#     `#` from a `${VAR#pat}` or a quoted one, and the whole point of deleting the 293-line scanner
#     was to stop doing quote-state parsing. So this refuses and SAYS SO, and the fix is to move the
#     comment to its own line. It is the one refusal an ordinary contributor is likely to trip, which
#     is why it is named here rather than left to be discovered as a red CI job.
#   Shapes that are sometimes assumed out of scope but are NOT - all VERIFIED to be flagged, because
#   a line-at-a-time grep does not care what surrounds the literal: `eval "bun test …"`,
#   `printf x | xargs bun test …`, a leading redirection (`2>/dev/null bun test src/`), and
#   `CMD="bun test"; $CMD`.
#
# Tests 1-5 cover the EXECUTED paths regardless of what test 7 can read, so test 7 is a backstop for
# files nothing here executes, not the primary defence.
#
# CONTRACT:
#   - test 1: the runner list is derived from apps/web/package.json (not hardcoded) and is non-empty;
#   - test 2: every runner, in every mode, exits 0 AND actually reached `bun` (vacuity, fails closed);
#   - test 3: every observed `bun test` passes EXACTLY one --timeout (`--timeout N` or `--timeout=N`),
#     an integer >= 15000. Two flags is a FAILURE, not something the guard resolves: bun honours the
#     last, so `--timeout 30000 --timeout 100` reads as 30s to a human and runs at 100ms;
#   - test 4: every observed `bun run test*` delegation resolves, in apps/web/package.json, to a
#     `bun test` line that passes test 7's textual rules;
#   - test 5: the SMALLEST observed value demonstrably survives a 5.6s test under real bun. A missing
#     `bun` binary is a hard FAIL, never a skip;
#   - test 6: no *.sh under apps/web/scripts that is NOT executed here contains a `bun test` at all,
#     once whole-line shell comments are stripped — a "you added an unverifiable runner" tripwire
#     whose failure direction is a false ALARM, never a false pass;
#   - test 7: every `bun test` written literally into .github/workflows/*.yml|*.yaml or into a
#     workspace package.json carries one adequate --timeout on the same line, and anything the check
#     cannot confidently read is a FAILURE that names itself. Fails closed on zero files read.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/apps/web"
TIMEOUT_FLOOR=15000
# The manifest an observed `bun run <script>` delegation is resolved against: the runners under
# apps/web/scripts run with apps/web as their package root.
DELEGATE_MANIFEST="$WEB_DIR/package.json"

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
# THE TEXTUAL CHECK, used by tests 4, 6 and 7. Grep, one line at a time, no state. On purpose —
# read the ⚠ note at the top of this file before making it cleverer.
#
# Rules, all of them:
#   - a line with no literal `bun test` on it is ignored;
#   - a line where the word `bun` is the last thing on it is UNREADABLE (the subcommand and any
#     flags are elsewhere) and is reported;
#   - a `#` anywhere at or after the `bun test` is UNREADABLE: it may be a comment hiding the rest
#     of the command, or it may be `${VAR#refs/heads/}` or `-t "issue #42"`. Refuse to decide;
#   - the number of `--timeout` flags must equal the number of `bun test` commands on the line, and
#     every value must be a literal integer >= the floor. A value that is a variable or a `${{ }}`
#     expression is refused, not assumed adequate.
# Whole-line comments are stripped by the caller (scan_file), which is the only stripping done: an
# inline strip could delete real command text after a quoted `#`, so ambiguity is reported instead.
# ---------------------------------------------------------------------------------------------

# Matches a literal `bun test` command. The leading class stops `mybun test` and `oven/bun test`;
# the trailing class stops `bun testfoo` while allowing the terminators that really occur — space,
# end of line, a JSON string's closing quote, and shell separators.
BUN_TEST_RE='(^|[^A-Za-z0-9_./-])bun[[:space:]]+test([[:space:]"'"'"';&|]|$)'

text_findings=""
text_count=0
text_min=""
CL_MIN=""

# occurrences <ere> <text> -> how many times <ere> matches
occurrences() {
    printf '%s' "$2" | /usr/bin/grep -oE -- "$1" | /usr/bin/grep -c ''
}

# check_line <line> <label>: the whole textual check. Appends to text_findings, sets CL_MIN to the
# smallest adequate value on this line, adds to text_count.
check_line() {
    local line="$1" label="$2" nbun ntimeout nvals vals v
    CL_MIN=""

    # `bun` with nothing after it: a `\` continuation, or a `run:` whose command starts on the next
    # line. Either way the subcommand and flags are elsewhere. Deliberately NOT a bare "line ends
    # with the word bun" test - that fires on `packageManager: bun`, a real false alarm on real
    # workflow files here.
    if printf '%s' "$line" | /usr/bin/grep -qE '((^|[[:space:]])bun[[:space:]]*\\|(^|[[:space:]])run:[[:space:]]*bun)[[:space:]]*$'; then
        text_findings+="    $label: the word 'bun' ends this line, so its subcommand and any --timeout are on another line. This check reads ONE LINE AT A TIME and will not guess: put the whole 'bun test ... --timeout N' on a single line."$'\n'
        return 0
    fi

    nbun=$(occurrences "$BUN_TEST_RE" "$line")
    [ "$nbun" -eq 0 ] && return 0

    # A backslash continuation after the command: the flags may be on the next line.
    case "$line" in
        *'\') text_findings+="    $label: this 'bun test' line is continued with a backslash, so its --timeout may be on the next line. This check reads one line at a time and will not guess: put the whole command on a single line. Line: $line"$'\n'
              return 0 ;;
    esac

    # A '#' at or after the command. Comment, or `${VAR#pat}`, or a quoted '#'? Unreadable.
    if printf '%s' "${line#*bun}" | /usr/bin/grep -q '#'; then
        text_findings+="    $label: a '#' appears at or after this 'bun test', and this check cannot tell a comment from a '\${VAR#pat}' or a quoted '#'. Rewrite the line so no '#' follows the command. Line: $line"$'\n'
        return 0
    fi

    ntimeout=$(occurrences '--timeout' "$line")
    if [ "$ntimeout" -ne "$nbun" ]; then
        text_findings+="    $label: $nbun 'bun test' command(s) on this line but $ntimeout --timeout flag(s). Each needs exactly one: with none it runs at bun's 5000ms default (the CI failure this guard exists for took 9910ms), and with two bun honours the LAST, so a large leading value masks a tiny trailing one. Line: $line"$'\n'
        return 0
    fi

    vals=$(printf '%s' "$line" | /usr/bin/grep -oE -- '--timeout[=[:space:]]+[^[:space:]",]*' | sed -E 's/^--timeout[=[:space:]]+//')
    nvals=$(printf '%s' "$vals" | /usr/bin/grep -c '[^[:space:]]')
    if [ "$nvals" -ne "$ntimeout" ]; then
        text_findings+="    $label: a --timeout on this line has no readable value after it. Line: $line"$'\n'
        return 0
    fi
    for v in $vals; do
        case "$v" in
            ''|*[!0-9]*)
                text_findings+="    $label: --timeout '$v' is not a literal integer. This check refuses variables and expressions rather than assuming they are adequate — write the number, or run the command from a script the guard can execute and observe (tests 2-5). Line: $line"$'\n'
                return 0
                ;;
        esac
        if [ "$v" -lt "$TIMEOUT_FLOOR" ]; then
            text_findings+="    $label: --timeout $v. That may clear bun's 5000ms default but not the observed slow case: the CI failure this guard exists for took 9910ms. Use at least $TIMEOUT_FLOOR. Line: $line"$'\n'
            return 0
        fi
        if [ -z "$CL_MIN" ] || [ "$v" -lt "$CL_MIN" ]; then CL_MIN="$v"; fi
    done
    if [ -n "$CL_MIN" ] && { [ -z "$text_min" ] || [ "$CL_MIN" -lt "$text_min" ]; }; then text_min="$CL_MIN"; fi
    text_count=$((text_count + nbun))
}

# scan_file <path> <label-prefix>: every line, with whole-line comments and HEREDOC BODIES dropped.
# Only lines mentioning `bun` at all are examined, which keeps this cheap.
#
# Heredoc bodies are skipped because test.yml feeds python programs to `python3 - <<'PY'`, and three
# of those programs mention "bun test" inside an error-message f-string. Reading them as shell is a
# false alarm on correct code (observed while writing this). The cost is stated in the out-of-scope
# list at the top: a `bun test` piped INTO a shell (`bash <<EOF ... EOF`) is not seen. No such
# pattern exists here, and a command AFTER the terminator is still read.
scan_file() {
    local f="$1" pfx="$2" lineno=0 line trimmed heredoc=""
    while IFS= read -r line || [ -n "$line" ]; do
        lineno=$((lineno + 1))
        trimmed="${line#"${line%%[![:space:]]*}"}"
        if [ -n "$heredoc" ]; then
            [ "${trimmed%%[[:space:]]*}" = "$heredoc" ] && heredoc=""
            continue
        fi
        if [[ $line =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*$ ]]; then
            heredoc="${BASH_REMATCH[1]}"
        fi
        case "$line" in *bun*) ;; *) continue ;; esac
        case "$trimmed" in '#'*) continue ;; esac
        check_line "$line" "$pfx:$lineno"
    done < "$f"
}

# resolve_pkg_script <manifest> <name> -> the script's command text; rc 1 if there is no such script
resolve_pkg_script() {
    BB_M="$1" BB_S="$2" bun -e '
const p = require(process.env.BB_M);
const v = (p.scripts || {})[process.env.BB_S];
if (typeof v !== "string") process.exit(1);
console.log(v);
' 2>/dev/null
}

# mentions_bun_test <file> -> 0 if the file still mentions a bun test invocation once WHOLE-LINE
# shell comments are gone. Test 6's tripwire.
#
# DEFECT 1 (#1537 round 18): the tripwire was a raw substring search over the whole file, so a
# helper whose only mention was `# Example command for contributors: bun test src/` was rejected and
# CI blocked over a comment. Only WHOLE-LINE comments are stripped: an inline strip could delete
# real command text after a quoted `#`, and the worst a whole-line strip can do is leave a trailing
# comment in place, which is a false ALARM — the acceptable direction for a tripwire.
mentions_bun_test() {
    local f="$1" body
    body=$(/usr/bin/grep -v -E '^[[:space:]]*#' "$f")
    case "$body" in
        *"bun test"*|*"bun run test"*|*"bunx test"*) return 0 ;;
    esac
    return 1
}
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

    # Delegation to a package.json script.
    #
    # DEFECT 5 (#1537 round 18): this used to be an automatic FAILURE ("the flags live somewhere
    # this run cannot observe") while the manifest scan RESOLVED the very same delegation and judged
    # the delegate's body. The two halves of this guard disagreed about whether `bun run test:unit`
    # is checkable. It is: the delegate is in the manifest, so follow it - with check_line(), the
    # same code test 7 uses - and report only when the delegation genuinely cannot be resolved (no
    # such script, unparseable manifest) or when what it resolves to is bad. A resolved delegation
    # contributes its smallest timeout to min_timeout, so test 5 still probes a value that was
    # really reached rather than skipping over the delegated run.
    #
    # No such delegation exists in the tree today (both runners invoke `bun test` directly), so this
    # branch is a guard against a future runner that delegates, not a live code path.
    if [ "$sub" = "run" ]; then
        case "$target" in
            test*)
                local d_body d_rc saved_findings saved_count saved_min
                d_body=$(resolve_pkg_script "$DELEGATE_MANIFEST" "$target")
                d_rc=$?
                if [ "$d_rc" -ne 0 ]; then
                    delegated+="    $label: $cmd run $target has no such script in $DELEGATE_MANIFEST (or the manifest could not be read), so its timeout is unverifiable. Invoke bun test directly with an explicit --timeout."$'\n'
                    return 0
                fi
                saved_findings="$text_findings"; saved_count="$text_count"; saved_min="$text_min"
                text_findings=""
                check_line "$d_body" "$label: $cmd run $target -> scripts.$target"
                if [ -n "$text_findings" ]; then
                    timeout_problems+="$text_findings"
                    text_findings="$saved_findings"; text_count="$saved_count"; text_min="$saved_min"
                    return 0
                fi
                text_findings="$saved_findings"; text_count="$saved_count"; text_min="$saved_min"
                if [ -z "$CL_MIN" ]; then
                    delegated+="    $label: $cmd run $target resolves in $DELEGATE_MANIFEST but its body reaches no readable 'bun test' at all, so this run's timeout was never verified."$'\n'
                    return 0
                fi
                test_invocations=$((test_invocations + 1))
                if [ -z "${min_timeout:-}" ] || [ "$CL_MIN" -lt "$min_timeout" ]; then
                    min_timeout="$CL_MIN"
                fi
                other_invocations+="    $label: $cmd run $target -> resolved in $DELEGATE_MANIFEST, smallest --timeout $CL_MIN"$'\n'
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
# Test 4: delegation to a package.json script. Followed, not refused - see check_record().
# ---------------------------------------------------------------------------------------------
echo "Test 4: every observed 'bun run test*' delegation resolves to a checked 'bun test'"
if [ -z "$delegated" ]; then
    ok "no observed delegation is unresolvable (a resolvable one is followed into $DELEGATE_MANIFEST and judged there)"
else
    bad "observed delegations whose timeout cannot be established:"
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
# This is deliberately the dumbest text check in the file: a literal substring search, no argument
# parsing at all. It only has to answer "is there a bun test in here that nothing above executed?".
#
# It used to strip nothing, and the "at worst it fires on a comment" that this note shrugged off was
# DEFECT 1 of #1537 round 18: a helper whose only mention was `# Example command for contributors:
# bun test src/` would have blocked CI for the whole repository. mentions_bun_test() now drops
# whole-line comments first, and nothing else, so the check still cannot produce a false pass on
# real command text.
# ---------------------------------------------------------------------------------------------
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
    if mentions_bun_test "$f"; then
        unverified+="    $rel: invokes 'bun test' but is not reachable from package.json's test* scripts, so this guard never executes it and cannot check its timeout. Add it to package.json (so it is observed) or remove the invocation."$'\n'
    fi
done < <(find "$WEB_DIR/scripts" -type f -name '*.sh' 2>/dev/null | sort)

if [ -z "$unverified" ]; then
    ok "every *.sh under apps/web/scripts that invokes 'bun test' is executed and observed above"
else
    bad "scripts invoking 'bun test' outside the observed set:"
    printf '%s' "$unverified"
fi


# ---------------------------------------------------------------------------------------------
# Test 7: every `bun test` WRITTEN DOWN in a file nothing above executes.
#
# Two surfaces, because that is where both real incidents lived: workflow `run:` blocks (incident 2
# — `bun test src/` for apps/mcp-server, in test.yml AND deploy-production.yml, no flag at all) and
# package.json `scripts` (apps/web's test:watch/test:all/test:integration and apps/mcp-server's
# test/test:watch, five scripts that ran with no flag).
#
# The file list is a literal glob, not discovery logic: a generated dist/package.json or a coverage/
# artefact must NOT be scanned (that false alarm shipped once), and `apps/*` + `packages/*` is what
# the root manifest's workspaces resolve to anyway. Both globs fail closed if they match nothing.
#
# A workflow COUNT of zero is fine — workflows are free to call `bun run <script>` and let the
# manifest scan check the body, which is what this repo does today. A manifest COUNT of zero is NOT:
# five such scripts exist, so zero means the scan broke rather than the tree being clean.
# ---------------------------------------------------------------------------------------------
echo "Test 7: every 'bun test' written into a workflow or a package.json carries one --timeout >= $TIMEOUT_FLOOR"

WF_FILES=()
for f in "$REPO_ROOT"/.github/workflows/*.yml "$REPO_ROOT"/.github/workflows/*.yaml; do
    [ -f "$f" ] && WF_FILES+=("$f")
done
PKG_FILES=()
for f in "$REPO_ROOT/package.json" "$REPO_ROOT"/apps/*/package.json "$REPO_ROOT"/packages/*/package.json; do
    [ -f "$f" ] && PKG_FILES+=("$f")
done

if [ "${#WF_FILES[@]}" -eq 0 ] || [ "${#PKG_FILES[@]}" -lt 2 ]; then
    bad "the textual scan read ${#WF_FILES[@]} workflow file(s) and ${#PKG_FILES[@]} manifest(s) - this repo has several of each, so a clean result would mean the file list is broken, not that the tree is clean"
else
    text_findings=""
    text_count=0
    for f in "${WF_FILES[@]}"; do
        scan_file "$f" "${f#"$REPO_ROOT"/}"
    done
    wf_count=$text_count
    for f in "${PKG_FILES[@]}"; do
        scan_file "$f" "${f#"$REPO_ROOT"/}"
    done
    pkg_count=$((text_count - wf_count))

    if [ -n "$text_findings" ]; then
        bad "written-down 'bun test' command lines that are inadequate or unreadable:"
        printf '%s' "$text_findings"
    elif [ "$pkg_count" -eq 0 ]; then
        bad "${#PKG_FILES[@]} manifest(s) were read but not one 'bun test' was found in any script - apps/web and apps/mcp-server both have several, so this is a broken scan, not a clean tree"
    else
        ok "${#WF_FILES[@]} workflow file(s) and ${#PKG_FILES[@]} manifest(s) read; $wf_count workflow and $pkg_count manifest 'bun test' command line(s), all with one --timeout >= $TIMEOUT_FLOOR"
    fi
fi

if [ -n "$other_invocations" ]; then
    echo
    echo "  (informational) non-'bun test' invocations observed:"
    printf '%s' "$other_invocations"
fi

finish
