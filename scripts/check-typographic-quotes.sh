#!/usr/bin/env bash
# check-typographic-quotes.sh
#
# Guard against #1487's corruption: `gofmt -w` rewrites the two-character ASCII
# sequence `''` into a single U+201D RIGHT DOUBLE QUOTATION MARK, and ` `` `
# into a single U+201C LEFT DOUBLE QUOTATION MARK, INSIDE Go doc comments only
# (go/doc/comment's legacy godoc "quote-pair" typographic convention, applied
# by gofmt's doc-comment reformatting since Go 1.19). It hit six files' worth
# of SQL documentation in this repo -- doc comments describing `<> ''` /
# `= ''` empty-string predicates get rewritten into text that reads as invalid
# SQL. It cannot reach production behaviour: raw and interpreted string
# literals are byte-identical before and after, only comments convert. But it
# silently mangles documentation, and nothing else in this repo's tooling
# would have caught it (see #1487).
#
# ---------------------------------------------------------------------------
# WHY THIS IS NOT `gofmt -l`, AND WHY THAT MATTERS MORE THAN THE SCRIPT ITSELF
# ---------------------------------------------------------------------------
# `gofmt -l` lists files that differ from gofmt's own opinion of correct
# formatting. After the #1487 repair, the CORRECTED files -- the ones with
# the corruption undone, straight ASCII quotes restored -- are exactly the
# files gofmt considers unformatted, because gofmt's doc-comment pass still
# wants to convert those straight quotes back into curly ones. Confirmed
# directly against this repo:
#
#   $ gofmt -l backend/internal/repository/analytics_repository.go
#   backend/internal/repository/analytics_repository.go
#
# That file is the corrected one -- 0 occurrences of U+201C/U+201D on this
# branch, 1 on `main`. `gofmt -l` still flags it. Wiring `gofmt -l` in as the
# guard would therefore fail on the fixed state and pass on the corrupted
# one: the formatter and the fix point in opposite directions, so the
# formatter cannot be the instrument that verifies the fix. This script does
# not ask "is this file gofmt-clean" at all -- it asks the narrower question
# directly, by scanning bytes, which is orthogonal to gofmt's formatting
# opinion in either direction.
#
# ---------------------------------------------------------------------------
# SCOPE: tracked backend/**/*.go files only -- not the whole repo, not a
# filesystem walk.
# ---------------------------------------------------------------------------
# - `.go` only: gofmt's doc-comment rewrite can only ever touch `.go` files.
#   Widening to "all tracked text" would flag legitimate typographic quotes
#   in prose (markdown docs, this very plan file, etc.) that gofmt never
#   touches and that are not this hazard.
# - `backend/` only: matches the brief and the hazard's actual location
#   (Go doc comments live in backend/, not apps/web or packages/).
# - `git ls-files`, not `find`/a filesystem walk: vendored or generated
#   content isn't tracked, so a filesystem walk would produce noise a
#   git-ls-files scope doesn't. It also means an untracked scratch file
#   containing the character never fails the build -- only a committed one
#   does, which is the actual thing worth gating.
#
# Usage: scripts/check-typographic-quotes.sh [path-under-repo-root]
#   Defaults to "backend". Exits 1 and prints every offending file:line if
#   any tracked *.go file under that path contains U+201C or U+201D; exits 0
#   otherwise. Exits 1 if the scan root resolves to ZERO tracked .go files --
#   a scan of nothing proves nothing, so that is a failure, not a pass (see
#   "THREE WAYS THIS GUARD USED TO PASS VACUOUSLY" below). Exits >=2 if grep
#   itself could not run (missing/broken binary) -- that must not be
#   silently treated as "0 occurrences found".
#
# ---------------------------------------------------------------------------
# THREE WAYS THIS GUARD USED TO PASS VACUOUSLY (fixed, kept here so the next
# person doesn't reintroduce one of them)
# ---------------------------------------------------------------------------
# 1. Empty scan root: if "backend" is renamed/moved or the argument is wrong,
#    the old script printed "nothing to scan" and exited 0. Now: exit 1.
# 2. Missing grep: both scans used to be `... || true`, so a `127` (grep not
#    found) produced an empty result set indistinguishable from "0 matches".
#    Now: grep's exit status is inspected explicitly -- 0 (matched) and 1 (no
#    match) are the only statuses treated as a completed scan; anything else
#    is a hard failure (exit 2), not a PASS.
# 3. `core.quotePath`: `git ls-files` C-quotes non-ASCII paths by default
#    (e.g. `"backend/f\303\251.go"`), so a plain `grep '\.go$'` filter never
#    matches the trailing quote and the file is silently dropped from the
#    scan. Now: `git -c core.quotePath=false ls-files` disables the quoting.
set -euo pipefail

SCAN_ROOT="${1:-backend}"

cd "$(git rev-parse --show-toplevel)"

# grep binary for both passes below. Hard-coded to /usr/bin/grep for every
# real invocation of this script. The override exists ONLY so this script's
# own regression suite (scripts/__tests__/check-typographic-quotes.test.sh)
# can simulate "grep could not run" -- there is no way to make the real
# /usr/bin/grep vanish from a test sandbox, so the test points this variable
# at a path that does not exist instead. Do not set this outside that suite.
GREP_BIN="${CHECK_TYPOGRAPHIC_QUOTES_GREP_BIN:-/usr/bin/grep}"

# U+201C LEFT DOUBLE QUOTATION MARK  = UTF-8 bytes E2 80 9C
# U+201D RIGHT DOUBLE QUOTATION MARK = UTF-8 bytes E2 80 9D
# Passed to grep -F (fixed-string, no regex metacharacter risk) as literal
# byte sequences under LC_ALL=C, so the match is a byte comparison and does
# not depend on the invoking shell or grep's locale correctly decoding
# multi-byte UTF-8 characters.
LEFT_DQUOTE=$'\xe2\x80\x9c'
RIGHT_DQUOTE=$'\xe2\x80\x9d'

# Tracked files only (git ls-files), filtered to *.go -- NOT a filesystem
# walk, so untracked/vendored/generated content produces no noise.
# `-c core.quotePath=false` stops git from C-quoting non-ASCII paths (see
# defect 3 above) -- without it, a tracked file with a non-ASCII name is
# printed as `"backend/f\303\251.go"` (with a literal trailing double-quote
# byte), which the `\.go$` filter below never matches, so the file is
# silently never scanned.
#
# grep's own exit status is captured explicitly instead of `... || true`:
# grep exits 0 (matched a line), 1 (no line matched -- a normal, expected
# outcome here), or >=2 (a real error: binary missing, bad pattern,
# permission denied). Collapsing all three into "assume empty" is exactly
# the fail-open bug this guard exists to avoid making of ITSELF (defect 2).
# `set +e`/`set -e` bracket the one statement whose status we need to
# inspect by hand, rather than `cmd || true`, which would suspend `set -e`
# for the whole left-hand operand for the rest of the script.
set +e
FILE_LIST="$(git -c core.quotePath=false ls-files -- "$SCAN_ROOT" | "$GREP_BIN" '\.go$')"
FILTER_RC=$?
set -e

if [ "$FILTER_RC" -gt 1 ]; then
  echo "check-typographic-quotes: ERROR -- '$GREP_BIN' exited $FILTER_RC (expected 0 or 1) while filtering tracked files to *.go." >&2
  echo "This usually means the grep binary is missing or broken, not that there is nothing to scan. Failing closed, not reporting a false PASS." >&2
  exit 2
fi

if [ -z "$FILE_LIST" ]; then
  echo "check-typographic-quotes: FAIL -- no tracked .go files found under '$SCAN_ROOT'." >&2
  echo "A scan of zero files proves nothing about #1487-style corruption, so this is a failure, not a pass." >&2
  echo "Check that '$SCAN_ROOT' is still the correct, current path (not renamed, moved, or a typo) and that this script's argument is right." >&2
  exit 1
fi

# Deliberately NOT `"${FILES[@]}"` over a bash array: building and expanding
# an array to hold FILE_LIST risks the empty-array-under-`set -u` crash on
# macOS bash 3.2 (this repo has been bitten by exactly that). A `while read`
# loop over a here-string sidesteps arrays entirely and, unlike piping into
# `xargs`, lets each invocation's own exit status be inspected directly
# rather than reconstructed from xargs' aggregate (123/124/126/127) mapping.
VIOLATIONS=""
GREP_ERROR=0
while IFS= read -r go_file; do
  [ -n "$go_file" ] || continue
  set +e
  MATCH="$("$GREP_BIN" -nHF -e "$LEFT_DQUOTE" -e "$RIGHT_DQUOTE" -- "$go_file")"
  SCAN_RC=$?
  set -e
  case "$SCAN_RC" in
    0)
      if [ -z "$VIOLATIONS" ]; then
        VIOLATIONS="$MATCH"
      else
        VIOLATIONS="$VIOLATIONS
$MATCH"
      fi
      ;;
    1) : ;; # no U+201C/U+201D in this file -- expected, not an error
    *)
      GREP_ERROR=1
      echo "check-typographic-quotes: ERROR -- '$GREP_BIN' exited $SCAN_RC (expected 0 or 1) scanning '$go_file'." >&2
      ;;
  esac
done <<< "$FILE_LIST"

if [ "$GREP_ERROR" -eq 1 ]; then
  echo "check-typographic-quotes: ERROR -- grep failed during the content scan; failing closed rather than reporting a false PASS." >&2
  exit 2
fi

if [ -n "$VIOLATIONS" ]; then
  echo "check-typographic-quotes: FAIL -- found U+201C/U+201D (typographic double quotes) in tracked .go files under '$SCAN_ROOT':"
  echo "$VIOLATIONS"
  echo ""
  echo "This is almost certainly gofmt -w corruption of an ASCII '' (or \`\`) pair inside a Go doc comment (#1487)."
  echo "Fix by hand: replace the character with a straight ASCII double quote (\") or restore the original '' / \`\` pair."
  echo "Do NOT run 'gofmt -w' to fix this -- that is the tool that produces this corruption."
  exit 1
fi

FILE_COUNT="$(printf '%s\n' "$FILE_LIST" | wc -l | tr -d ' ')"
echo "check-typographic-quotes: PASS -- 0 occurrences of U+201C/U+201D across $FILE_COUNT tracked .go files under '$SCAN_ROOT'"
exit 0
