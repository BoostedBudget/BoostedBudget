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
#   - test 4: every observed `bun run test*` delegation RESOLVES, in apps/web/package.json, to a
#     `bun test` that passes test 3's rules - followed with the same scanner test 8 uses, so the two
#     halves of this guard cannot disagree about whether a delegation is checkable. Only a
#     delegation that genuinely cannot be resolved (no such script, a cycle) is a failure;
#   - test 5: the SMALLEST observed value demonstrably survives a 5.6s test under real bun. A missing
#     `bun` binary is a hard FAIL, never a skip;
#   - test 6: no *.sh under apps/web/scripts that is NOT executed here contains a `bun test` at all,
#     once shell comments are stripped — that is the only place a bare substring search survives, and
#     only as a "you added an unverifiable runner" tripwire whose failure direction is a false ALARM,
#     never a false pass. Comments are excluded because the alarm gates CI for the whole repo;
#   - test 7: no `run:` block in any .github/workflows/*.yml OR *.yaml executes a `bun test` without
#     exactly one adequate --timeout;
#   - test 8: no `scripts` entry in any of the repo's OWN manifests — the root package.json plus the
#     workspace members it declares, never a generated or fixture package.json — does either,
#     following `bun run <name>` delegations within the same manifest rather than flagging them;
#   - test 9: tests 7 and 8's scanner is itself exercised against SYNTHETIC FIXTURES, one per known
#     failure mode, each asserted to be flagged (or, for the negative controls, left clean).
#
# The floor is 15000, not "above bun's 5000 default": `--timeout 6000` would clear both a >5000 check
# and the 5.6s probe, yet the CI failure that started this took 9910ms. The exact value 30000 is
# deliberately NOT pinned — that would fail a legitimate tuning change.
#
# SCOPE LIMIT: apps/web/package.json's `test:all`, `test:watch` and `test:integration` run `bun test`
# directly, outside apps/web/scripts. (They carried no --timeout when this note was written; they do
# now, and test 8 is what keeps them that way.) Nothing runs them: `make test-fast`,
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
# Tests 7-9 now cover every place a `bun test` command line is WRITTEN DOWN rather than executed —
# workflow `run:` blocks and package.json `scripts` — because the defect is "a bun suite running at
# the default", not "an apps/web script missing a flag".
#
# ═══ WHY TESTS 7-9 ARE ALLOWED TO PARSE TEXT, AND HOW THEY ARE KEPT HONEST ═════════════════════
#
# The observe-don't-parse design above cannot reach these two surfaces: a workflow step is executed
# by GitHub, and a package.json script by bun's own runner, so there is no runner script this guard
# can invoke under a PATH shim to watch the argv. Reading the text is the only option left. The
# first attempt at that (#1537 round 17, test 7 as originally written) shipped FIVE defects, all
# adversarially found, ALL of them false PASSES over a suite really running at 100ms or 5000ms:
#
#   1. it globbed `*.yml` only, so deploy-to-staging.yaml — a real deploy workflow — was never read;
#   2. it read one invocation per PHYSICAL LINE, so `bun test --timeout 30000 && bun test src/` was
#      green on the strength of the first command while the second ran at the 5s default;
#   3. it never saw a `run: |` block whose command was continued with `bun \` + newline, nor a
#      folded scalar (`>`) that put `bun` and `test` on separate source lines;
#   4. its greedy `sed -n 's/.*--timeout[ =]*\([0-9]*\).*/\1/p'` happily read the number out of a
#      trailing COMMENT: `bun test --timeout 100 # --timeout 30000` reported 30000 and passed;
#   5. it did not look at package.json at all, where three apps/web scripts and two apps/mcp-server
#      scripts were running `bun test` with no flag whatsoever.
#
# Every one of those five shipped for exactly one reason: the check only ever ran against the live
# repository, so its FAILURE MODES were never exercised. Nothing in the suite could tell "the tree
# is clean" apart from "the scanner is blind". That is what test 9 fixes, and it is the structural
# lesson, not the five individual bugs: a text scanner without fixtures is an untested function.
#
# The scanner is therefore built to shrink the parsing surface as far as it can go, and to fail
# LOUDLY wherever it cannot:
#
#   - YAML is parsed by `Bun.YAML.parse`, never by a line regex. Block scalars, folded scalars and
#     multi-line quoted scalars arrive already resolved into one shell snippet, which is what makes
#     defects 1/3 unrepeatable by construction rather than by another regex. JSON likewise via
#     JSON.parse. (The same reasoning as test 1, which parses package.json with bun, not a regex.)
#   - comment stripping tracks quote state across the WHOLE snippet, not per line — the exact bug
#     that produced a demonstrated false pass in rounds 1-9 — and treats `#` as a comment only when
#     it is unquoted AND at start-of-text or preceded by whitespace, so `${VAR#refs/heads/}` and
#     `-t "issue #42"` are left intact. Both directions have fixtures in test 9.
#   - a `--timeout` value written as `$VAR` is resolved ONLY against a literal `VAR=<integer>`
#     assignment appearing earlier in the same snippet, and any value that cannot be resolved that
#     way (`${{ env.T }}`, `$1`, `${T:-x}`, a value from the outer shell) is REFUSED, not assumed
#     adequate. The blanket refusal that came before was fail-closed but fired on correct code;
#     resolving one narrow, checkable shape is not the same as trusting variables.
#   - an unterminated quote is reported rather than parsed through, and an unparseable workflow or
#     package.json is a FINDING, not a skipped file.
#   - the timeout rules themselves (exactly one flag, integer, >= floor) are the same rules
#     check_record() applies to observed argv, so the two halves of this guard cannot drift apart.
#
# WHAT TESTS 7-9 STILL DO NOT SEE, stated plainly rather than left to be discovered:
#   - a `bun test` assembled at runtime — `eval`, a command built by string concatenation, or a
#     command living in a variable (`CMD="bun test"; $CMD`). A text scanner cannot execute the
#     shell, and a full shell interpreter is out of scope; that route is instead closed by policy:
#     workflows and package.json scripts write their bun command lines literally;
#   - a `bun test` inside a *script file* invoked by a workflow. apps/web/scripts is covered by
#     tests 2-6 (executed) and test 6 (tripwire); a script under any OTHER directory is not covered
#     by this file at all;
#   - which package.json a workflow's `bun run test` resolves to. It is not resolved, and does not
#     need to be: test 8 scans EVERY package.json in the repo, so the delegate's body is checked
#     wherever it lives. A package.json outside the repo (an action's own) is out of reach;
#   - workflow surfaces other than `run:` — a `with: args:` handed to a container action, a `bun
#     test` inside an `env:` default. None exist today; if one appears it is invisible here;
#   - a heredoc BODY is skipped entirely (test.yml feeds python programs that way, and parsing them
#     as shell produced false alarms). So a `bun test` piped INTO a shell — `bash <<EOF ... EOF` —
#     is not seen. No such pattern exists in this repo; the fixture `heredoc-resumes` at least pins
#     that a command after the terminator is still read, which is the failure that would matter most.
#
# ═══ ROUND 18: SIX FALSE ALARMS AND TWO VACUOUS FIXTURES ═══════════════════════════════════════
#
# Round 17 shipped a scanner that failed closed everywhere it was unsure. An adversarial review then
# demonstrated that four of those closed doors shut on CORRECT code — and this guard gates CI for the
# whole repository, so a false alarm is the worse failure direction here, not the safer one:
#
#   1. test 6's tripwire was a raw substring search, so a helper documenting `# Example command for
#      contributors: bun test src/` blocked CI over a comment. Comments are stripped first now.
#   2. `TIMEOUT=30000; bun test --timeout "$TIMEOUT" src/` — correct, working code — was refused
#      because the value was not a literal. A same-snippet literal assignment is resolved now.
#   3. EVERY YAML key named `run` was scanned as shell, so a `with: { run: ... }` action INPUT, which
#      GitHub never executes, was read as a command. Only a `steps[]` entry's own `run` is shell now.
#   4. package.json discovery was `find -name package.json` minus node_modules, so a generated
#      dist/package.json or a coverage/ artefact would have failed CI. The list is now the root
#      manifest plus the workspace members it declares — the repo's own packages, nothing else.
#
# The review also found the guard disagreeing with itself, and one real gap:
#
#   5. an OBSERVED `bun run test:unit` was an automatic failure while the package.json scanner
#      RESOLVED the identical delegation and judged the delegate. Both follow it now.
#   6. `if bun test src/; then echo ok; fi` reported COUNT 0 — a real invocation at bun's 5s default,
#      silently unflagged — because head()'s keyword list had `then`/`do` but not `if`/`while`/`until`.
#
# And two fixtures were VACUOUS, which matters more than any single defect above, because a vacuous
# fixture is how every earlier round shipped: `inline-comment` and `two-timeout-flags` both still
# passed with the defence they name deleted, each rescued by a different branch. `inline-comment` had
# a second problem worth remembering — its `#` sat in a PLAIN YAML scalar, so `Bun.YAML.parse`
# deleted the comment before the shell stripper ever saw it. A fixture for the SHELL comment stripper
# must use a block scalar. Both were rewritten until their own mutant failed.
#
# One more thing worth stating, because it is the difference between this round and the last: the
# fixtures in test 9 were each MUTATION-TESTED — the corresponding defence in the scanner was broken
# on purpose and the fixture confirmed to go red. Four of the first drafts did NOT (a fixture whose
# `${VAR#pat}` sat on its own harmless line; a delegation fixture that the direct scan flagged
# anyway; a heredoc body whose hazards were safely double-quoted; a below-floor case that was also
# below bun's default). Each was rewritten until its mutant failed. Asserting an expectation is not
# the same as proving the assertion can fail — do the mutation before you believe a new fixture.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEB_DIR="$REPO_ROOT/apps/web"
TIMEOUT_FLOOR=15000
# The manifest an observed `bun run <script>` delegation is resolved against: the runners under
# apps/web/scripts run with apps/web as their package root. Overridden by the `argv` fixtures in
# test 9 so they can exercise check_record() against a synthetic manifest.
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
# THE TEXT SCANNER, used by tests 4, 6, 7, 8 and 9.
#
# Read the "WHY TESTS 7-9 ARE ALLOWED TO PARSE TEXT" section at the top of this file before
# changing anything below. Short version: workflow `run:` blocks and package.json `scripts` cannot
# be observed under a PATH shim the way the runners above are, so they are read as text — but the
# YAML/JSON layer is handled by a real parser, the shell layer fails closed, and every failure mode
# has a fixture in test 9.
#
# It is ONE program used five ways so that a fix can never apply to the live scan but not to the
# fixtures, or the other way round:
#   workflows <dir>              tests 7 and 9 - every steps[].run in every workflow in <dir>
#   pkg <file>...                tests 8 and 9 - every scripts.* value in each manifest
#   pkgscript <file> <name>      test 4 - ONE script, so an OBSERVED `bun run <name>` delegation is
#                                resolved by exactly the same code that resolves it in test 8
#   strip <file>                 test 6 - the file with shell comments removed, quote-aware
#   discover <root>              test 8 - which package.json files are the repo's own manifests
#
# It sits here, above the recording stub, because check_record() (test 2/4) needs `pkgscript`.
#
# Output protocol (tab-separated, one record per line):
#   FILES\t<n>\t<names>   what was actually read - the anti-vacuity signal
#   COUNT\t<n>            how many `bun test` command lines were examined
#   MIN\t<n>              the smallest ADEQUATE --timeout seen, for the effect probe in test 5
#   FINDING\t<label>\t<why>  a problem (this is what fails a test)
#   INFO\t<label>\t<why>     reader context only, never a failure
#   ERROR\t<label>\t<why>    the scanner could not run; exit 1
# `strip` and `discover` print raw text instead, since their output is not a report.
# ---------------------------------------------------------------------------------------------
SCANNER="$WORK/scan.mjs"
cat > "$SCANNER" <<'SCANJS'
// bun-run scanner: find `bun test` invocations in GitHub workflow `run:` blocks and in
// package.json `scripts` values, and report any that lack an adequate --timeout.
const FLOOR = Number(process.env.BB_TIMEOUT_FLOOR || "15000");
const out = [];
const emit = (kind, label, msg) => out.push(`${kind}\t${label}\t${msg}`);
let examined = 0;
let minAccepted = null;

// --- shell text -> command list -------------------------------------------------------------
// Strips shell comments and joins `\`-newline continuations. Quote state is tracked across the
// WHOLE snippet (not per line): the historical per-line stripper in this guard's rounds 1-9 reset
// quote state at every newline, which let a line starting with `#` inside a multiline double-quoted
// argument be read as a comment. A `#` is a comment ONLY when unquoted AND at start-of-text or
// preceded by whitespace, so `${VAR#pat}` and `file#frag` survive.
function strip(src, skipHeredocBodies = true) {
  let o = "", i = 0, q = null, prevWs = true;
  while (i < src.length) {
    const c = src[i];
    // A HEREDOC BODY IS NOT SHELL. .github/workflows/test.yml embeds python programs via
    // `python3 <<'PY' ... PY`, and those bodies contain both the words "bun test" (in an error
    // message) and unbalanced apostrophes. Parsing them as commands produces false ALARMS, not
    // false passes, but a guard that cries wolf as the workflow grows gets deleted. So the body is
    // skipped wholesale and scanning resumes after the terminator line. `<<<` (a here-string) is
    // ordinary command text and is left alone. Mode `strip` (test 6's tripwire) passes
    // skipHeredocBodies=false: that check only wants comments gone, and must not go quiet on a
    // `bun test` written inside a heredoc.
    if (skipHeredocBodies && !q && c === "<" && src[i + 1] === "<" && src[i + 2] !== "<") {
      let j = i + 2;
      if (src[j] === "-") j++;
      while (src[j] === " " || src[j] === "\t") j++;
      let dq = null, delim = "";
      if (src[j] === "'" || src[j] === '"') { dq = src[j]; j++; }
      while (j < src.length && (dq ? src[j] !== dq : /[A-Za-z0-9_]/.test(src[j]))) { delim += src[j]; j++; }
      if (dq) j++;
      if (delim !== "") {
        // Emit the rest of the current line (the command itself), then drop lines up to the
        // terminator. An UNTERMINATED heredoc drops the remainder, which is the fail-closed
        // direction here: unread text cannot be reported clean, and test 8's COUNT would notice.
        while (j < src.length && src[j] !== "\n") { o += src[j]; j++; }
        o += "\n";
        j++;
        while (j < src.length) {
          let e = src.indexOf("\n", j);
          if (e === -1) e = src.length;
          if (src.slice(j, e).trim() === delim) { j = e + 1; break; }
          j = e + 1;
        }
        i = j; prevWs = true; continue;
      }
    }
    if (q === "'") { if (c === "'") q = null; o += c; i++; prevWs = false; continue; }
    if (q === '"') {
      if (c === "\\" && i + 1 < src.length) { o += c + src[i + 1]; i += 2; prevWs = false; continue; }
      if (c === '"') q = null;
      o += c; i++; prevWs = false; continue;
    }
    if (c === "\\" && src[i + 1] === "\n") { o += " "; i += 2; prevWs = true; continue; }
    if (c === "\\" && i + 1 < src.length) { o += c + src[i + 1]; i += 2; prevWs = false; continue; }
    if (c === "'" || c === '"') { q = c; o += c; i++; prevWs = false; continue; }
    if (c === "#" && prevWs) { while (i < src.length && src[i] !== "\n") i++; continue; }
    o += c; prevWs = c === " " || c === "\t" || c === "\n"; i++;
  }
  return { text: o, unterminated: q !== null };
}

// Splits on unquoted shell separators. `&&`/`||` fall out of splitting on single `&`/`|`.
function commands(text) {
  const res = []; let cur = "", q = null;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (q) { if (c === "\\" && q === '"') { cur += c + text[++i]; continue; } if (c === q) q = null; cur += c; continue; }
    if (c === "\\") { cur += c + text[++i]; continue; }
    if (c === "'" || c === '"') { q = c; cur += c; continue; }
    // NOT split on `{`/`}`: a GitHub Actions `${{ ... }}` expression would be torn apart, and
    // shell brace grouping is already separated by the `;` inside it. `head()` skips a stray brace.
    if (c === "\n" || c === ";" || c === "&" || c === "|" || c === "(" || c === ")") { res.push(cur); cur = ""; continue; }
    cur += c;
  }
  res.push(cur);
  return res.filter((s) => s.trim() !== "");
}

function words(cmd) {
  const res = []; let cur = null, q = null;
  for (let i = 0; i < cmd.length; i++) {
    const c = cmd[i];
    if (q) { if (c === "\\" && q === '"') { cur += cmd[++i]; continue; } if (c === q) { q = null; continue; } cur = (cur ?? "") + c; continue; }
    if (c === "\\") { cur = (cur ?? "") + (cmd[++i] ?? ""); continue; }
    if (c === "'" || c === '"') { q = c; cur = cur ?? ""; continue; }
    if (c === " " || c === "\t") { if (cur !== null) { res.push(cur); cur = null; } continue; }
    cur = (cur ?? "") + c;
  }
  if (cur !== null) res.push(cur);
  return res;
}

// Drop leading `VAR=val` assignments and transparent wrappers so `CI=1 env bun test` is still seen.
// DEFECT 6 (#1537 round 18): this list was missing the reserved words that OPEN a compound
// command, so `if bun test src/; then echo ok; fi` reported COUNT 0 - a real, unflagged invocation.
// The binary operators (`&&`, `||`, `;`, `(`, `)`) are already handled by commands() splitting;
// what is left for head() to skip is the reserved words, the braces and the negation.
const WRAPPERS = new Set(["env", "time", "exec", "nice", "nohup", "command", "sudo",
  "{", "}", "!", "if", "while", "until", "then", "else", "elif", "do", "fi", "done"]);
function head(ws) {
  let i = 0;
  while (i < ws.length && (/^[A-Za-z_][A-Za-z0-9_]*=/.test(ws[i]) || WRAPPERS.has(ws[i]))) i++;
  return i;
}
const isBun = (w) => w === "bun" || /(^|\/)bun$/.test(w);

// DEFECT 2 (#1537 round 18): refusing EVERY non-literal value was a deliberate fail-closed choice
// that fired on correct code - `TIMEOUT=30000; bun test --timeout "$TIMEOUT" src/` is a legitimate
// script and was rejected. A `$VAR` is now resolved against the literal assignments seen earlier in
// the SAME snippet, and only that. Everything else is still refused rather than assumed adequate:
// `${{ env.X }}`, `$1`, `${X:-y}`, a value from the outer shell, or an assignment whose own value is
// not a literal integer. Resolving one narrow, checkable shape is not the same as trusting variables.
function resolveValue(val, env) {
  if (/^[0-9]+$/.test(val)) return val;
  const m = /^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$/.exec(val);
  if (m && env && env.has(m[1]) && /^[0-9]+$/.test(env.get(m[1]))) return env.get(m[1]);
  return null;
}

// Exactly one --timeout, resolving to an integer >= FLOOR. Mirrors check_record()'s argv rules so
// the text scan and the observe-the-argv scan cannot disagree about what "adequate" means. Returns
// { problem } when the invocation is bad, { problem: null, value } when it is good - the value feeds
// MIN, so test 5 can probe the smallest timeout this scan actually accepted.
function timeoutVerdict(args, env) {
  let n = 0, val = null, set = false;
  const no = (msg) => ({ problem: msg });
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--timeout") { n++; if (i + 1 < args.length) { val = args[i + 1]; set = true; } else { set = false; } }
    else if (args[i].startsWith("--timeout=")) { n++; val = args[i].slice(10); set = true; }
  }
  if (n === 0) return no(`runs with NO --timeout, so it runs at bun's 5000ms default (the CI failure this guard exists for took 9910ms)`);
  if (n > 1) return no(`passes ${n} --timeout flags; bun honours the LAST, so a large leading value can mask a tiny trailing one. Pass exactly one`);
  if (!set || val === "") return no(`passes --timeout with no value after it`);
  const lit = resolveValue(val, env);
  if (lit === null) return no(`passes --timeout '${val}', which this scan cannot resolve to a literal integer (there is no earlier NAME=<integer> assignment for it in the same script), so it is refused rather than assumed adequate`);
  if (Number(lit) < FLOOR) return no(`passes --timeout ${val === lit ? val : `${val} (= ${lit})`}, below the ${FLOOR}ms floor`);
  return { problem: null, value: Number(lit) };
}

// Scans one shell snippet. `resolve(name)` is non-null when `bun run <name>` can be followed
// (package.json mode); it returns the delegate's snippet or undefined if the script is missing.
function scanSnippet(snippet, label, resolve, seen) {
  const s = strip(snippet);
  if (s.unterminated) emit("FINDING", label, "has an unterminated quote, so this scanner cannot tell command text from string contents; it refuses to report it clean");
  // Literal `NAME=value` assignments seen SO FAR in this snippet, in source order, for
  // resolveValue(). Built as we walk the commands, so a later reassignment cannot retroactively
  // excuse an earlier invocation, and an assignment after the invocation is not visible to it.
  const env = new Map();
  for (const cmd of commands(s.text)) {
    const ws = words(cmd);
    for (let a = 0; a < ws.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(ws[a]); a++) {
      const eq = ws[a].indexOf("=");
      env.set(ws[a].slice(0, eq), ws[a].slice(eq + 1));
    }
    const h = head(ws);
    if (!(ws.length > h && isBun(ws[h]))) continue;
    const verb = ws[h + 1];
    if (verb === "test") {
      examined++;
      const v = timeoutVerdict(ws.slice(h + 2), env);
      if (v.problem) emit("FINDING", label, `\`${cmd.trim()}\` ${v.problem}`);
      else if (minAccepted === null || v.value < minAccepted) minAccepted = v.value;
      continue;
    }
    if (verb === "run") {
      const target = ws[h + 2];
      if (target === undefined) continue;
      // `bun run ./x.ts` executes a FILE, not a package.json script. Only a bare name can be one.
      if (/[/\\]/.test(target) || /\.(ts|tsx|js|jsx|mjs|cjs)$/.test(target)) continue;
      if (!resolve) {
        // Workflow mode: which package.json a `bun run` resolves to depends on the step's
        // working-directory, so it is not resolved here. It does not need to be: test 8 scans
        // EVERY package.json in the repo, so the delegate's own body is checked there regardless
        // of who invokes it. Only test* targets are worth reporting, and only for the reader.
        if (target.startsWith("test")) emit("INFO", label, `delegates to \`bun run ${target}\`; the delegate's own body is checked by the package.json scan`);
        continue;
      }
      if (seen.has(target)) { emit("FINDING", label, `\`bun run ${target}\` is a delegation cycle, so its timeout cannot be resolved`); continue; }
      const next = resolve(target);
      if (next === undefined) { emit("FINDING", label, `\`bun run ${target}\` delegates to a script that is not defined in this package.json, so its timeout cannot be resolved`); continue; }
      scanSnippet(next, `${label} -> ${target}`, resolve, new Set([...seen, target]));
    }
  }
}

// --- run-block extraction --------------------------------------------------------------------
// The `run` of a STEP: an element of a `steps` array, which covers jobs.*.steps[].run and the
// `runs.steps[].run` of a composite action, at any depth. Using the YAML PARSER (not a line regex)
// is what makes block scalars (`|`), folded scalars (`>`, which fold a newline between `bun` and
// `test` into a space) and multi-line quoted scalars visible at all.
//
// DEFECT 3 (#1537 round 18): this used to take EVERY key named `run` at any depth, which is not the
// same thing. `with: { run: ... }` is an INPUT to some action - arbitrary data that GitHub never
// executes as shell - and scanning it produced a false alarm on correct workflow YAML. Only a step's
// own `run` is a shell snippet, so only a step's own `run` is read. The `isStep` flag is set when
// descending into a key literally named `steps` and cleared by any other key, so a `run` nested
// under a step's `with:`/`env:` is data again.
function runBlocks(node, path, sink, isStep) {
  if (Array.isArray(node)) { node.forEach((v, i) => runBlocks(v, `${path}[${i}]`, sink, isStep)); return; }
  if (node && typeof node === "object") {
    for (const [k, v] of Object.entries(node)) {
      if (k === "run" && typeof v === "string" && isStep) sink.push({ path: `${path}.run`, text: v });
      else runBlocks(v, `${path}.${k}`, sink, k === "steps");
    }
  }
}

const [mode, ...paths] = process.argv.slice(2);
let raw = null; // set by the modes whose output is text, not a report
try {
  if (mode === "workflows") {
    const dir = paths[0];
    const { readdirSync } = await import("node:fs");
    const files = readdirSync(dir).filter((f) => f.endsWith(".yml") || f.endsWith(".yaml")).sort();
    if (files.length === 0) { console.log(`ERROR\t${dir}\tno *.yml or *.yaml files found`); process.exit(1); }
    console.log(`FILES\t${files.length}\t${files.join(" ")}`);
    for (const f of files) {
      const p = `${dir}/${f}`;
      let doc;
      try { doc = Bun.YAML.parse(await Bun.file(p).text()); }
      catch (e) { emit("FINDING", f, `is not parseable YAML (${e.message}), so its run blocks cannot be inspected`); continue; }
      const sink = [];
      for (const d of Array.isArray(doc) ? doc : [doc]) runBlocks(d, "", sink, false);
      for (const b of sink) scanSnippet(b.text, `${f}${b.path}`, null, new Set());
    }
  } else if (mode === "pkg") {
    if (paths.length === 0) { console.log("ERROR\t-\tno package.json paths given"); process.exit(1); }
    console.log(`FILES\t${paths.length}\t${paths.join(" ")}`);
    for (const p of paths) {
      let scripts;
      try { scripts = (JSON.parse(await Bun.file(p).text()).scripts) || {}; }
      catch (e) { emit("FINDING", p, `is not parseable JSON (${e.message}), so its scripts cannot be inspected`); continue; }
      const resolve = (name) => scripts[name];
      for (const [name, body] of Object.entries(scripts)) {
        if (typeof body !== "string") continue;
        scanSnippet(body, `${p} scripts.${name}`, resolve, new Set([name]));
      }
    }
  } else if (mode === "pkgscript") {
    // ONE named script, for test 4. An observed `bun run test:unit` is resolved by exactly this
    // code path, so the argv half of the guard and the package.json half cannot disagree about
    // whether a delegation is checkable (defect 5). Anything unresolvable exits non-zero.
    const [p, name] = paths;
    if (!p || !name) { console.log("ERROR\t-\tpkgscript takes <package.json> <script-name>"); process.exit(1); }
    let scripts;
    try { scripts = (JSON.parse(await Bun.file(p).text()).scripts) || {}; }
    catch (e) { console.log(`ERROR\t${p}\tis not parseable JSON (${e.message})`); process.exit(1); }
    if (typeof scripts[name] !== "string") { console.log(`ERROR\t${p}\tno script named '${name}'`); process.exit(1); }
    console.log(`FILES\t1\t${p}`);
    scanSnippet(scripts[name], `${p} scripts.${name}`, (n) => scripts[n], new Set([name]));
  } else if (mode === "strip") {
    // Shell comments removed, quote-aware, for test 6's substring tripwire (defect 1). Heredoc
    // bodies are KEPT: that check wants comments gone, nothing else.
    if (paths.length !== 1) { console.log("ERROR\t-\tstrip takes exactly one file"); process.exit(1); }
    raw = strip(await Bun.file(paths[0]).text(), false).text;
  } else if (mode === "discover") {
    // Which package.json files are this repo's OWN manifests, for test 8.
    //
    // DEFECT 4 (#1537 round 18): discovery was `find -name package.json` minus node_modules and
    // .git, so a GENERATED dist/package.json, a build/ or coverage/ artefact, or a package.json
    // inside a test fixture would have been scanned and could have failed CI over output that no
    // one runs. The manifests worth judging are exactly the ones bun itself treats as packages: the
    // root manifest plus the workspace members THAT MANIFEST declares. Same philosophy as test 1 -
    // derive the list from the repo's own config rather than hardcoding a directory topology, so a
    // new workspace is still covered automatically while generated output never is.
    const root = paths[0];
    if (!root) { console.log("ERROR\t-\tdiscover takes a repo root"); process.exit(1); }
    let pkg;
    try { pkg = JSON.parse(await Bun.file(`${root}/package.json`).text()); }
    catch (e) { console.log(`ERROR\t${root}\troot package.json missing or unparseable (${e.message})`); process.exit(1); }
    const globs = Array.isArray(pkg.workspaces) ? pkg.workspaces
      : (pkg.workspaces && Array.isArray(pkg.workspaces.packages) ? pkg.workspaces.packages : []);
    const found = new Set([`${root}/package.json`]);
    for (const g of globs) {
      const pat = `${String(g).replace(/\/+$/, "")}/package.json`;
      for (const rel of new Bun.Glob(pat).scanSync({ cwd: root, onlyFiles: true })) found.add(`${root}/${rel}`);
    }
    raw = [...found].sort().join("\n") + "\n";
  } else { console.log(`ERROR\t-\tunknown mode '${mode}'`); process.exit(1); }
} catch (e) {
  console.log(`ERROR\t-\t${e.message}`); process.exit(1);
}
if (raw !== null) {
  process.stdout.write(raw);
} else {
  console.log(`COUNT\t${examined}`);
  if (minAccepted !== null) console.log(`MIN\t${minAccepted}`);
  for (const l of [...new Set(out)]) console.log(l);
}
SCANJS

# run_scanner <mode> <args...> -> SCAN_RC, SCAN_OUT, SCAN_FINDINGS, SCAN_COUNT, SCAN_FILES
run_scanner() {
    SCAN_OUT=$(BB_TIMEOUT_FLOOR="$TIMEOUT_FLOOR" bun "$SCANNER" "$@" 2>&1)
    SCAN_RC=$?
    SCAN_FINDINGS=$(printf '%s\n' "$SCAN_OUT" | /usr/bin/grep '^FINDING' | sed $'s/^FINDING\t/    /; s/\t/: /')
    SCAN_COUNT=$(printf '%s\n' "$SCAN_OUT" | sed -n $'s/^COUNT\t//p' | tail -1)
    SCAN_FILES=$(printf '%s\n' "$SCAN_OUT" | sed -n $'s/^FILES\t//p' | tail -1)
    SCAN_MIN=$(printf '%s\n' "$SCAN_OUT" | sed -n $'s/^MIN\t//p' | tail -1)
    [ -n "$SCAN_COUNT" ] || SCAN_COUNT=0
}

# mentions_bun_test <file> -> 0 if the file still mentions a bun test invocation once SHELL COMMENTS
# ARE GONE. Test 6's tripwire, fixtures `sh-*` in test 9.
#
# DEFECT 1 (#1537 round 18): the tripwire was a raw substring search over the whole file, so a
# helper whose only mention was `# Example command for contributors: bun test src/` was rejected and
# CI blocked over a comment. The search stays deliberately dumb - substrings, no quoting rules, no
# argument parsing - but it now runs over comment-stripped text, using the same quote-aware stripper
# tests 7-8 use (so `${VAR#pat}` and a quoted `#` still survive). If the stripper cannot run, the
# raw text is used instead: a false ALARM is the acceptable direction for a tripwire, silence is not.
mentions_bun_test() {
    local f="$1" body
    if ! body=$(BB_TIMEOUT_FLOOR="$TIMEOUT_FLOOR" bun "$SCANNER" strip "$f" 2>/dev/null); then
        body=$(cat "$f")
    fi
    case "$body" in
        *"bun test"*|*"bun run test"*|*"bunx test"*) return 0 ;;
    esac
    return 1
}

# discover_pkgs <root> -> the package.json files test 8 judges. See the `discover` mode above for
# why this is workspace-derived rather than a bare `find`.
discover_pkgs() {
    BB_TIMEOUT_FLOOR="$TIMEOUT_FLOOR" bun "$SCANNER" discover "$1" 2>/dev/null
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
    # this run cannot observe") while the package.json scanner RESOLVED the very same delegation and
    # judged the delegate's body. The two halves of this guard disagreed about whether `bun run
    # test:unit` is checkable. It is: the delegate is in the manifest, so follow it - with the same
    # scanner code, via `pkgscript` - and report only when the delegation genuinely cannot be
    # resolved (no such script, unparseable manifest, a cycle) or when what it resolves to is bad.
    # A resolved delegation contributes its smallest timeout to min_timeout, so test 5 still probes
    # a value that was really reached rather than skipping over the delegated run.
    if [ "$sub" = "run" ]; then
        case "$target" in
            test*)
                local d_out d_rc d_find d_min
                d_out=$(BB_TIMEOUT_FLOOR="$TIMEOUT_FLOOR" bun "$SCANNER" pkgscript "$DELEGATE_MANIFEST" "$target" 2>&1)
                d_rc=$?
                if [ "$d_rc" -ne 0 ]; then
                    delegated+="    $label: $cmd run $target could not be resolved against $DELEGATE_MANIFEST ($(printf '%s\n' "$d_out" | tail -1 | tr '\t' ' ')), so its timeout is unverifiable. Invoke bun test directly with an explicit --timeout."$'\n'
                    return 0
                fi
                d_find=$(printf '%s\n' "$d_out" | /usr/bin/grep '^FINDING' | sed $'s/^FINDING\t//; s/\t/: /')
                if [ -n "$d_find" ]; then
                    timeout_problems+="    $label: $cmd run $target resolves to $(printf '%s' "$d_find" | tr '\n' ' ')"$'\n'
                    return 0
                fi
                d_min=$(printf '%s\n' "$d_out" | sed -n $'s/^MIN\t//p' | tail -1)
                if [ -z "$d_min" ]; then
                    delegated+="    $label: $cmd run $target resolves in $DELEGATE_MANIFEST but reaches no 'bun test' at all, so this run's timeout was never verified."$'\n'
                    return 0
                fi
                test_invocations=$((test_invocations + 1))
                if [ -z "${min_timeout:-}" ] || [ "$d_min" -lt "$min_timeout" ]; then
                    min_timeout="$d_min"
                fi
                other_invocations+="    $label: $cmd run $target -> resolved in $DELEGATE_MANIFEST, smallest --timeout $d_min"$'\n'
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
# This is deliberately the dumbest text check in the file: a literal substring search, no quoting
# rules, no argument parsing (tests 7-8 need a real scanner; this one does not).
#
# It used to strip nothing at all, and the "at worst it fires on a comment" that this note shrugged
# off was DEFECT 1 of #1537 round 18: a helper whose only mention was `# Example command for
# contributors: bun test src/` would have blocked CI for the whole repository. Comments are now
# stripped first, by mentions_bun_test() above, using the same quote-aware stripper as tests 7-8 -
# and nothing else is, so the check still cannot produce a false pass on real command text.
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
# Test 7: every `bun test` written into a workflow run: block.
#
# #1537 round 17. Tests 1-6 scan apps/web/scripts. They never saw the WORKFLOWS, which invoked
# `bun test src/` directly for apps/mcp-server — and this guard's own SCOPE LIMIT note mentioned
# mcp-server only to EXCLUDE it ("not apps/web's, verified"). That dismissal is what failed: on
# 2026-08-21 `MCP Server Validation` went red with
#     src/spec/index-builder.test.ts (fail) (unnamed) [6665.15ms]
#       ^ a beforeEach/afterEach hook timed out for this test
# — a 6.7s hook against bun's 5s default, on a loaded host, in a suite that passes when the host is
# idle. Exactly the defect this file was created for, one directory over. The same raw invocation
# was in deploy-production.yml, so a loaded PRODUCTION DEPLOY could have failed the same way.
#
# A COUNT of zero is NOT a failure here: workflows are free to invoke `bun run <script>` and let
# test 8 check the script body, which is what this repo does today. The anti-vacuity signal for
# this test is FILES, not COUNT - the scanner exits 1 if the workflow directory yielded no files.
# ---------------------------------------------------------------------------------------------
echo "Test 7: no workflow run: block executes 'bun test' without exactly one --timeout >= $TIMEOUT_FLOOR"
run_scanner workflows "$REPO_ROOT/.github/workflows"
if [ "$SCAN_RC" -ne 0 ]; then
    bad "the workflow scan could not run, so its silence means nothing: $(printf '%s\n' "$SCAN_OUT" | tail -3 | tr '\n' ' ')"
elif [ -n "$SCAN_FINDINGS" ]; then
    bad "workflow 'bun test' invocations with an unusable timeout:"
    printf '%s\n' "$SCAN_FINDINGS"
else
    ok "${SCAN_FILES%%$'\t'*} workflow file(s) parsed (.yml and .yaml); $SCAN_COUNT raw 'bun test' invocation(s), all with one --timeout >= $TIMEOUT_FLOOR"
fi

# ---------------------------------------------------------------------------------------------
# Test 8: every `bun test` written into a package.json script, anywhere in the repo.
#
# This is where the five known offenders lived (apps/web's test:watch, test:integration, test:all
# and apps/mcp-server's test, test:watch): all five ran `bun test` with no flag at all, and the
# SCOPE LIMIT note above excused three of them on the grounds that "nothing runs them" — which was
# true until deploy-production.yml and test.yml started running `bun run test` for mcp-server.
#
# `bun run <name>` is FOLLOWED into the same package.json rather than flagged (test 4 flags an
# observed delegation because it cannot see where the flags live; here it can), so a script that
# only composes other scripts is judged by what it ultimately executes. Unresolvable delegation —
# a missing script, a cycle — is a FINDING, never a pass. `bun run ./x.ts` is a FILE, not a script.
#
# The file list is discovered, not hardcoded: the root manifest plus the workspace members the root
# manifest declares (see the `discover` mode), which today is the root plus apps/* plus packages/*.
# A new workspace is covered automatically; a GENERATED package.json under dist/, build/ or
# coverage/, or one inside a test fixture, is not - that was DEFECT 4 of #1537 round 18, a false
# alarm waiting for the first person to run a build before running the tests.
# COUNT must be non-zero: five such scripts exist, so zero means discovery broke.
# ---------------------------------------------------------------------------------------------
echo "Test 8: no package.json script runs 'bun test' without exactly one --timeout >= $TIMEOUT_FLOOR"
PKGS=()
while IFS= read -r f; do
    [ -n "$f" ] && PKGS+=("$f")
done < <(discover_pkgs "$REPO_ROOT")

if [ "${#PKGS[@]}" -lt 2 ]; then
    bad "discovery returned ${#PKGS[@]} package.json file(s) under $REPO_ROOT - this is a bun workspace repo with several, so a clean result would be meaningless"
else
    run_scanner pkg "${PKGS[@]}"
    if [ "$SCAN_RC" -ne 0 ]; then
        bad "the package.json scan could not run, so its silence means nothing: $(printf '%s\n' "$SCAN_OUT" | tail -3 | tr '\n' ' ')"
    elif [ -n "$SCAN_FINDINGS" ]; then
        bad "package.json scripts running 'bun test' with an unusable timeout:"
        printf '%s\n' "$SCAN_FINDINGS"
    elif [ "$SCAN_COUNT" -eq 0 ]; then
        bad "${#PKGS[@]} package.json file(s) were read but not one 'bun test' was found in any script - apps/web and apps/mcp-server both have several, so this is a broken scan, not a clean tree"
    else
        ok "${#PKGS[@]} package.json file(s) scanned; $SCAN_COUNT 'bun test' script(s) found, all with one --timeout >= $TIMEOUT_FLOOR"
    fi
fi

# ---------------------------------------------------------------------------------------------
# Test 9: the scanner's own failure modes, on synthetic fixtures.
#
# THIS IS THE TEST THAT MATTERS MOST IN THIS FILE. Tests 7 and 8 run against a tree that is clean,
# so on their own they cannot distinguish a working scanner from a blind one - which is precisely
# how the first version of test 7 shipped five demonstrated false passes (see the header). Every
# fixture below is a command line that a human reads as safe and bun runs at 100ms or 5000ms.
#
# The negative controls are not decoration: they are what stops the next person from "fixing" the
# comment stripper by deleting `#` handling wholesale, or the delegation follower by flagging every
# `bun run`. A fixture asserted CLEAN fails just as loudly as one asserted FLAGGED.
#
# If you add a defence to the scanner, add a fixture here and watch it go from FLAGGED-expected-but-
# clean to flagged. If you cannot make a fixture fail without your change, you have not fixed
# anything.
# ---------------------------------------------------------------------------------------------
echo "Test 9: the scanners are exercised against synthetic fixtures, one per known defect"

FIX="$WORK/fixtures"
mkdir -p "$FIX"

# fixture <name> <mode> <expect> <why> <ext>, body on stdin.
#
# mode      what is exercised                                    body is
#   wf      the workflow scan (test 7)                           a workflow YAML
#   pkg     the package.json script scan (test 8)                a package.json
#   sh      the unexecuted-runner tripwire (test 6)              a shell script
#   disc    package.json discovery (test 8)                      a `=== path` tree, see below
#   argv    check_record(), the OBSERVED half (tests 3 and 4)    a `=== path` tree
#
# expect is `clean`, `flagged`, `flagged:SUBSTRING` or `discovered:<space-separated paths>`.
# `flagged:SUBSTRING` additionally requires the finding to CONTAIN that substring, which is how an
# assertion can pin *how* the scanner reached the invocation and not merely that it did. Needed for
# the delegation follower: every script is scanned directly anyway, so "did you follow `bun run`?"
# is invisible to a plain flagged/clean assertion (verified: a mutant that stopped following still
# passed) - only the finding's label, `scripts.test -> test:unit`, distinguishes the two.
# For mode `wf` the body is a workflow YAML written into its own directory (so one fixture cannot
# mask another); for mode `pkg` it is a package.json.
fx_names=(); fx_modes=(); fx_expect=(); fx_paths=(); fx_why=()
fixture() {
    local name="$1" mode="$2" expect="$3" why="$4" ext="$5"
    local dir="$FIX/$name"
    mkdir -p "$dir"
    case "$mode" in
        wf) cat > "$dir/wf.$ext" ;;
        pkg) cat > "$dir/package.json" ;;
        sh) cat > "$dir/script.sh" ;;
        disc|argv)
            # Multi-file fixture: a line `=== <relative path>` opens a new file under $dir/tree.
            local cur="" line
            while IFS= read -r line; do
                case "$line" in
                    '=== '*)
                        cur="$dir/tree/${line#=== }"
                        mkdir -p "$(dirname "$cur")"
                        : > "$cur"
                        ;;
                    *) [ -n "$cur" ] && printf '%s\n' "$line" >> "$cur" ;;
                esac
            done
            ;;
        *) echo "fixture: unknown mode '$mode'" >&2; cat > /dev/null ;;
    esac
    fx_names+=("$name"); fx_modes+=("$mode"); fx_expect+=("$expect"); fx_paths+=("$dir"); fx_why+=("$why")
}

# defect 1 - the old scan globbed *.yml, so this real-world extension was never opened at all.
fixture yaml-extension wf flagged "a .yaml workflow (deploy-to-staging.yaml is one) must be read too" yaml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - name: mcp tests
        run: bun test src/
FX

# defect 2 - the old scan took one invocation per physical line and stopped at the first --timeout.
fixture two-per-line wf flagged "the SECOND command on a line runs at the 5s default" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: bun test --timeout 30000 src/ && bun test src/spec/
FX

# defect 3a - a `\` continuation inside a block scalar hid every following flag from a line regex.
fixture backslash-continuation wf flagged "a command continued with a backslash is still one command" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          bun \
            test src/
FX

# defect 3b - a folded scalar (`>`) folds the newline between `bun` and `test` into a space, so the
# invocation exists in the shell but appears on two separate SOURCE lines.
fixture folded-scalar wf flagged "a folded scalar splitting 'bun' from 'test' is still 'bun test'" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: >-
          bun
          test src/
FX

# round-17 defect 4 - the greedy sed read the number out of the trailing COMMENT and reported 30000.
#
# TWO things were wrong with the first draft of this fixture, `- run: bun test --timeout 100 #
# --timeout 30000`, and both made it VACUOUS - it passed with the scanner's comment stripping
# removed, so it never proved the defence it names:
#
#   - a `#` in a PLAIN YAML scalar is a YAML comment. `Bun.YAML.parse` had already deleted it, so
#     the run block the scanner received was `bun test --timeout 100` and the shell stripper was
#     never involved. Verified directly: parsing `a: bun test --timeout 100 # --timeout 30000`
#     yields `{a: "bun test --timeout 100"}`. Any fixture for the SHELL comment stripper must use a
#     block scalar (`|`), which preserves `#`;
#   - what actually flagged it was the below-floor branch (100 < 15000), not comment handling.
#
# So: a block scalar, and a command that is bad in a way ONLY the comment can hide - no --timeout at
# all, with the sole `--timeout 30000` in the text sitting inside the comment. Unstripped that reads
# as one adequate flag, which is exactly the round-17 false pass.
fixture inline-comment wf "flagged:NO --timeout" "the real command has no timeout; the only --timeout 30000 is in a shell comment" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          bun test src/ # run this locally with --timeout 30000
FX

# negative control for defect 4: a `#` inside a quoted string, and a `${VAR#pattern}` expansion,
# are NOT comments. Stripping them would corrupt real command text (and did, in rounds 1-9).
# Both hazards are on the SAME command line as the bun test on purpose. An earlier draft put the
# `${VAR#pat}` on its own line, and a mutant that dropped the word-boundary rule still passed: the
# truncation ate only that harmless line. A `#` hazard is only load-bearing where cutting the line
# would change what flags the scanner sees, so the expansion sits BEFORE --timeout, where greedy
# stripping turns this into a bare `bun test --preload` with no timeout at all.
fixture hash-not-a-comment wf clean "quoted '#' and \${VAR#pat} must survive comment stripping" yaml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          bun test --preload ${PRELOAD#./} -t "issue #42 handling" --timeout 30000 src/
FX

# a --timeout whose value is a variable cannot be resolved by a text scan, so it is refused.
fixture unresolvable-value wf flagged "a non-literal --timeout value must not be assumed adequate" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: bun test --timeout "$SOME_TIMEOUT" src/
FX

# The FLOOR itself, not just "above bun's default". 6000 clears a >5000 check and would even
# survive test 5's 5.6s probe, yet the CI failure that started #1537 took 9910ms. Without this
# fixture a mutant that lowered the floor to 5000 passed everything, including test 9.
fixture below-floor wf flagged "6000ms clears bun's default but not the observed 9910ms case" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: bun test --timeout 6000 src/
FX

# negative control for the `--timeout=N` spelling: it must be RECOGNISED, not read as "no flag".
fixture equals-form wf clean "--timeout=30000 is one adequate flag, not a missing one" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: bun test --timeout=30000 src/
FX

# Exactly one --timeout, because bun honours the LAST and a human reads the first.
#
# The first draft used `--timeout 30000 --timeout 100`, and it was VACUOUS: with the duplicate-flag
# branch removed the scanner falls back to the LAST value, 100, which is below the floor, so the
# fixture stayed green on the strength of a different defence. Both values are therefore >= the
# floor here, which makes the duplication itself the only thing wrong with the command - the
# ambiguity is the defect, whichever value bun happens to pick.
fixture two-timeout-flags wf "flagged:2 --timeout flags" "two --timeout flags is ambiguous even when both are adequate" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: bun test --timeout 30000 --timeout 20000 src/
FX

# negative control: a heredoc body is data, not commands. Shaped after the python validator in
# test.yml, whose body contains the words "bun test" and an unbalanced apostrophe. BOTH lines below
# are chosen so that parsing the body as shell would produce a finding - a bare `bun test src/`
# (no timeout) and an unterminated quote. An earlier draft of this fixture put both inside double
# quotes, where they were harmless either way, and a mutant that removed heredoc skipping passed it.
fixture heredoc-body wf clean "a heredoc body must not be parsed as shell" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          cat <<'DOC'
          Reproduce locally with:
            bun test src/
          It's the same suite CI runs.
          DOC
          bun test --timeout 30000 src/
FX

# ...and scanning must RESUME after the terminator, or a heredoc anywhere earlier in a run block
# would hide every command below it.
fixture heredoc-resumes wf flagged "a bare 'bun test' after a heredoc terminator is still seen" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - run: |
          cat <<EOF
          bun test --timeout 30000 src/
          EOF
          bun test src/
FX

# defect 5 - package.json was not scanned at all; this is where all five real offenders lived.
fixture pkg-raw pkg flagged "a package.json script running bun test with no flag" '' <<'FX'
{ "scripts": { "test": "bun test src/" } }
FX

# delegation must be FOLLOWED, so a clean-looking wrapper cannot hide a bare invocation.
fixture pkg-delegated-bad pkg "flagged:scripts.test -> test:unit" "delegation must be followed into the delegate's body" '' <<'FX'
{ "scripts": { "test": "bun run test:unit", "test:unit": "bun test src/" } }
FX

# negative control: following delegation must not turn every `bun run` into a failure.
fixture pkg-delegated-good pkg clean "a delegation whose target is adequately flagged is fine" '' <<'FX'
{ "scripts": { "test": "bun run test:unit", "test:unit": "bun test --timeout 30000 src/" } }
FX

# negative control: `bun run <file>.ts` executes a FILE, not a package.json script, and must not be
# reported as an unresolvable delegation (apps/mcp-server has several of these).
fixture pkg-runs-a-file pkg clean "'bun run src/x.ts' is a file, not a missing script" '' <<'FX'
{ "scripts": { "build": "bun run src/spec/index-builder.ts" } }
FX

# a delegation that goes nowhere is a FINDING: its timeout is unknowable, so it cannot be cleared.
fixture pkg-missing-target pkg flagged "delegation to a script that does not exist" '' <<'FX'
{ "scripts": { "test": "bun run test:nope" } }
FX

# ═══ #1537 round 18: one fixture per defect found by the adversarial review of round 17 ═════════

# DEFECT 3 - `run` was taken at ANY depth, so an action INPUT called `run` was scanned as shell.
# GitHub never executes this; flagging it blocks CI over correct YAML.
fixture with-input-not-a-step wf clean "a 'run' key under with: is action input data, not a step" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - uses: some/action@v1
        with:
          run: bun test src/
FX

# ...and the same shape at a step's env:, which is also data.
fixture env-value-not-a-step wf clean "a 'run' key under env: is data, not a step" yml <<'FX'
name: fixture
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - uses: some/action@v1
        env:
          run: bun test src/
FX

# DEFECT 2 - a timeout resolved from a literal assignment in the SAME script is legitimate code and
# must not be refused. This fixture is red before the resolution exists and clean after.
fixture var-resolved-literal pkg clean "TIMEOUT=30000 earlier in the same script resolves" '' <<'FX'
{ "scripts": { "test": "TIMEOUT=30000; bun test --timeout \"$TIMEOUT\" src/" } }
FX

# ...and resolution must not become a free pass: a resolved value below the floor is still a finding,
# and the substring pins that it was resolved (a scanner that refused the variable outright would
# flag it too, but with the 'cannot resolve' wording, so this fixture separates the two).
fixture var-resolved-below-floor pkg "flagged:below the 15000ms floor" "a resolved value is still held to the floor" '' <<'FX'
{ "scripts": { "test": "T=100; bun test --timeout $T src/" } }
FX

# ...and an assignment that comes AFTER the invocation must not excuse it: shells do not work
# backwards, and neither does the resolver.
fixture var-assigned-later pkg "flagged:cannot resolve" "an assignment after the command does not resolve it" '' <<'FX'
{ "scripts": { "test": "bun test --timeout $T src/; T=30000" } }
FX

# DEFECT 6 - a command preceded by a shell reserved word was invisible: COUNT 0, no finding, a real
# suite running at bun's 5s default.
fixture keyword-prefixed pkg flagged "'if bun test ...' is still a bun test invocation" '' <<'FX'
{ "scripts": { "test": "if bun test src/; then echo ok; fi" } }
FX

# ...the loop forms too, since `while`/`until` were missing from the same list.
fixture keyword-prefixed-loop pkg flagged "'until bun test ...' is still a bun test invocation" '' <<'FX'
{ "scripts": { "test": "until bun test src/; do echo retry; done" } }
FX

# DEFECT 1 - test 6's tripwire fired on a MENTION in a comment, blocking CI over documentation.
fixture sh-comment-only sh clean "a 'bun test' inside a comment is not an invocation" '' <<'FX'
#!/usr/bin/env bash
# Example command for contributors: bun test src/
echo "this helper runs nothing"
FX

# ...and stripping comments must not blind the tripwire to a real invocation.
fixture sh-real-invocation sh flagged "a real 'bun test' in an unexecuted script must still fire" '' <<'FX'
#!/usr/bin/env bash
bun test --timeout 30000 src/
FX

# ...nor to one written inside a heredoc, which is why `strip` keeps heredoc bodies.
fixture sh-heredoc-mention sh flagged "a 'bun test' inside a heredoc must still fire the tripwire" '' <<'FX'
#!/usr/bin/env bash
cat <<'DOC' > run-me.sh
bun test src/
DOC
FX

# DEFECT 4 - discovery was a bare find, so generated and fixture manifests were scanned. Only the
# root manifest and the workspace members it declares are this repo's own packages.
fixture disc-only-workspaces disc "discovered:apps/web/package.json package.json packages/x/package.json" "generated and fixture manifests are not repo manifests" '' <<'FX'
=== package.json
{ "workspaces": ["apps/*", "packages/*"] }
=== apps/web/package.json
{ "scripts": { "test": "bun test --timeout 30000 src/" } }
=== apps/web/dist/package.json
{ "scripts": { "test": "bun test src/" } }
=== coverage/package.json
{ "scripts": { "test": "bun test src/" } }
=== scripts/__tests__/fx/package.json
{ "scripts": { "test": "bun test src/" } }
=== packages/x/package.json
{ "type": "module" }
FX

# DEFECT 5 - an OBSERVED `bun run test:*` was an automatic failure while the package.json scan
# resolved the identical delegation. Resolvable now means followed, and clean means clean.
fixture argv-delegation-clean argv clean "an observed delegation that resolves to an adequate bun test is fine" '' <<'FX'
=== argv
bun
run
test:unit
=== package.json
{ "scripts": { "test:unit": "bun test --timeout 30000 src/" } }
FX

# ...and following it must judge what it reaches, not wave it through.
fixture argv-delegation-bad argv "flagged:NO --timeout" "a delegation to a bare bun test is a timeout failure" '' <<'FX'
=== argv
bun
run
test:unit
=== package.json
{ "scripts": { "test:unit": "bun test src/" } }
FX

# ...and a delegation that cannot be resolved is still refused, which is the fail-closed half.
fixture argv-delegation-missing argv "flagged:could not be resolved" "a delegation to a script that does not exist stays a failure" '' <<'FX'
=== argv
bun
run
test:nope
=== package.json
{ "scripts": { "test:unit": "bun test --timeout 30000 src/" } }
FX

fx_problems=""
fx_i=0
while [ "$fx_i" -lt "${#fx_names[@]}" ]; do
    fx_name="${fx_names[$fx_i]}"; fx_mode="${fx_modes[$fx_i]}"
    fx_exp="${fx_expect[$fx_i]}"; fx_dir="${fx_paths[$fx_i]}"; fx_msg="${fx_why[$fx_i]}"
    fx_i=$((fx_i + 1))

    fx_got=""
    case "$fx_mode" in
        wf) run_scanner workflows "$fx_dir" ;;
        pkg) run_scanner pkg "$fx_dir/package.json" ;;
        sh)
            # Test 6's tripwire, not the command scanner.
            SCAN_RC=0; SCAN_FINDINGS=""
            if mentions_bun_test "$fx_dir/script.sh"; then
                SCAN_FINDINGS="    $fx_name: the tripwire fires on this script"
            fi
            ;;
        disc)
            # Test 8's discovery. Compared as a list, so both a missing and an extra manifest fail.
            SCAN_RC=0; SCAN_FINDINGS=""
            fx_got=$(discover_pkgs "$fx_dir/tree" | sed "s#^$fx_dir/tree/##" | tr '\n' ' ')
            fx_got="${fx_got% }"
            ;;
        argv)
            # check_record() itself, on a synthetic argv record, against a synthetic manifest. The
            # globals it accumulates into are saved and restored so a fixture cannot contaminate the
            # live results of tests 3-5 (nor be masked by them).
            SCAN_RC=0
            fx_rec="$fx_dir/rec"
            : > "$fx_rec"
            while IFS= read -r fx_a; do printf '%s\0' "$fx_a" >> "$fx_rec"; done < "$fx_dir/tree/argv"
            fx_s_tp="$timeout_problems"; fx_s_dg="$delegated"; fx_s_oi="$other_invocations"
            fx_s_ti="$test_invocations"; fx_s_mt="${min_timeout:-}"; fx_s_dm="$DELEGATE_MANIFEST"
            timeout_problems=""; delegated=""; other_invocations=""
            DELEGATE_MANIFEST="$fx_dir/tree/package.json"
            check_record "$fx_rec" "$fx_name"
            SCAN_FINDINGS="$timeout_problems$delegated"
            timeout_problems="$fx_s_tp"; delegated="$fx_s_dg"; other_invocations="$fx_s_oi"
            test_invocations="$fx_s_ti"; min_timeout="$fx_s_mt"; DELEGATE_MANIFEST="$fx_s_dm"
            ;;
        *)
            fx_problems+="    $fx_name: unknown fixture mode '$fx_mode' - fix the fixture table"$'\n'
            continue
            ;;
    esac

    if [ "$SCAN_RC" -ne 0 ]; then
        fx_problems+="    $fx_name: the scanner exited $SCAN_RC on this fixture: $(printf '%s\n' "$SCAN_OUT" | tail -2 | tr '\n' ' ')"$'\n'
        continue
    fi
    case "$fx_exp" in
        clean)
            if [ -n "$SCAN_FINDINGS" ]; then
                fx_problems+="    $fx_name: expected no findings ($fx_msg) but got: $(printf '%s' "$SCAN_FINDINGS" | tr '\n' ' ')"$'\n'
            fi
            ;;
        flagged)
            if [ -z "$SCAN_FINDINGS" ]; then
                fx_problems+="    $fx_name: expected a FINDING and got none - the scanner is blind to this case ($fx_msg). This fixture exists because that blindness shipped once already."$'\n'
            fi
            ;;
        discovered:*)
            fx_want="${fx_exp#discovered:}"
            if [ "$fx_got" != "$fx_want" ]; then
                fx_problems+="    $fx_name: discovery returned '$fx_got' but must return '$fx_want' ($fx_msg)"$'\n'
            fi
            ;;
        flagged:*)
            fx_want="${fx_exp#flagged:}"
            if [ -z "$SCAN_FINDINGS" ]; then
                fx_problems+="    $fx_name: expected a FINDING and got none - the scanner is blind to this case ($fx_msg). This fixture exists because that blindness shipped once already."$'\n'
            elif ! printf '%s' "$SCAN_FINDINGS" | /usr/bin/grep -qF -- "$fx_want"; then
                fx_problems+="    $fx_name: flagged, but no finding mentions '$fx_want', so it was not reached the way this fixture asserts ($fx_msg). Got: $(printf '%s' "$SCAN_FINDINGS" | tr '\n' ' ')"$'\n'
            fi
            ;;
        *)
            fx_problems+="    $fx_name: unknown expectation '$fx_exp' - fix the fixture table"$'\n'
            ;;
    esac
done

if [ -n "$fx_problems" ]; then
    bad "scanner fixtures that did not behave as asserted:"
    printf '%s' "$fx_problems"
else
    ok "all ${#fx_names[@]} fixture(s) behaved as asserted (each known false-pass is flagged; each negative control stays clean)"
fi

if [ -n "$other_invocations" ]; then
    echo
    echo "  (informational) non-'bun test' invocations observed:"
    printf '%s' "$other_invocations"
fi

finish
