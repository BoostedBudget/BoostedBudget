#!/usr/bin/env bash
# #1569 round 13 task 1 — the mechanical guard the ruling asked for.
#
# THE STORY: backend/scripts/assert-migrate-reset-target-disposable.sh gates `make migrate-reset`
# (`migrate ... down -all`). It has been defeated three separate ways since round 12, and every
# time the gate itself was correct — the defeat was always a path around it that nobody had
# enumerated:
#   1. Execution order: `down -all` runs in reverse, so a later migration's unconditional DELETE
#      could run before an earlier migration's row-count refusal ever saw the rows.
#   2. Documentation: the migration guide taught the raw `migrate ... down -all` invocation in a
#      dozen places, none of which mentioned the gate.
#   3. A sibling Makefile target: `backend/Makefile`'s `migrate-down` (`migrate ... down 1`) never
#      called the gate at all — it only checked that DATABASE_URL was set.
#
# The user's ruling: stop manually re-sweeping for the next bypass and build a guard that FAILS
# the moment a new ungated invocation of `migrate ... down` appears anywhere in the SCANNED
# UNIVERSE (see below) — in a new Makefile target, a new script, a new CI workflow step, a new
# Dockerfile RUN/CMD/ENTRYPOINT, or an edit that quietly deletes a gate call from an existing one.
#
# ⚠⚠ ROUND 13 FIX ROUND 1 (Codex finding): the FIRST version of this script scanned only
# `*.sh`/`Makefile` — a fourth, unenumerated bypass path, structurally identical to the three
# above. `migrate` is a real binary resident in the runtime image (backend/Dockerfile builds it at
# :65 and copies it into the final stage at :85), so a future CI workflow `run:` step or a
# Dockerfile `RUN`/`CMD`/`ENTRYPOINT` line could invoke it with the old version of this script,
# `make test-scripts`, AND CI all green. The scanned universe below was widened in response, and
# the SCOPE CLAIM in this comment (and in the docs that cite this script) was cut back to match
# what is actually scanned, rather than left broad — see "WHAT IS NOT SCANNED" at the bottom for
# the residual this script does NOT cover, named explicitly rather than silently.
#
# WHAT THIS SCRIPT DOES: scans every tracked file in these categories (test fixtures under
# __tests__/ and *.test.sh excluded — they legitimately drive the real `migrate` binary to set up
# scenarios for the guards' own test suites, see the exclusion below):
#   - `*.sh` and `Makefile` / `*/Makefile` (shell scripts and Makefiles, by extension/name)
#   - `.github/workflows/*.yml` and `*.yaml` (a `run:` step is shell text embedded in YAML — the
#     same line-based detector below applies to it unchanged)
#   - `Dockerfile`, `Dockerfile.*`, `*/Dockerfile`, `*/Dockerfile.*` (a `RUN`/`CMD`/`ENTRYPOINT`
#     line is shell text too, and `migrate` is resident in the runtime image per the finding above)
#   - any OTHER tracked file with no extension in its basename that starts with a shell shebang
#     (`#!.../bash`, `#!.../sh`, `#!.../dash`, `#!.../zsh`) — an extensionless script would
#     otherwise slip past both the `*.sh` glob and the reader's expectation that "it's a script" is
#     visible from its name
# for lines that actually INVOKE the `migrate` CLI binary with a `down` subcommand — as opposed to
# a comment or a log/echo string that merely mentions "migrate down" as text. Every invocation
# found must be one of a small, hardcoded, justified allowlist below, AND — this is what makes the
# allowlist not a one-time rubber stamp — each allowlisted site is also checked STRUCTURALLY: the
# specific gate call it relies on must still be present immediately ahead of it. Deleting the gate
# call (leaving the invocation line itself untouched) fails this script exactly as loudly as adding
# a brand new ungated invocation does.
#
# Exit 0: every invocation found (in the scanned universe) is allowlisted and structurally gated.
# Exit 1: a new (non-allowlisted) invocation was found, or an allowlisted one lost its gate call.
#
# ⚠⚠ WHAT IS NOT SCANNED — named explicitly rather than left as a silent gap in a broad claim:
#   - `.mjs`/`.js`/`.ts`/`.cjs` (bun/npm scripts). A shell invocation from JS is necessarily inside
#     a string literal argument to something like `execSync(...)` — there is no bare-word command
#     position to anchor on the way there is in a shell script or a YAML `run:` block, so the
#     command-position heuristic below (which deliberately excludes a quote-preceded "migrate" to
#     avoid flagging prose like `log_error "...migrate down -all..."`) would have to accept
#     virtually every quoted string, and would then flag ordinary comments and console.log/test
#     strings mentioning "migrate down" as prose. A guard that fires on legitimate code gets
#     deleted by the next engineer, so this is left unscanned rather than shipped as an unsound
#     detector. No `.js`/`.mjs`/`.ts`/`.cjs` file in this repo currently mentions "migrate" and
#     "down" together (checked by hand at the time of writing); this is a residual, not a proof it
#     stays empty.
#   - Variable/eval indirection even WITHIN the scanned universe — `MIGRATE_BIN=migrate` then
#     `"$MIGRATE_BIN" ... down -all`, or `cmd="migrate ... down -all"` then `eval "$cmd"`. The first
#     shape has no literal "migrate" token on the invoking line at all; the second starts with a
#     quote, which the command-position class deliberately excludes for the same false-positive
#     reason as the JS case above. Judged not soundly detectable without risking exactly that
#     false-positive failure mode, so it is named here as a residual rather than left as an
#     implicit gap behind a broader claim.
#   - Anything in an UNTRACKED file (see the `git ls-files` note below) or in a multi-line
#     shell/Dockerfile continuation split across physical lines in a way this line-based scanner
#     does not reassemble.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

FAILURES=0

fail() {
    echo "FAIL: $1" >&2
    FAILURES=$((FAILURES + 1))
}

# ── Step 1: enumerate every tracked Makefile / shell script, excluding test fixtures ──
#
# Test fixtures are excluded deliberately (#1569 round 13 task 1, per the brief's ruling): a file
# under __tests__/ or named *.test.sh exists to drive the REAL migrate binary against a throwaway
# probe database as part of proving a guard works — that is not a bypass of anything, it is the
# proof. Gating fixtures the same way as production code would make the guard fail on the tests
# that exist specifically to exercise the thing it protects.
# `mapfile` is deliberately not used here (macOS ships bash 3.2 by default, which predates it, and
# this script has to run under whatever `bash` a developer's shell or CI resolves to).
#
# ⚠ `git ls-files` only lists TRACKED files. A brand new script that has not been `git add`-ed yet
# is invisible to this scan. That is intentional scope, not an oversight: this guard's job is to
# fail CI (and a local `make test-scripts` run) on a committed bypass, and by the time a PR reaches
# CI its files are tracked. Run this guard again after staging a new file if you want the local
# check to see it before committing.
CANDIDATE_FILES=()
while IFS= read -r f; do
    [ -n "$f" ] && CANDIDATE_FILES+=("$f")
done < <(
    git -C "$REPO_ROOT" ls-files -- \
        '*.sh' 'Makefile' '*/Makefile' \
        '.github/workflows/*.yml' '.github/workflows/*.yaml' \
        'Dockerfile' 'Dockerfile.*' '*/Dockerfile' '*/Dockerfile.*' \
        | grep -vE '(^|/)__tests__/' \
        | grep -vE '\.test\.sh$'
)

# Extensionless shell-shebang scripts: not reachable by an extension glob at all, so enumerated
# separately by checking every OTHER tracked file's own first line. Restricted to files whose
# basename has no '.' AND is not already one of the exact no-extension names handled above
# (`Makefile`/`Dockerfile`, which would otherwise be re-added here as duplicates) — a cheap,
# correct filter that avoids opening every one of the ~3,800 tracked files in this repo on every
# run for a shebang check that only ever matters for the handful without one.
while IFS= read -r f; do
    [ -n "$f" ] || continue
    base="${f##*/}"
    case "$base" in
        *.*) continue ;; # has an extension; already covered by a glob above (or irrelevant)
        Makefile|Dockerfile) continue ;; # already enumerated by name above
    esac
    case "$f" in
        */__tests__/*|*.test.sh) continue ;;
    esac
    [ -f "$REPO_ROOT/$f" ] || continue
    if head -c 64 "$REPO_ROOT/$f" 2>/dev/null | grep -qE '^#!.*(bash|/sh$|/sh[[:space:]]|dash|zsh)'; then
        CANDIDATE_FILES+=("$f")
    fi
done < <(git -C "$REPO_ROOT" ls-files)

# ── Step 2: within each candidate file, find lines that actually INVOKE `migrate ... down`, not
# lines that merely mention it in a comment or in a log/echo message string. ──
#
# The invocation regex requires "migrate" to appear in COMMAND position — preceded by the start of
# the line, whitespace, an opening paren (command substitution, `$(migrate ...)`), or a backtick —
# and never preceded by a quote character. That second half is what tells a real invocation like
#   rollback_output=$(migrate -database "$DATABASE_URL" -path "$DIR" down -all 2>&1)
# apart from a message that just talks about one, like
#   log_error "This suite runs ... and 'migrate down -all' against that"
# — in the message case "migrate" is preceded by a single-quote character used as a decorative
# quote mark inside the double-quoted string, which the command-position class below excludes.
#
# A full-line comment (after trimming leading whitespace and, for Makefile recipes, a leading `@`)
# is skipped outright by is_comment_line() below — belt-and-braces with the command-position check,
# since a comment can freely start with a bare word "migrate" that the anchor class would otherwise
# accept.
INVOCATION_RE='(^|[[:space:](`])migrate[[:space:]]+.*[[:space:]]down([[:space:]]|$)'

is_comment_line() {
    local line="$1" trimmed
    trimmed="${line#"${line%%[![:space:]]*}"}"      # strip leading whitespace
    trimmed="${trimmed#@}"                            # strip a Makefile silent-recipe '@'
    trimmed="${trimmed#"${trimmed%%[![:space:]]*}"}" # strip whitespace again (handles '@  #...')
    [[ "$trimmed" == \#* ]]
}

# file:line pairs found to invoke `migrate ... down`, populated below.
declare -a FOUND_FILES=()
declare -a FOUND_LINES=()

for f in "${CANDIDATE_FILES[@]}"; do
    [ -f "$REPO_ROOT/$f" ] || continue
    while IFS=: read -r lineno content; do
        [ -z "$lineno" ] && continue
        if is_comment_line "$content"; then
            continue
        fi
        FOUND_FILES+=("$f")
        FOUND_LINES+=("$lineno")
    done < <(grep -nE "$INVOCATION_RE" "$REPO_ROOT/$f" || true)
done

# ── Step 3: every invocation found must be one of these two allowlisted sites. Anything else is a
# brand new bypass path and fails outright — this is the "next engineer adds a target/script"
# case. ──
#
# Each allowlisted site is ALSO checked structurally immediately below (not just "the file/line
# exists") — see gate calls checked per-site below.
allowlisted() {
    local file="$1"
    case "$file" in
        backend/Makefile) return 0 ;;
        scripts/test-migrations.sh) return 0 ;;
        *) return 1 ;;
    esac
}

for i in "${!FOUND_FILES[@]}"; do
    f="${FOUND_FILES[$i]}"
    ln="${FOUND_LINES[$i]}"
    if ! allowlisted "$f"; then
        fail "$f:$ln invokes 'migrate ... down' and is not one of the allowlisted, gated sites (backend/Makefile, scripts/test-migrations.sh). Either route this through an existing gate (backend/scripts/assert-migrate-reset-target-disposable.sh) and add it to the allowlist in this script with a justification, or remove the invocation."
    fi
done

# ── Step 4: structural check on backend/Makefile — every `migrate ... down` recipe line must be
# immediately preceded, within its own recipe (i.e. after the most recent target header and before
# this line), by a call to the disposability gate. This is what catches the mutant that deletes the
# gate call but leaves the invocation line itself untouched. ──
if [ -f "$REPO_ROOT/backend/Makefile" ]; then
    MAKEFILE_LINES=()
    while IFS= read -r ml || [ -n "$ml" ]; do
        MAKEFILE_LINES+=("$ml")
    done < "$REPO_ROOT/backend/Makefile"
    total_lines=${#MAKEFILE_LINES[@]}
    for i in "${!FOUND_FILES[@]}"; do
        [ "${FOUND_FILES[$i]}" = "backend/Makefile" ] || continue
        ln="${FOUND_LINES[$i]}"
        # Find the most recent target header (a line matching `^[A-Za-z0-9_.-]+:`, not a recipe
        # line — recipe lines are tab-indented) at or before this invocation line.
        target_header_idx=-1
        idx=$((ln - 1)) # 0-based index of the invocation line itself
        for ((j = idx; j >= 0; j--)); do
            l="${MAKEFILE_LINES[$j]}"
            if [[ "$l" =~ ^[A-Za-z0-9_.%-]+:.*$ ]] && [[ "$l" != $'\t'* ]]; then
                target_header_idx=$j
                break
            fi
        done
        if [ "$target_header_idx" -lt 0 ]; then
            fail "backend/Makefile:$ln — could not find the enclosing target header to check for a preceding gate call"
            continue
        fi
        gated=false
        for ((j = target_header_idx; j < idx; j++)); do
            # Skip comment lines here too — a doc comment that merely NAMES the gate script (e.g.
            # "See scripts/assert-migrate-reset-target-disposable.sh for the full refusal design")
            # must not satisfy this check. Without this, deleting the actual gate-invocation line
            # while leaving an explanatory comment mentioning the same filename intact would pass
            # silently — exactly the shape of bypass this script exists to catch.
            if is_comment_line "${MAKEFILE_LINES[$j]}"; then
                continue
            fi
            if [[ "${MAKEFILE_LINES[$j]}" == *"assert-migrate-reset-target-disposable.sh"* ]]; then
                gated=true
                break
            fi
        done
        if [ "$gated" = false ]; then
            fail "backend/Makefile:$ln invokes 'migrate ... down' but its recipe carries no call to backend/scripts/assert-migrate-reset-target-disposable.sh ahead of it — the gate call was removed, or this is a new target that never had one."
        fi
    done
fi

# ── Step 5: structural check on scripts/test-migrations.sh — the three disposability/locality
# assertions rollback_migrations() has always relied on must still be CALLED (not merely defined)
# somewhere in the file. ──
#
# ⚠ Scope, deliberately: this checks presence of a bare call to each guard function, not that it
# runs before rollback_migrations() specifically. rollback_migrations() is a plain helper defined
# early in the file and invoked from main() much later, near the end — its physical source position
# (found above, ~line 892) is nowhere near its actual call order, so a line-number comparison
# against the guard calls (which live inside main(), later in the file) would either always fail
# (comparing the wrong things) or require real call-graph analysis this script does not do. That
# ordering is already covered, thoroughly, by this file's own dedicated regression suite —
# scripts/__tests__/test-migrations-target-guard.test.sh — including its own mutants. What this
# check adds on top is narrower and still real: if a future edit deletes one of the three assert
# calls outright (leaving only its definition, or removing it entirely), this fails immediately
# without needing that other suite's live-Postgres fixtures to notice.
if [ -f "$REPO_ROOT/scripts/test-migrations.sh" ]; then
    invokes_test_migrations_rollback=false
    for i in "${!FOUND_FILES[@]}"; do
        if [ "${FOUND_FILES[$i]}" = "scripts/test-migrations.sh" ]; then
            invokes_test_migrations_rollback=true
            break
        fi
    done
    if [ "$invokes_test_migrations_rollback" = true ]; then
        for fn in assert_host_is_local assert_target_is_disposable assert_target_cluster_is_disposable; do
            # A bare call is the whole line (nothing but optional leading whitespace, the function
            # name, and optionally trailing whitespace/a comment) — not the `fn() {` definition
            # line. The `$` below is grep -E's end-of-line anchor, not a literal dollar sign.
            call_line=$(grep -nE "^[[:space:]]*${fn}[[:space:]]*(#.*)?\$" "$REPO_ROOT/scripts/test-migrations.sh" | head -1 | cut -d: -f1 || true)
            if [ -z "$call_line" ]; then
                fail "scripts/test-migrations.sh invokes 'migrate ... down' (via rollback_migrations()) but no bare call to $fn() was found anywhere in the file — only a definition, or none at all. One of the three disposability/locality gates this file has always relied on was removed."
            fi
        done
    fi
fi

if [ "$FAILURES" -gt 0 ]; then
    echo "" >&2
    echo "$FAILURES ungated (or newly-ungated) 'migrate ... down' invocation(s) found. See failures above." >&2
    exit 1
fi

echo "OK: every 'migrate ... down' invocation in the tree is allowlisted and structurally gated (${#FOUND_FILES[@]} invocation(s) checked across ${#CANDIDATE_FILES[@]} candidate file(s))."
