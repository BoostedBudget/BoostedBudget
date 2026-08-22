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
# ═══ HOW THIS GUARD WORKS: IT EXECUTES THINGS AND READS THE ARGV ═══════════════════════════════
#
# Almost every check here OBSERVES what actually happens rather than reading source text. A
# recording stub named `bun` (plus bunx/npm/npx/pnpm/yarn and, for the manifest half, the other
# command names those bodies use) is placed on PATH; it writes its own argv, NUL-delimited, and
# exits 0. Two families of things are then really EXECUTED against it:
#
#   - the frontend RUNNER SCRIPTS discovered from apps/web/package.json (tests 1, 3);
#   - every PACKAGE.JSON SCRIPT, in the root manifest and in each declared workspace member, whose
#     body invokes `bun test` (tests 2, 4).
#
# Whatever bash handed to `bun` IS the truth — comments, quoting of every kind, multiline strings,
# `\` continuations, `${VAR#pattern}`, arrays expanded as "$@", values that arrive from variables,
# helper functions called five times, `$( )` substitution, and indirect invocation all come out
# resolved with no regex involved. Test 7 then proves the smallest observed value really survives a
# 5.6-second test under real bun, which is the only check here that would notice bun changing how it
# honours the flag.
#
# ⚠ ROUND-12 LESSON, DO NOT UNDO IT. Test 4 used to be a TEXTUAL scan of every package.json — grep
# the raw file for `bun test`, count `--timeout` flags on the line, parse their values. Two
# independent adversarial reviews confirmed it wrong in both directions:
#   - FALSE PASS: `bun test src/ && bunx playwright test --timeout 30000` counted one `bun test` and
#     one `--timeout` and passed, though the flag belongs to playwright and bun ran at 5000ms
#     (observed: guard exit 0). Same for `bun test --timeout 30000 --timeout 20000 src/ && bun test
#     src/`, and for `OUT=$(bun test)`.
#   - FALSE ALARM: `"description": "Run bun test locally"` failed the guard, because the scan read
#     the whole JSON file rather than the `scripts` object (observed: guard exit 1).
# Both classes die the moment the body is EXECUTED instead of read: the flag is attached to a
# process or it is not. So the manifest half now enumerates with JSON.parse over the `scripts`
# object only, and judges nothing but observed argv.
#
# What the observing half does NOT know, stated plainly rather than left to be discovered:
#   - only the code paths TAKEN in the modes exercised below (no args, and --coverage) are observed.
#     A branch gated on some other condition (`if [ "$CI" = true ]`) is invisible. Mitigation: every
#     runner must produce at least one observed `bun test` in EVERY mode, and every candidate
#     manifest script at least one, or the guard hard-fails;
#   - it sees the RUNTIME value of --timeout, so it cannot tell a literal 30000 from a variable that
#     happened to hold 30000 — and does not care, since the runtime value is what bun obeys;
#   - a `bun` invoked by ABSOLUTE path bypasses the PATH stub. That direction fails closed: the run
#     produces no `bun test` record and the vacuity test fails;
#   - the stubs make every intercepted command a no-op exit 0. If a runner ever needs one of those
#     to do real work, the run misbehaves LOUDLY (non-zero exit or no record = FAIL);
#   - a manifest body is executed by `/bin/bash -c`, not by bun's own script shell, and with a
#     HERMETIC PATH containing only the stub directory. So (a) a body using bun-shell-only syntax
#     would fail loudly rather than silently, and (b) a command name that is not stubbed is
#     "command not found", the `&&` chain stops, no `bun test` record appears, and test 4 fails
#     naming the script and showing the output. Nothing real can execute — which is deliberate:
#     `packages/api-client`'s `generate:api` contains `rm -rf node_modules`, and a guard must never
#     be one candidate-selection bug away from running that.
#
# Test 9 is the one check that reads text, and it has been reduced from a scanner to a TRIPWIRE.
# Because every package.json script is now observed, a workflow that says `bun run test` needs no
# further validation — the delegate's timeout is checked where it lives. So the only workflow rule
# left is: A WORKFLOW MUST NOT INVOKE `bun test` DIRECTLY. That deleted the whole terminator class,
# the aggregate flag counting and the value parsing. There are ZERO raw invocations today, so this
# is a tripwire against a regression, not a scanner; test 9 therefore also runs itself against a
# synthetic fixture of known-bad shapes and fails if the tripwire does not flag every one of them,
# so a scanner that silently matches nothing cannot read as a clean tree.
#
# ⚠ WHY DUMB, AND WHY THAT IS THE LESSON. Rounds 17-19 grew a 293-line embedded JS scanner here
# (Bun.YAML.parse, quote-state comment stripping, heredoc handling, delegation following, workspace
# globbing) plus 31 synthetic fixtures and a mutation harness to test it. Two independent adversarial
# reviews found ~18 defects in THAT code — 11 in one round, 7 in the next — and none, ever, in the
# observing half. It was not converging, and a false alarm in a repo-wide CI gate blocks everything.
# So it was deleted, and round 12 deleted its textual successor too. The one lesson kept: FAIL
# LOUDLY ON ANYTHING THE TEXT CHECK CANNOT CONFIDENTLY READ, naming what confused it. Do not
# re-grow a parser here, and in particular do not reintroduce shell quote-state tracking.
#
# OUT OF SCOPE, named honestly instead of chased. "Reachable" = could plausibly appear here.
#   - MANIFEST HALF — a script body that invokes bun test without the literal `bun test` in it:
#     assembled from pieces (`P=test; bun $P`), read from a file, or reached through `bun run` of a
#     .ts file that shells out. Candidate selection needs SOME textual trigger to decide what is
#     worth executing (executing every script is not an option: see `rm -rf node_modules` above).
#     NOT reachable today; nothing in any manifest looks like this.
#   - MANIFEST HALF — a package.json that is not the root and not inside a declared `workspaces`
#     glob. Enumeration follows `workspaces` (today `apps/*`, `packages/*`), which covers all five
#     manifests in the tree. A sixth added outside those globs would be invisible: reachable, and
#     the fix is to declare it as a workspace.
#   - MANIFEST HALF — `workspaces` glob shapes other than an exact path or `dir/*`. `**`, `?`, `[]`
#     and `{}` are REFUSED with a named error rather than half-implemented. Not reachable today.
#   - WORKFLOW TRIPWIRE — a HEREDOC BODY is skipped. test.yml feeds python programs to
#     `python3 - <<'PY'` and three of them mention "bun test" inside an error-message f-string,
#     which a shell-shaped read flags as a false alarm (observed). So a `bun test` piped INTO a
#     shell (`bash <<EOF ... EOF`) is invisible. Reachable in principle; no such pattern exists,
#     and a command after the terminator is still read.
#   - WORKFLOW TRIPWIRE — a line that is NOTHING BUT a diagnostic `echo`/`printf` is skipped. That
#     is deliberate: `echo "::error::bun test failed"` is this repo's own convention and flagging it
#     was a confirmed false alarm. The skip is withheld the moment the line also contains `&&`,
#     `||`, `;`, `|`, `$(` or a backtick, because `echo starting && bun test src/` carries a real
#     invocation and an unconditional first-word skip would be a fresh false pass (both directions
#     are pinned in test 9's own fixtures). What remains invisible is an `echo` with no separator
#     that nonetheless produces a runnable command, e.g. `echo "bun test src/" > run.sh`. Barely
#     reachable; not present. (A leading redirection such as `>&2 bun test src/` is NOT skipped —
#     `>&2` is not `echo` — so that one is still caught.)
#   - WORKFLOW TRIPWIRE — `bun test` split across source lines. Two shapes ARE refused: a line
#     whose entire content is `bun` (or `.../bun`), which is how a YAML folded scalar (`run: >-`)
#     hides an invocation, and a line ending in `bun \`. A third shape — `some cmd | bun \` followed
#     by `test src/`, i.e. a bare `bun` that is NOT the whole line and NOT a continuation — is
#     invisible. Barely reachable; the fix if it ever appears is to refuse it too.
#   - WORKFLOW TRIPWIRE — it does not distinguish YAML keys at all: it reads every line of the
#     workflow, which over-reads rather than under-reads. A `bun test` in a `with: args:` or an
#     `env:` default is therefore caught, not missed.
#   - A `bun test` inside a script file invoked by a workflow. apps/web/scripts is covered by tests
#     1/3 (executed) and test 8 (tripwire); a script under any OTHER directory is not covered.
#
# CONTRACT:
#   - test 1: the runner list is derived from apps/web/package.json (not hardcoded) and is non-empty;
#   - test 2: the manifest/script universe is enumerated with JSON.parse over the root manifest plus
#     each declared workspace member, `scripts` object only, and is non-empty in all three counts;
#   - test 3: every runner, in every mode, exits 0 AND produced at least one observed `bun test` —
#     PER (runner, mode), not merely somewhere in the aggregate. A mode gutted to `bun run lint;
#     exit 0` used to pass on the aggregate floor alone (observed);
#   - test 4: every enumerated script whose body invokes `bun test` is EXECUTED and exits 0 AND
#     produced at least one observed `bun test`;
#   - test 5: every observed `bun test`, from either family, passes EXACTLY one --timeout
#     (`--timeout N` or `--timeout=N`), an integer >= 15000. Two flags is a FAILURE, not something
#     the guard resolves: bun honours the last, so `--timeout 30000 --timeout 100` reads as 30s to a
#     human and runs at 100ms;
#   - test 6: every observed `bun run <target>` resolves — to a script this guard enumerated, or to
#     a file on disk. An unresolvable target is a FAILURE, because it means the universe is wrong;
#   - test 7: the SMALLEST observed value demonstrably survives a 5.6s test under real bun. A
#     missing `bun` binary is a hard FAIL, never a skip;
#   - test 8: no *.sh under apps/web/scripts that is NOT executed here contains a `bun test` at all,
#     once whole-line shell comments are stripped — a "you added an unverifiable runner" tripwire
#     whose failure direction is a false ALARM, never a false pass;
#   - test 9: no line of .github/workflows/*.yml|*.yaml invokes `bun test` directly, and the
#     tripwire proves itself against a fixture of known-bad shapes first. Fails closed on zero
#     files read.
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

if ! command -v bun >/dev/null 2>&1; then
    bad "bun is not on PATH. This repo requires bun; without it neither the runner discovery nor the effect probe can run, and a green result would be meaningless."
    finish
fi

# ── Test 1: runner discovery ───────────────────────────────────────────────────────────────────
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

# ── Test 2: enumerate the manifest/script universe with JSON.parse ─────────────────────────────
#
# NOTE ON THE ONE REGEX IN HERE. Selecting which scripts are worth EXECUTING needs a textual
# trigger, and it is the same shape the workflow tripwire uses: a `bun` (optionally reached through
# a path, since `~/.bun/bin` is bun's default install location) followed by whitespace and the word
# `test`. Over-selection is harmless — the body is executed and its argv judged. Under-selection is
# the risk, and it is named in the out-of-scope list above.
echo "Test 2: the manifest/script universe is enumerated with JSON.parse from declared workspaces"

MAN_OUT="$WORK/man"
mkdir -p "$MAN_OUT"
cat > "$WORK/enum.js" <<'ENUM'
const fs = require("fs");
const path = require("path");

const root = process.env.BB_ROOT;
const out = process.env.BB_OUT;

// Same shape as the workflow tripwire's rule. A leading class that excludes only [A-Za-z0-9_-]
// deliberately ALLOWS `/`, `.` and `~`, so `/opt/homebrew/bin/bun test` and `~/.bun/bin/bun test`
// are seen. The trailing lookahead stops `bun tests/` matching.
const BUN_TEST = /(?:^|[^A-Za-z0-9_-])bun[ \t]+test(?![A-Za-z0-9_-])/;

function die(msg) {
  fs.writeFileSync(path.join(out, "error.txt"), msg + "\n");
  process.exit(3);
}

function readJson(p) {
  let raw;
  try {
    raw = fs.readFileSync(p, "utf8");
  } catch (e) {
    die("cannot read manifest " + p + ": " + e.message);
  }
  try {
    return JSON.parse(raw);
  } catch (e) {
    die("manifest " + p + " is not parseable JSON, so its scripts cannot be enumerated: " + e.message);
  }
}

const rootMan = path.join(root, "package.json");
const rootPkg = readJson(rootMan);

let globs;
if (Array.isArray(rootPkg.workspaces)) {
  globs = rootPkg.workspaces;
} else if (rootPkg.workspaces && Array.isArray(rootPkg.workspaces.packages)) {
  globs = rootPkg.workspaces.packages;
} else {
  die(
    "the root package.json has no readable `workspaces` array (nor `workspaces.packages`), so the " +
      "set of manifests to check cannot be derived. This guard refuses to fall back to a hardcoded " +
      "list, because a hardcoded list is exactly how a new workspace goes unchecked."
  );
}

const manifests = [rootMan];
for (const g of globs) {
  if (typeof g !== "string") {
    die("workspace entry " + JSON.stringify(g) + " is not a string; this enumeration will not guess at it.");
  }
  if (/[*?[\]{}]/.test(g.replace(/\/\*$/, ""))) {
    die(
      "workspace glob '" + g + "' uses a shape this enumeration does not implement (only an exact " +
        "path or 'dir/*' are supported; '**', '?', '[]' and '{}' are refused rather than " +
        "half-matched). Either simplify the glob or teach this enumeration the shape."
    );
  }
  if (!g.includes("*")) {
    const m = path.join(root, g, "package.json");
    if (fs.existsSync(m)) manifests.push(m);
    else die("workspace entry '" + g + "' has no package.json at " + m);
    continue;
  }
  if (!g.endsWith("/*")) {
    die("workspace glob '" + g + "' contains a '*' that is not a trailing '/*'; refused rather than guessed.");
  }
  const dir = path.join(root, g.slice(0, -2));
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch (e) {
    die("workspace glob '" + g + "' points at a directory that cannot be read: " + dir);
  }
  for (const e of entries.sort((a, b) => (a.name < b.name ? -1 : 1))) {
    if (!e.isDirectory()) continue;
    const m = path.join(dir, e.name, "package.json");
    if (fs.existsSync(m)) manifests.push(m);
  }
}

const manLines = [];
const scriptLines = [];
const candLines = [];
let idx = 0;

for (const m of manifests) {
  const rel = path.relative(root, m);
  manLines.push(rel);
  const pkg = readJson(m);
  const scripts = pkg.scripts;
  if (scripts === undefined) continue;
  if (scripts === null || typeof scripts !== "object" || Array.isArray(scripts)) {
    die("manifest " + rel + " has a `scripts` field that is not an object; refused rather than guessed.");
  }
  for (const [name, body] of Object.entries(scripts)) {
    if (typeof body !== "string") {
      die("manifest " + rel + " script '" + name + "' has a non-string body; refused rather than guessed.");
    }
    scriptLines.push(rel + "\t" + name);
    if (!BUN_TEST.test(body)) continue;
    idx += 1;
    const tag = String(idx).padStart(4, "0");
    fs.writeFileSync(path.join(out, "body." + tag), body);
    candLines.push(tag + "\t" + rel + "\t" + name);
  }
}

fs.writeFileSync(path.join(out, "manifests.txt"), manLines.join("\n") + (manLines.length ? "\n" : ""));
fs.writeFileSync(path.join(out, "scripts.txt"), scriptLines.join("\n") + (scriptLines.length ? "\n" : ""));
fs.writeFileSync(path.join(out, "candidates.txt"), candLines.join("\n") + (candLines.length ? "\n" : ""));
ENUM

BB_ROOT="$REPO_ROOT" BB_OUT="$MAN_OUT" bun "$WORK/enum.js" >"$MAN_OUT/enum.log" 2>&1
enum_rc=$?

man_count=0
script_count=0
CANDS=()
if [ "$enum_rc" -ne 0 ]; then
    bad "enumerating the manifest/script universe failed (exit $enum_rc), so tests 4-6 would judge nothing:"
    if [ -f "$MAN_OUT/error.txt" ]; then
        printf '    %s' "$(cat "$MAN_OUT/error.txt")"
        echo
    fi
    [ -s "$MAN_OUT/enum.log" ] && printf '    %s\n' "$(tail -5 "$MAN_OUT/enum.log")"
else
    man_count=$(/usr/bin/grep -c '[^[:space:]]' "$MAN_OUT/manifests.txt" 2>/dev/null || echo 0)
    script_count=$(/usr/bin/grep -c '[^[:space:]]' "$MAN_OUT/scripts.txt" 2>/dev/null || echo 0)
    while IFS= read -r c; do
        [ -n "$c" ] && CANDS+=("$c")
    done < "$MAN_OUT/candidates.txt"

    if [ "$man_count" -lt 2 ] || [ "$script_count" -lt 5 ] || [ "${#CANDS[@]}" -lt 2 ]; then
        bad "the enumeration read $man_count manifest(s), $script_count script(s) and found ${#CANDS[@]} that invoke 'bun test'. This repo has five manifests and several bun test scripts, so these numbers mean the enumeration is broken, not that the tree is clean."
    else
        ok "$man_count manifest(s), $script_count script(s); ${#CANDS[@]} invoke 'bun test' and are executed in test 4"
    fi
fi

# ── recording stub machinery, shared by tests 3 and 4 ──────────────────────────────────────────
#
# One stub implementation, symlinked under many command names, used by both families. It uses only
# bash builtins (no basename/cat) because the manifest half runs it under a HERMETIC PATH that
# contains nothing but the stub directory.
SHIM_DIR="$WORK/shim"
HERM_DIR="$WORK/hermetic"
mkdir -p "$SHIM_DIR" "$HERM_DIR"
cat > "$WORK/.record" <<'STUB'
#!/bin/bash
set -u
_name=${0##*/}
_seq=0
if [ -r "$BB_ARGV_DIR/.seq" ]; then read -r _seq < "$BB_ARGV_DIR/.seq"; fi
_seq=$((_seq + 1))
printf '%s\n' "$_seq" > "$BB_ARGV_DIR/.seq"
printf '%s\0' "$_name" "$@" > "$BB_ARGV_DIR/$(printf 'rec.%04d' "$_seq")"
exit 0
STUB
chmod +x "$WORK/.record"

# The runner family keeps the original, narrow name set: those scripts run under a REAL PATH, so
# anything not listed here still works as it normally would.
for c in bun bunx npm npx pnpm yarn; do
    ln -s "$WORK/.record" "$SHIM_DIR/$c"
done
# The manifest family runs under PATH="$HERM_DIR" alone. Every command name a candidate body uses
# must be here or the body fails loudly with "command not found" — which is the safe direction, and
# is why nothing real (`rm -rf node_modules`, a live playwright run, a 40-second tsc) can execute.
for c in bun bunx npm npx pnpm yarn node deno tsc vite playwright eslint prettier turbo wrangler; do
    ln -s "$WORK/.record" "$HERM_DIR/$c"
done

MODE_ARGS=("" "--coverage")
MODE_NAMES=("no args" "--coverage")

timeout_problems=""
other_invocations=""
delegation_records=""
test_invocations=0
ctx_test_invocations=0
min_timeout=""

# check_record <record-file> <label> <manifest-rel-path>
# Reads one NUL-delimited argv record and judges it. Only `bun test` is judged; a `bun run <target>`
# is remembered for test 6; everything else is informational.
check_record() {
    local rec="$1" label="$2" man_rel="$3"
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

    if [ "$cmd" = "bun" ] && [ "$sub" = "run" ]; then
        if [ -n "$target" ]; then
            delegation_records+="$label"$'\t'"$man_rel"$'\t'"$target"$'\n'
        fi
        other_invocations+="    $label: bun run $target (a delegation - its own script is enumerated and judged in test 4)"$'\n'
        return 0
    fi

    if [ "$cmd" != "bun" ] || [ "$sub" != "test" ]; then
        other_invocations+="    $label: $cmd $sub ... (not a 'bun test' - ignored)"$'\n'
        return 0
    fi

    test_invocations=$((test_invocations + 1))
    ctx_test_invocations=$((ctx_test_invocations + 1))

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
        timeout_problems+="    $label: bun test was executed with NO --timeout, so it runs at bun's 5000ms default (the CI failure this guard exists for took 9910ms). If a --timeout appears elsewhere on the same command line it belongs to a DIFFERENT process; this is the argv bun itself received."$'\n'
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

# collect_records <dir> <label-prefix> <manifest-rel>
# Feeds every argv record in <dir> to check_record, and reports how many there were via REC_COUNT.
REC_COUNT=0
collect_records() {
    local dir="$1" label="$2" man_rel="$3"
    local recs=() r k=0
    while IFS= read -r r; do
        [ -n "$r" ] && recs+=("$r")
    done < <(find "$dir" -maxdepth 1 -type f -name 'rec.*' 2>/dev/null | sort)
    REC_COUNT="${#recs[@]}"
    for r in "${recs[@]}"; do
        k=$((k + 1))
        check_record "$r" "$label invocation #$k" "$man_rel"
    done
}

# ── Test 3: execute every runner, in every mode ────────────────────────────────────────────────
echo "Test 3: each runner, in EVERY mode, executes under a recording 'bun', exits 0, and reaches 'bun test'"

vacuity_problems=""
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

        ctx_test_invocations=0
        collect_records "$run_dir" "$rel ($mode_name)" "apps/web/package.json"

        if [ "$rc" -ne 0 ]; then
            vacuity_problems+="    $rel ($mode_name): exited $rc under the recording stub, so its observation cannot be trusted. Last output: $(tail -3 "$log" | tr '\n' ' ')"$'\n'
        fi
        if [ "$REC_COUNT" -eq 0 ]; then
            vacuity_problems+="    $rel ($mode_name): never invoked bun/bunx/npm/npx/pnpm/yarn at all - zero argv records. Either the runner no longer runs bun, or it calls it by absolute path where PATH cannot intercept it."$'\n'
        elif [ "$ctx_test_invocations" -eq 0 ]; then
            vacuity_problems+="    $rel ($mode_name): produced $REC_COUNT argv record(s) but NOT ONE was a 'bun test'. This mode runs no tests, so nothing about its timeout was verified. A per-mode floor exists because the aggregate one does not catch it: a mode reduced to 'bun run lint; exit 0' still left a dozen 'bun test' invocations in the total (observed)."$'\n'
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
    ok "$run_index runner execution(s) all exited 0 and each reached 'bun test'; $test_invocations invocation(s) observed"
fi

# ── Test 4: execute every enumerated script that invokes bun test ──────────────────────────────
echo "Test 4: every package.json script that invokes 'bun test' is EXECUTED and its argv observed"

if [ "${#CANDS[@]}" -eq 0 ]; then
    bad "no candidate manifest script to execute (see test 2) - the manifest half verified nothing"
else
    man_problems=""
    man_observed=0
    for c in "${CANDS[@]}"; do
        tag="${c%%$'\t'*}"
        rest="${c#*$'\t'}"
        man_rel="${rest%%$'\t'*}"
        sname="${rest#*$'\t'}"

        body=$(/bin/cat "$MAN_OUT/body.$tag")
        man_dir="$REPO_ROOT/$(dirname "$man_rel")"
        cand_dir="$WORK/cand.$tag"
        mkdir -p "$cand_dir"
        clog="$cand_dir/output.log"

        ( cd "$man_dir" && BB_ARGV_DIR="$cand_dir" PATH="$HERM_DIR" /bin/bash -c "$body" ) >"$clog" 2>&1
        crc=$?

        ctx_test_invocations=0
        collect_records "$cand_dir" "$man_rel scripts.$sname" "$man_rel"

        if [ "$crc" -ne 0 ]; then
            man_problems+="    $man_rel scripts.$sname: executing the body exited $crc, so its argv cannot be trusted. Body: $body -- last output: $(tail -3 "$clog" | tr '\n' ' ') (a 'command not found' here means the body uses a command this guard does not stub; add it to HERM_DIR)"$'\n'
        elif [ "$ctx_test_invocations" -eq 0 ]; then
            man_problems+="    $man_rel scripts.$sname: the body was selected because it contains the literal 'bun test', but executing it produced $REC_COUNT argv record(s) and NOT ONE was a 'bun test'. Body: $body"$'\n'
        else
            man_observed=$((man_observed + 1))
        fi
    done

    if [ -n "$man_problems" ]; then
        bad "manifest scripts whose 'bun test' could not be observed:"
        printf '%s' "$man_problems"
    else
        ok "$man_observed manifest script(s) executed under the hermetic stub, each exited 0 and reached 'bun test'"
    fi
fi

# ── Test 5: judge every observed bun test ──────────────────────────────────────────────────────
echo "Test 5: every observed 'bun test' passes exactly one --timeout >= $TIMEOUT_FLOOR"
if [ "$test_invocations" -eq 0 ]; then
    bad "no 'bun test' invocation was observed anywhere, so there is nothing to check - treated as a failure, not a pass"
elif [ -n "$timeout_problems" ]; then
    bad "bun test invocations with an unusable timeout:"
    printf '%s' "$timeout_problems"
else
    ok "all $test_invocations observed 'bun test' invocation(s) pass exactly one --timeout >= $TIMEOUT_FLOOR (smallest: $min_timeout, covering the observed 9910ms case)"
fi

# ── Test 6: every observed delegation resolves into the enumerated universe ────────────────────
echo "Test 6: every observed 'bun run <target>' resolves to an enumerated script or a file on disk"
deleg_problems=""
deleg_seen=0
if [ -n "$delegation_records" ]; then
    while IFS=$'\t' read -r d_label d_man d_target; do
        [ -n "$d_target" ] || continue
        deleg_seen=$((deleg_seen + 1))
        if [ ! -f "$MAN_OUT/scripts.txt" ]; then
            deleg_problems+="    $d_label: 'bun run $d_target' cannot be resolved because the script universe was never enumerated (test 2 failed)."$'\n'
            continue
        fi
        if /usr/bin/grep -qxF -- "$d_man"$'\t'"$d_target" "$MAN_OUT/scripts.txt"; then
            continue
        fi
        if [ -e "$REPO_ROOT/$(dirname "$d_man")/$d_target" ]; then
            continue
        fi
        deleg_problems+="    $d_label: 'bun run $d_target' names neither a script in $d_man nor a file on disk, so whatever it runs is outside everything this guard enumerated."$'\n'
    done <<< "$delegation_records"
fi
if [ -n "$deleg_problems" ]; then
    bad "observed delegations that resolve to nothing this guard knows about:"
    printf '%s' "$deleg_problems"
else
    ok "$deleg_seen observed delegation(s) all resolve to an enumerated script or a file on disk"
fi

# ── Test 7: the live effect probe ──────────────────────────────────────────────────────────────
echo "Test 7: the smallest observed --timeout demonstrably survives a 5.6s test under real bun"
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

# ── Test 8: unexecuted runner tripwire ─────────────────────────────────────────────────────────
mentions_bun_test() {
    local f="$1" body
    body=$(/usr/bin/grep -v -E '^[[:space:]]*#' "$f")
    case "$body" in
        *"bun test"*|*"bun run test"*|*"bunx test"*) return 0 ;;
    esac
    return 1
}

echo "Test 8: no unexecuted script under apps/web/scripts invokes 'bun test'"
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

# ── Test 9: the workflow tripwire ──────────────────────────────────────────────────────────────
#
# A workflow cannot be executed under a PATH shim, so this one surface is read as text. It no
# longer VALIDATES anything, because every package.json script is now observed: a workflow that
# says `bun run test` is already covered where the timeout lives. The only rule left is that a
# workflow must not run `bun test` itself.
echo "Test 9: no workflow invokes 'bun test' directly (delegate via 'bun run <script>' instead)"

# Same leading class as the enumeration's: excluding only [A-Za-z0-9_-] deliberately ALLOWS `/`,
# `.`, `~`, `(`, `"` before `bun`, so `/opt/homebrew/bin/bun test`, `~/.bun/bin/bun test` and
# `$(bun test)` are all seen. The old class excluded `/`, `.` and `-`, which made every pathed
# invocation invisible (observed: guard exit 0 with `/opt/homebrew/bin/bun test src/` in a workflow).
WF_BUN_TEST_RE='(^|[^A-Za-z0-9_-])bun[[:space:]]+test([^A-Za-z0-9_-]|$)'

wf_findings=""
wf_lines=0

scan_workflow() {
    local f="$1" pfx="$2"
    local lineno=0 line trimmed heredoc="" body nb last first
    while IFS= read -r line || [ -n "$line" ]; do
        lineno=$((lineno + 1))
        wf_lines=$((wf_lines + 1))
        trimmed="${line#"${line%%[![:space:]]*}"}"

        # Heredoc bodies are skipped: test.yml feeds python programs to `python3 - <<'PY'` whose
        # error strings mention "bun test", and reading them shell-shaped is a false alarm.
        if [ -n "$heredoc" ]; then
            [ "${trimmed%%[[:space:]]*}" = "$heredoc" ] && heredoc=""
            continue
        fi
        if [[ $line =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*$ ]]; then
            heredoc="${BASH_REMATCH[1]}"
        fi

        case "$line" in *bun*) ;; *) continue ;; esac

        # Peel a YAML sequence dash and an optional `run:` key, so the first word of the COMMAND is
        # what gets classified.
        body="$trimmed"
        case "$body" in '- '*) body="${body#- }"; body="${body#"${body%%[![:space:]]*}"}" ;; esac
        case "$body" in 'run:'*) body="${body#run:}"; body="${body#"${body%%[![:space:]]*}"}" ;; esac

        # Comments are skipped outright. A diagnostic `echo`/`printf` is skipped too —
        # `echo "::error::bun test failed"` is this repo's own convention for reporting a failure and
        # flagging it was a confirmed false alarm — but ONLY when the line is nothing but that
        # diagnostic. `echo starting && bun test src/` still has a real invocation on it, and an
        # unconditional first-word skip would have handed back a fresh false pass.
        first="${body%%[[:space:]]*}"
        case "$first" in '#'*) continue ;; esac
        case "$first" in
            echo|printf)
                case "$body" in
                    *'&&'*|*'||'*|*';'*|*'|'*|*'$('*|*'`'*) ;;
                    *) continue ;;
                esac
                ;;
        esac

        # A `\` continuation whose last word is `bun`: the subcommand is on the next line.
        case "$line" in
            *'\')
                nb="${line%\\}"
                nb="${nb%"${nb##*[![:space:]]}"}"
                last="${nb##*[[:space:]]}"
                case "$last" in
                    bun|*/bun)
                        wf_findings+="    $pfx:$lineno: the word 'bun' ends this line with a '\\' continuation, so its subcommand is on another line. This tripwire reads ONE LINE AT A TIME and will not guess. Line: $line"$'\n'
                        continue
                        ;;
                esac
                ;;
        esac

        # A line whose ENTIRE content is `bun`: that is how a YAML folded scalar (`run: >-`) hides an
        # invocation, with `test src/` on the following line. Refused, not guessed.
        case "$body" in
            bun|*/bun)
                wf_findings+="    $pfx:$lineno: this line's entire content is 'bun', which in a YAML folded scalar (run: >- / run: >) joins with the NEXT line into a single command. This tripwire reads raw file lines, not parsed YAML, so it refuses rather than guess what the command turns out to be. Put the whole command on one line, or delegate with 'bun run <script>'. Line: $line"$'\n'
                continue
                ;;
        esac

        if printf '%s' "$body" | /usr/bin/grep -qE -- "$WF_BUN_TEST_RE"; then
            wf_findings+="    $pfx:$lineno: a workflow must not invoke 'bun test' directly. A raw invocation carries whatever timeout is typed on that line, and nothing executes it here, so this guard cannot observe the argv bun really receives - which is exactly how apps/mcp-server's 283-test suite ran at the 5s default in both test.yml and deploy-production.yml. Delegate instead: 'bun run <script>', and put the --timeout in the package.json script, where tests 2/4/5 execute it and read the real argv. Line: $line"$'\n'
        fi
    done < "$f"
}

# Self-test first. The live expectation is ZERO findings, so a tripwire that silently matched
# nothing would be indistinguishable from a clean tree. Each fixture line below is a shape that
# was CONFIRMED to slip through an earlier version of this check, or that must keep being caught.
FIXTURE="$WORK/tripwire_fixture.yml"
cat > "$FIXTURE" <<'FIX'
jobs:
  a:
    steps:
      - name: plain
        run: bun test src/
      - name: homebrew path
        run: /opt/homebrew/bin/bun test src/
      - name: bun default install path
        run: ~/.bun/bin/bun test src/
      - name: command substitution
        run: OUT=$(bun test); echo "$OUT"
      - name: folded scalar
        run: >-
          bun
          test src/
      - name: backslash continuation
        run: |
          bun \
            test src/
      - name: borrowed flag
        run: bun test src/ && bunx playwright test --timeout 30000
      - name: duplicate flags
        run: bun test --timeout 30000 --timeout 20000 src/
      - name: real command hiding behind a diagnostic echo
        run: echo starting && bun test src/
FIX
FIXTURE_EXPECTED=9
wf_findings=""
scan_workflow "$FIXTURE" "fixture"
fixture_hits=$(printf '%s' "$wf_findings" | /usr/bin/grep -c 'fixture:' || true)

# And a negative fixture: shapes that MUST NOT be flagged, each one a confirmed false alarm.
NEG="$WORK/tripwire_negative.yml"
cat > "$NEG" <<'NEG'
jobs:
  a:
    steps:
      - name: Setup bun
        uses: oven-sh/setup-bun@v2
      - name: delegate
        run: bun run test   # timeout lives in apps/mcp-server/package.json
      - name: diagnostic echo
        run: echo "::error::bun test failed"
      - name: diagnostic printf
        run: printf '%s\n' "bun test --timeout 0"
      - name: diagnostic echo of a --timeout value
        run: echo "--timeout 0"
      - name: commented out
        run: |
          # bun test src/ was moved into package.json
          bun run test:ci
      - name: python heredoc
        run: |
          python3 - <<'PY'
          print("::error::file does not contain exactly one bun test summary")
          PY
NEG
wf_findings=""
scan_workflow "$NEG" "negative"
neg_hits="$wf_findings"

if [ "$fixture_hits" -ne "$FIXTURE_EXPECTED" ]; then
    bad "the tripwire's own self-test flagged $fixture_hits of $FIXTURE_EXPECTED known-bad fixture line(s). A tripwire that matches nothing looks identical to a clean tree, so this is a failure of the CHECK, not of the repo. Findings were:"
    printf '%s' "$wf_findings"
elif [ -n "$neg_hits" ]; then
    bad "the tripwire flagged line(s) in its own must-not-flag fixture - these are confirmed false alarms and would block every CI job:"
    printf '%s' "$neg_hits"
else
    WF_FILES=()
    for f in "$REPO_ROOT"/.github/workflows/*.yml "$REPO_ROOT"/.github/workflows/*.yaml; do
        [ -f "$f" ] && WF_FILES+=("$f")
    done
    if [ "${#WF_FILES[@]}" -eq 0 ]; then
        bad "read 0 workflow file(s) from $REPO_ROOT/.github/workflows - this repo has several, so a clean result would mean the file list is broken, not that the tree is clean"
    else
        wf_findings=""
        wf_lines=0
        for f in "${WF_FILES[@]}"; do
            scan_workflow "$f" "${f#"$REPO_ROOT"/}"
        done
        if [ -n "$wf_findings" ]; then
            bad "workflow line(s) that invoke 'bun test' directly, or that hide a 'bun' whose subcommand is on another line:"
            printf '%s' "$wf_findings"
        else
            ok "self-test caught $fixture_hits/$FIXTURE_EXPECTED known-bad shapes and none of the must-not-flag ones; ${#WF_FILES[@]} workflow file(s), $wf_lines line(s) read, zero direct 'bun test' invocations"
        fi
    fi
fi

if [ -n "$other_invocations" ]; then
    echo
    echo "  (informational) non-'bun test' invocations observed:"
    printf '%s' "$other_invocations"
fi

finish
