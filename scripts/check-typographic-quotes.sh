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
#   any tracked *.go file under that path contains U+201C or U+201D;
#   exits 0 otherwise.
set -euo pipefail

SCAN_ROOT="${1:-backend}"

cd "$(git rev-parse --show-toplevel)"

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
# `|| true` because grep exits 1 on "no match", which is not an error here
# and must not trip `set -e`.
FILE_LIST="$(git ls-files -- "$SCAN_ROOT" | /usr/bin/grep '\.go$' || true)"

if [ -z "$FILE_LIST" ]; then
  echo "check-typographic-quotes: no tracked .go files under '$SCAN_ROOT' -- nothing to scan"
  exit 0
fi

# Deliberately NOT `"${FILES[@]}"` over a bash array: building and expanding
# an array to hold FILE_LIST risks the empty-array-under-`set -u` crash on
# macOS bash 3.2 (this repo has been bitten by exactly that). Piping the
# newline-separated list through xargs sidesteps arrays entirely.
#
# Captured into a variable rather than piped straight to a conditional:
# `cmd1 | cmd2` in a pipeline masks cmd1's exit status with cmd2's under
# `set -o pipefail`-less shells and vice versa, and `grep ... || true` on the
# right of a pipe would otherwise suspend `set -e` for the whole pipeline.
# Capturing first, then testing the captured *content* (not an exit code),
# avoids that class of bug.
# -H forces the filename prefix even though xargs -I{} invokes grep with a
# single filename argument at a time (grep omits the filename by default
# when given exactly one file, which would otherwise make every violation
# line unidentifiable).
VIOLATIONS="$(printf '%s\n' "$FILE_LIST" | LC_ALL=C xargs -I{} /usr/bin/grep -nHF -e "$LEFT_DQUOTE" -e "$RIGHT_DQUOTE" {} 2>/dev/null || true)"

if [ -n "$VIOLATIONS" ]; then
  echo "check-typographic-quotes: FAIL -- found U+201C/U+201D (typographic double quotes) in tracked .go files under '$SCAN_ROOT':"
  echo "$VIOLATIONS"
  echo ""
  echo "This is almost certainly gofmt -w corruption of an ASCII '' (or \`\`) pair inside a Go doc comment (#1487)."
  echo "Fix by hand: replace the character with a straight ASCII double quote (\") or restore the original '' / \`\` pair."
  echo "Do NOT run 'gofmt -w' to fix this -- that is the tool that produces this corruption."
  exit 1
fi

echo "check-typographic-quotes: PASS -- 0 occurrences of U+201C/U+201D in tracked .go files under '$SCAN_ROOT'"
exit 0
