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
#   - the frontend RUNNER SCRIPTS discovered from apps/web/package.json (tests 1, 3), both
#     synthetically and as replays of the package.json scripts that launch them;
#   - every PACKAGE.JSON SCRIPT, in the root manifest and in each declared workspace member, whose
#     body invokes `bun test` (tests 2, 4).
#
# The stub claims its record filename ATOMICALLY (set -C + `>`, i.e. O_EXCL). The read-modify-write
# `.seq` counter it replaced lost records under concurrency — 24 concurrent invocations produced 8
# records — and losing the BAD record is a false pass. See the note above the stub.
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
# The runner half executes each runner TWICE OVER. Once synthetically, in a hardcoded matrix of
# modes (no args, --coverage) — that is the per-runner floor, and it is what would catch a runner
# that stops running tests in a mode nothing currently uses. And once by REPLAYING the package.json
# script that launches it (`test`, `test:ci`) as its own body: same cwd, same env prefixes, same
# argument shapes, same `&&` chaining. The replay exists because the synthetic matrix is a GUESS
# about how the runner is called, and the guess was falsified in both directions — see the two
# findings recorded at test 3, phase B. Whatever branch CI takes is now the branch observed.
#
# What the observing half does NOT know, stated plainly rather than left to be discovered:
#   - only the code paths TAKEN in the invocations performed below are observed. Those are now the
#     invocations package.json itself performs, plus the synthetic matrix — but a branch gated on
#     something NEITHER supplies (a workflow step's own `env:` block, a runner-only environment
#     variable set by the CI image) is still invisible. Mitigation: every runner must produce at
#     least one observed `bun test` in EVERY synthetic mode AND in every replayed package.json
#     invocation, and every candidate manifest script at least one, or the guard hard-fails;
#   - it sees the RUNTIME value of --timeout, so it cannot tell a literal 30000 from a variable that
#     happened to hold 30000 — and does not care, since the runtime value is what bun obeys;
#   - a `bun` reached through a PATH-BEARING name bypasses the PATH stub, so its argv can never be
#     observed. This does NOT fail closed — an earlier version of this header claimed it did, and
#     that claim was wrong: it holds only in isolation. With any sibling invocation in the same
#     execution producing a good record, the run reads clean (observed: `bun test --timeout 30000
#     src/ && /tmp/redbun/bun test src/` in apps/mcp-server's `test` script gave 9 passed, 0
#     failed). The shape is therefore REFUSED by name, in the manifest half (test 2) and the runner
#     half (tests 3 and 8) alike;
#   - a `bun test` handed to another command AS AN ARGUMENT (`npx --yes concurrently "bun test
#     src/"`) never runs: every stub records its argv and returns without executing anything. Again
#     a sibling good record hides it (observed: 9 passed, 0 failed). Also REFUSED by name, in
#     check_record, rather than analysed — running the nested command for real is exactly what the
#     stubs exist to prevent;
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
#     glob. This USED to be invisible (confirmed: a `tools/ci/package.json` whose `test` script ran
#     `bun test src/`, invoked from test.yml with a `working-directory`, left the guard at 9 passed,
#     0 failed). It is now REFUSED, not enumerated: test 2 lists every package.json `git ls-files`
#     knows about — tracked and untracked-but-not-ignored — and fails on any that is outside the
#     declared globs, naming it. Refusing was chosen over widening discovery because widening would
#     make this guard's universe differ from the one bun and turbo build from: it would start
#     policing scripts nothing in this repo runs, while leaving the actual defect (an undeclared
#     workspace) in place. Refusing forces the declaration, which fixes both at once — and it
#     covers the workflow `working-directory` variant at the root, with no YAML parsing.
#     Consequence: the guard requires a git work tree, and says so if it does not have one.
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
#   - RUNNER HALF — a branch gated on env that NEITHER package.json nor the synthetic matrix
#     supplies: a workflow step's own `env:` block, or a variable the CI image exports. Closing it
#     would mean parsing workflow YAML, which is the thing this file was burned for twice. Barely
#     reachable (no workflow step that runs the frontend suite sets any env today); if it ever
#     appears, the fix is to move the branch into the package.json script, where it is replayed.
#   - RUNNER HALF — a runner invoked from somewhere OTHER than an apps/web `test*` script. Only
#     those are replayed. Test 8 is the tripwire for the neighbouring case (an apps/web/scripts
#     *.sh that nothing in package.json reaches).
#   - `echo "bun test src/" > run.sh` — an `echo` with no command separator that nonetheless
#     produces a runnable command. Still skipped as a diagnostic, in workflows and in manifest
#     script bodies alike. Adding `>` to the separator list would catch it, and was NOT done: a
#     `echo ... >> "$GITHUB_OUTPUT"` line mentioning bun test would then become a CI-blocking false
#     alarm, and this file's rule is that a false alarm in a repo-wide gate costs more than a
#     barely-reachable false pass. Not present.
#
# CONTRACT:
#   - test 1: the runner list is derived from apps/web/package.json (not hardcoded) and is non-empty;
#   - test 2: the manifest/script universe is enumerated with JSON.parse over the root manifest plus
#     each declared workspace member, `scripts` object only, and is non-empty in all three counts.
#     It additionally (a) proves its own candidate selection against a fixture of bodies that must
#     and must not be selected, (b) fails on any package.json in the tree that the declared
#     `workspaces` globs do not cover, and (c) refuses any script body that reaches bun through a
#     path;
#   - test 3: every runner exits 0 AND produced at least one observed `bun test` — PER (runner,
#     synthetic mode) and PER replayed package.json invocation, not merely somewhere in the
#     aggregate. A mode gutted to `bun run lint; exit 0` used to pass on the aggregate floor alone
#     (observed). At least one apps/web `test*` script must be found that launches a runner, or the
#     replay verified nothing and the test fails;
#   - test 4: every enumerated script whose body invokes `bun test` is EXECUTED and exits 0 AND
#     produced at least one observed `bun test`;
#   - test 5: every observed `bun test`, from either family, passes EXACTLY one --timeout
#     (`--timeout N` or `--timeout=N`), an integer >= 15000. Two flags is a FAILURE, not something
#     the guard resolves: bun honours the last, so `--timeout 30000 --timeout 100` reads as 30s to a
#     human and runs at 100ms. It also fails on any observed command that was handed a `bun test`
#     as an ARGUMENT, since that nested invocation never ran and was never observed;
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

# ── shared textual shapes ──────────────────────────────────────────────────────────────────────
# ONE definition of "this text invokes bun test", used by the manifest candidate selection, the
# nested-invocation refusal and the workflow tripwire. A leading class that excludes only
# [A-Za-z0-9_-] deliberately ALLOWS `/`, `.`, `~`, `(` and `"` before `bun`, so
# `/opt/homebrew/bin/bun test`, `~/.bun/bin/bun test` and `$(bun test)` are all seen. The trailing
# class stops `bun tests/` matching.
BUN_TEST_RE='(^|[^A-Za-z0-9_-])bun[[:space:]]+test([^A-Za-z0-9_-]|$)'
# The same, narrowed to a `bun` reached through a PATH-BEARING name. That is precisely the shape a
# PATH stub CANNOT intercept, so it is refused rather than observed. See "finding 4" in the header.
PATHED_BUN_TEST_RE='[^[:space:]]*/bun[[:space:]]+test([^A-Za-z0-9_-]|$)'

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

# line_is_diagnostic_only <command-text>
# True when the text is nothing but an `echo`/`printf` that prints something. This repo's own
# convention is `echo "::error::bun test failed"`, and flagging that was a CONFIRMED false alarm —
# in workflows AND, as of this round, in package.json scripts, where a `"test:help": "echo 'run bun
# test src/foo'"` was selected as a candidate, executed, produced no argv record and failed the
# guard. The skip is withheld the moment the text also contains a separator, because
# `echo starting && bun test src/` carries a real invocation and an unconditional first-word skip
# would be a fresh false pass. Both directions are pinned by fixtures (tests 2 and 9).
line_is_diagnostic_only() {
    local body="$1" first
    first="${body%%[[:space:]]*}"
    case "$first" in
        echo|printf) ;;
        *) return 1 ;;
    esac
    case "$body" in
        *'&&'*|*'||'*|*';'*|*'|'*|*'$('*|*'`'*) return 1 ;;
    esac
    return 0
}

# body_invokes_bun_test <script-body>
# Candidate selection for the manifest half: does this body have a line that really runs bun test?
# Whole-line comments and pure diagnostics do not count. Over-selection is harmless (the body is
# executed and its argv judged); under-selection is the risk, and is named in the out-of-scope list.
body_invokes_bun_test() {
    local body="$1" line trimmed
    while IFS= read -r line || [ -n "$line" ]; do
        trimmed="${line#"${line%%[![:space:]]*}"}"
        case "$trimmed" in '#'*) continue ;; esac
        line_is_diagnostic_only "$trimmed" && continue
        if printf '%s' "$trimmed" | /usr/bin/grep -qE -- "$BUN_TEST_RE"; then return 0; fi
    done <<< "$body"
    return 1
}

# has_pathed_bun_test <text>
# A `bun test` reached through a path bypasses the PATH stub entirely, so nothing this guard does
# can observe its argv. Refused, never analysed.
has_pathed_bun_test() {
    printf '%s' "$1" | /usr/bin/grep -qE -- "$PATHED_BUN_TEST_RE"
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
const allLines = [];
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
    // Every body is written out. Deciding WHICH bodies are worth executing is no longer done here:
    // it moved to bash, so that the manifest half and the workflow tripwire share ONE definition of
    // "this text invokes bun test" (including the diagnostic-echo skip that this JS lacked).
    idx += 1;
    const tag = String(idx).padStart(4, "0");
    fs.writeFileSync(path.join(out, "body." + tag), body);
    allLines.push(tag + "\t" + rel + "\t" + name);
  }
}

fs.writeFileSync(path.join(out, "manifests.txt"), manLines.join("\n") + (manLines.length ? "\n" : ""));
fs.writeFileSync(path.join(out, "scripts.txt"), scriptLines.join("\n") + (scriptLines.length ? "\n" : ""));
fs.writeFileSync(path.join(out, "all.txt"), allLines.join("\n") + (allLines.length ? "\n" : ""));
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

    universe_problems=""

    # ── candidate selection self-test ──────────────────────────────────────────────────────────
    # Selection decides what test 4 executes, so a selector that silently matched nothing would
    # look exactly like a tree with no bun test in it. Each POSITIVE line below must be selected;
    # each NEGATIVE line must not. The negatives are confirmed false alarms (a diagnostic `echo`
    # mentioning bun test was selected, executed, produced no argv record and failed the guard).
    sel_problems=""
    while IFS= read -r probe; do
        [ -n "$probe" ] || continue
        exp="${probe%%$'\t'*}"
        txt="${probe#*$'\t'}"
        if body_invokes_bun_test "$txt"; then got=yes; else got=no; fi
        [ "$got" = "$exp" ] || sel_problems+="    expected select=$exp got=$got for body: $txt"$'\n'
    done <<'SELFTEST'
yes	bun test --timeout 30000 src/
yes	echo starting && bun test src/
yes	tsc --noEmit && bun test src/
yes	echo done; bun test src/
no	echo 'to debug a single file run bun test src/foo.test.ts'
no	printf '%s\n' "bun test --timeout 0"
no	# bun test src/ was moved into a runner
no	bun run test:integration
no	bunx playwright test --timeout 30000
SELFTEST
    [ -n "$sel_problems" ] && universe_problems+="  candidate selection self-test disagreed with its own fixtures:"$'\n'"$sel_problems"

    # ── every package.json in the tree must be inside the declared workspaces ──────────────────
    # FINDING 6, decided as a REFUSAL rather than as widened discovery. A manifest outside the
    # `workspaces` globs (say a new tools/ci/package.json whose `test` script runs `bun test src/`,
    # invoked from a workflow with a `working-directory`) was previously invisible: enumeration
    # follows `workspaces`, the workflow only says `bun run test`, and the tripwire has nothing to
    # flag. Confirmed by construction: adding exactly that pair left the guard at "9 passed, 0
    # failed". The alternative — enumerating every manifest in the tree and checking it — was
    # rejected because it would make this guard's universe DIFFER from the one bun and turbo build
    # from, i.e. it would start policing scripts that nothing in this repo actually runs, while
    # leaving the real defect (an undeclared workspace) in place. Refusing forces the declaration,
    # which fixes both at once. The list is `git ls-files` over tracked AND untracked-but-not-
    # ignored files, so a manifest added in the working tree is caught before it is ever committed.
    declared=""
    while IFS= read -r m; do
        [ -n "$m" ] && declared+="$m"$'\n'
    done < "$MAN_OUT/manifests.txt"

    if ! (cd "$REPO_ROOT" && git rev-parse --is-inside-work-tree) >/dev/null 2>&1; then
        universe_problems+="    $REPO_ROOT is not a git work tree, so the set of package.json files in it cannot be listed and an undeclared workspace manifest would be invisible."$'\n'
    else
        found_any=0
        while IFS= read -r m; do
            [ -n "$m" ] || continue
            case "$m" in */node_modules/*|node_modules/*) continue ;; esac
            found_any=1
            if ! printf '%s' "$declared" | /usr/bin/grep -qxF -- "$m"; then
                universe_problems+="    $m is a package.json this guard never enumerated, because it is not the root manifest and not inside any glob declared in the root 'workspaces' ($(tr '\n' ' ' < "$MAN_OUT/manifests.txt")). Its scripts are therefore unchecked, and a workflow step with a 'working-directory' pointing at it could run 'bun run test' at bun's 5000ms default with nothing noticing. Declare it in the root package.json 'workspaces' array (which also makes bun and turbo see it), or delete it."$'\n'
            fi
        done < <(cd "$REPO_ROOT" && git ls-files --cached --others --exclude-standard | /usr/bin/grep -E '(^|/)package\.json$')
        if [ "$found_any" -eq 0 ]; then
            universe_problems+="    'git ls-files' listed ZERO package.json files under $REPO_ROOT. This repo has several, so that is a broken listing, not a clean tree."$'\n'
        fi
    fi

    # ── candidate selection proper, plus the pathed-bun refusal ────────────────────────────────
    while IFS= read -r c; do
        [ -n "$c" ] || continue
        c_tag="${c%%$'\t'*}"
        c_rest="${c#*$'\t'}"
        c_man="${c_rest%%$'\t'*}"
        c_name="${c_rest#*$'\t'}"
        c_body=$(/bin/cat "$MAN_OUT/body.$c_tag")
        # FINDING 4. A `bun` reached through a path is not the PATH stub, so executing this body
        # would record nothing for it — and if any SIBLING invocation in the same body produced a
        # good record, the run looks clean. (The header used to claim this "fails closed"; it does
        # so only in isolation. Observed with `bun test --timeout 30000 src/ && /tmp/redbun/bun test
        # src/`: 9 passed, 0 failed.) Refused instead of analysed.
        if has_pathed_bun_test "$c_body"; then
            universe_problems+="    $c_man scripts.$c_name invokes 'bun test' through a PATH-BEARING name. This guard observes argv by putting a recording 'bun' on PATH, which such an invocation bypasses entirely, so its --timeout can never be verified here - and a sibling invocation on the same line would make the run look clean. Call it as plain 'bun'. Body: $c_body"$'\n'
            continue
        fi
        body_invokes_bun_test "$c_body" && CANDS+=("$c")
    done < "$MAN_OUT/all.txt"

    if [ -n "$universe_problems" ]; then
        bad "the manifest/script universe cannot be trusted:"
        printf '%s' "$universe_problems"
    elif [ "$man_count" -lt 2 ] || [ "$script_count" -lt 5 ] || [ "${#CANDS[@]}" -lt 2 ]; then
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
#
# ⚠ THE SEQUENCE NUMBER MUST BE CLAIMED ATOMICALLY. It used to be a read-increment-write of a
# `.seq` file, which is a lost-update race the moment two stubs run at once — and they do, because
# a script body may background its commands (`bun test a & bun test b & wait`). Measured on this
# machine: 24 concurrent stub invocations against the `.seq` version produced EIGHT records, and an
# end-to-end run with a backgrounded `bun test src/` (no --timeout) alongside twelve good ones let
# the guard report "9 passed, 0 failed" on one run in three. Losing the BAD record is a false pass,
# so the claim is now `set -C` + `>`, i.e. open(O_CREAT|O_EXCL) — atomic, and a bash builtin, which
# matters because the manifest family runs under a PATH containing nothing but this stub directory.
cat > "$WORK/.record" <<'STUB'
#!/bin/bash
set -u
_name=${0##*/}
_i=0
while :; do
    _i=$((_i + 1))
    printf -v _f '%s/rec.%06d' "$BB_ARGV_DIR" "$_i"
    if ( set -C; : > "$_f" ) 2>/dev/null; then break; fi
done
printf '%s\0' "$_name" "$@" > "$_f"
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
nested_problems=""
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

    # FINDING 3. Every command name in the stub set is a no-op that RECORDS AND RETURNS — it does
    # not run whatever it was handed. So `npx --yes concurrently "bun test src/"` records `npx`,
    # the nested `bun test` never executes, and if the same body also contains one good invocation
    # the whole run reads clean (observed: 9 passed, 0 failed with exactly that body in
    # apps/mcp-server's `test` script). Executing the nested command for real is not an option —
    # the stubs exist precisely so nothing real runs — so this shape is REFUSED by name.
    local ai
    for (( ai = 1; ai < n; ai++ )); do
        if printf '%s' "${args[ai]}" | /usr/bin/grep -qE -- "$BUN_TEST_RE"; then
            nested_problems+="    $label: '$cmd' was handed a nested 'bun test' as the argument '${args[ai]}'. This guard stubs '$cmd' to record its argv and return without running anything, so that nested invocation never executed and its --timeout was never observed - and a sibling invocation in the same body would make the run look clean. Invoke bun test directly instead of wrapping it in another command."$'\n'
            return 0
        fi
    done

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
    # `for r in "${recs[@]}"` on an EMPTY array is an unbound-variable error under `set -u`, which
    # killed the whole guard mid-run (observed: "line 507: recs[@]: unbound variable", exit 1, tests
    # 5-9 never executed) the first time an executed body produced no records at all. That also made
    # test 3's own zero-record branch unreachable dead code.
    for r in ${recs[@]+"${recs[@]}"}; do
        k=$((k + 1))
        check_record "$r" "$label invocation #$k" "$man_rel"
    done
}

# ── Test 3: execute every runner, in every mode ────────────────────────────────────────────────
echo "Test 3: each runner executes under a recording 'bun' - both synthetically and exactly as package.json invokes it"

vacuity_problems=""
run_index=0

# FINDING 4, runner half. A runner that reaches bun through a path is not intercepted by the PATH
# stub, so its argv can never be observed; refused rather than analysed. (Test 8 covers the *.sh
# files this loop does NOT execute.)
for rel in "${RUNNERS[@]}"; do
    if has_pathed_bun_test "$(/bin/cat "$WEB_DIR/$rel")"; then
        vacuity_problems+="    $rel: invokes 'bun test' through a PATH-BEARING name. This guard observes argv by putting a recording 'bun' on PATH, which such an invocation bypasses entirely, so its --timeout can never be verified - and a sibling invocation elsewhere in the same run would make the result look clean. Call it as plain 'bun'."$'\n'
    fi
done

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

# ── Test 3, phase B: replay the invocations package.json ACTUALLY performs ─────────────────────
#
# FINDINGS 1 AND 2. The loop above invokes each runner DIRECTLY, with a hardcoded mode matrix of ""
# and "--coverage". That is a guess about how the runner is called, and both halves of the guess
# were falsifiable:
#
#   - ENV. `"test:ci": "NODE_ENV=production bash scripts/test-regular.sh --coverage && ..."` sets
#     env the direct invocation never sets, so a runner branch gated on it is invisible. Observed
#     with a `BB_FAST` branch that dropped --timeout: guard 9 passed, 0 failed, while `bun run
#     test:ci` — what CI runs — took the flagless path.
#   - ARGUMENT SHAPE. `--coverage=lcov` is not `--coverage`. Observed with a `--coverage=*` branch
#     that dropped --timeout: guard 9 passed, 0 failed.
#
# Neither is fixable by adding more guessed modes; the fix is to stop guessing. Each apps/web
# `test*` script whose body launches a runner is now EXECUTED AS ITS OWN BODY — same cwd, same env
# prefixes, same argument shapes, same `&&` chaining — under the recording stub. Whatever env or
# argument branch CI takes is therefore the branch observed here, by construction. The synthetic
# matrix above is kept as an ADDITIONAL per-runner floor: it is what catches a runner that stops
# running tests in a mode nothing currently uses.
#
# This half runs under PATH="$SHIM_DIR:$PATH" (not the hermetic PATH), because a runner needs real
# find/grep/sed/wc to do its work; `bash` is deliberately NOT stubbed so the runner really runs.
invoker_count=0
if [ -f "$MAN_OUT/all.txt" ]; then
    while IFS= read -r c; do
        [ -n "$c" ] || continue
        i_tag="${c%%$'\t'*}"
        i_rest="${c#*$'\t'}"
        i_man="${i_rest%%$'\t'*}"
        i_name="${i_rest#*$'\t'}"
        [ "$i_man" = "apps/web/package.json" ] || continue
        case "$i_name" in test*) ;; *) continue ;; esac
        case "$i_name" in *e2e*) continue ;; esac
        i_body=$(/bin/cat "$MAN_OUT/body.$i_tag")

        launches=0
        for rel in "${RUNNERS[@]}"; do
            case "$i_body" in *"$rel"*) launches=1 ;; esac
        done
        [ "$launches" -eq 1 ] || continue

        invoker_count=$((invoker_count + 1))
        run_index=$((run_index + 1))
        inv_dir="$WORK/inv.$i_tag"
        mkdir -p "$inv_dir"
        ilog="$inv_dir/output.log"

        ( cd "$WEB_DIR" && BB_ARGV_DIR="$inv_dir" PATH="$SHIM_DIR:$PATH" /bin/bash -c "$i_body" ) >"$ilog" 2>&1
        irc=$?

        ctx_test_invocations=0
        collect_records "$inv_dir" "$i_man scripts.$i_name (as package.json invokes it)" "$i_man"

        if [ "$irc" -ne 0 ]; then
            vacuity_problems+="    $i_man scripts.$i_name: executing the body the way package.json does exited $irc, so its argv cannot be trusted. Body: $i_body -- last output: $(tail -3 "$ilog" | tr '\n' ' ')"$'\n'
        elif [ "$ctx_test_invocations" -eq 0 ]; then
            vacuity_problems+="    $i_man scripts.$i_name: launches a runner but produced $REC_COUNT argv record(s) and NOT ONE was a 'bun test'. This is the invocation CI performs, so nothing about its timeout was verified. Body: $i_body"$'\n'
        fi
    done < "$MAN_OUT/all.txt"
fi

if [ "$invoker_count" -eq 0 ]; then
    vacuity_problems+="    no apps/web 'test*' script was found that launches any of the discovered runner(s) (${RUNNERS[*]}). The runners are then only ever invoked with this guard's GUESSED modes, and an env-gated or argument-gated branch that CI takes would be invisible. Either package.json stopped launching them, or this matching is broken."$'\n'
fi

if [ -n "$vacuity_problems" ]; then
    bad "runner executions that cannot be trusted:"
    printf '%s' "$vacuity_problems"
elif [ "$test_invocations" -lt 2 ]; then
    bad "only $test_invocations 'bun test' invocation(s) observed across $run_index runner execution(s) - both runners are expected to run bun test, so this is a broken observation, not a clean one"
else
    ok "$run_index runner execution(s) all exited 0 and each reached 'bun test' ($invoker_count of them replaying package.json's own invocation, env prefixes and argument shapes included); $test_invocations invocation(s) observed"
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
echo "Test 5: every observed 'bun test' passes exactly one --timeout >= $TIMEOUT_FLOOR, and none is hidden inside another command"
if [ "$test_invocations" -eq 0 ]; then
    bad "no 'bun test' invocation was observed anywhere, so there is nothing to check - treated as a failure, not a pass"
elif [ -n "$nested_problems" ]; then
    bad "'bun test' handed to another command as an argument, so its argv could never be observed:"
    printf '%s' "$nested_problems"
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

# The shape is $BUN_TEST_RE, defined once at the top and shared with the manifest half. Excluding
# only [A-Za-z0-9_-] before `bun` deliberately ALLOWS `/`, `.`, `~`, `(`, `"`, so
# `/opt/homebrew/bin/bun test`, `~/.bun/bin/bun test` and `$(bun test)` are all seen. The old class
# excluded `/`, `.` and `-`, which made every pathed invocation invisible (observed: guard exit 0
# with `/opt/homebrew/bin/bun test src/` in a workflow).
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
        line_is_diagnostic_only "$body" && continue

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

        if printf '%s' "$body" | /usr/bin/grep -qE -- "$BUN_TEST_RE"; then
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
