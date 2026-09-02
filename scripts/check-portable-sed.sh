#!/usr/bin/env bash
# Guard: no non-portable in-place sed in this repo's cross-platform script corpus.
#
# `sed -i '' EXPR FILE` is macOS/BSD syntax; GNU sed (every Linux CI runner) reads the empty
# string as the script argument and fails. `sed -i EXPR FILE` is the mirror trap: GNU-only,
# fails on BSD. Both are rejected.
#
# This was a real blocker, not a hypothetical: four call sites in
# scripts/__tests__/no-ungated-migrate-down-invocation-guard.test.sh -- a suite that runs INSIDE
# the e2e-test job -- would have broken the moment that job moved to a Linux runner (#1598),
# while continuing to pass on the developer's Mac. The failure would have looked like a guard
# defect rather than a portability defect.
#
# The portable replacement, which behaves identically on BSD and GNU:
#     sed 'EXPR' "$f" > "$f.tmp" && cat "$f.tmp" > "$f" && rm -f "$f.tmp"
#
# ⚠ `cat "$f.tmp" > "$f"`, NOT `mv "$f.tmp" "$f"`. `mv` replaces the file, so the rewritten copy
# carries the temp file's umask-derived mode (0644) rather than the original's. That silently
# stripped the executable bit off scripts/test-migrations.sh when this guard's own fix was first
# applied -- a regression introduced by the portability fix itself. The BSD form being replaced
# edits in place and preserves the mode, so any replacement must too. Redirecting into the
# existing path keeps its inode, mode and ownership.
#
# ⚠ NO `mapfile`, `readarray` or `declare -A` below. macOS ships bash 3.2.57 as both /bin/bash
# and whatever `#!/usr/bin/env bash` resolves to on this repo's dev machines, and 3.2 predates
# all three. The same constraint is recorded at
# scripts/assert-no-ungated-migrate-down-invocation.sh:94-95 and scripts/check-ci-env-drift.sh:79-81.
# Getting this wrong is one-directional and easy to miss: a bash-4 guard passes in CI (bash 5)
# and breaks only in `make test-scripts` locally.
#
# CORPUS. Deliberately as wide as the sibling migrate-down guard's -- every tracked *.sh,
# Makefile, workflow, Dockerfile and extensionless shell-shebang script -- rather than *.sh
# alone. A `sed -i ''` added to a workflow `run:` body is exactly where a future runner
# migration would put one, and a *.sh-only scan cannot see it.
#
# EXCLUSIONS are paths RELATIVE TO THE SCAN ROOT, overridable via PORTABLE_SED_EXCLUDE. Both
# properties exist so a fixture can exercise the exclusion path: an earlier draft compared
# absolute fixture paths against a relative entry, which could never match, leaving the one axis
# that silently disables the whole guard untestable.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCAN_ROOT="${1:-$REPO_ROOT}"
SCAN_ROOT="$(cd "$SCAN_ROOT" && pwd)"

# Paths whose ONLY execution context is a Linux container, and which are therefore entitled to
# GNU sed. Each entry names its reason; a bare path list invites silent growth.
#
#   apps/web/docker-entrypoint.sh -- runs only as the nginx container's ENTRYPOINT, substituting
#     placeholders into /etc/nginx/nginx.conf. It never runs on a developer Mac, and never
#     directly on a CI runner. GNU `sed -i` is correct there.
#
#   apps/web/Dockerfile -- a RUN layer inside the same Linux image build, for the same reason.
#
#   scripts/__tests__/check-portable-sed.test.sh -- THIS guard's own regression suite. Its
#     fixtures must contain every rejected form verbatim; that is precisely what they test. Note
#     this is a narrower exclusion than the sibling migrate-down guard's, which excludes
#     __tests__/ and *.test.sh wholesale (see
#     scripts/assert-no-ungated-migrate-down-invocation.sh:87-92). That blanket rule must NOT be
#     copied here: the motivating defect for this guard was a non-portable sed inside
#     scripts/__tests__/no-ungated-migrate-down-invocation-guard.test.sh, a *.test.sh that runs
#     in CI, so excluding test files by pattern would have excluded the exact bug. A fixture that
#     merely CONTAINS the anti-pattern as data is fine; one that EXECUTES it is not, and only a
#     per-file judgement can tell those apart.
DEFAULT_EXCLUDE='apps/web/docker-entrypoint.sh
apps/web/Dockerfile
scripts/__tests__/check-portable-sed.test.sh'
EXCLUDE="${PORTABLE_SED_EXCLUDE-$DEFAULT_EXCLUDE}"

# Each alternative is present because it evades a narrower regex: `-i ''`/`-i ""` (BSD),
# `-i''` (the zero-width BSD idiom), `-i.bak` (GNU suffix), `--in-place`, and `-i` followed by
# the expression (GNU). Interleaved short flags such as `sed -n -i` are covered by the flag
# group. The portable temp-file form contains no `-i` at all and cannot match.
SED_RE="(^|[^A-Za-z0-9_./-])(g?sed)([[:space:]]+-[A-Za-z]+)*[[:space:]]+(-i([[:space:]]*(''|\"\")|\.[A-Za-z0-9]+|[[:space:]]+[^[:space:]])|--in-place)"

is_excluded() {
  [ -n "$EXCLUDE" ] || return 1
  printf '%s\n' "$EXCLUDE" | grep -Fxq "$1"
}

# Candidate set. In repo mode use git -- tracked files only, which is deliberate scope: this
# guard protects what CI runs, and an unstaged file is not that yet. In fixture mode walk the
# tree, because a fixture is not a git repo.
if [ "$SCAN_ROOT" = "$REPO_ROOT" ]; then
  CANDIDATES="$(cd "$REPO_ROOT" && git ls-files)"
else
  CANDIDATES="$(cd "$SCAN_ROOT" && find . -type f -print | sed 's|^\./||')"
fi

violations=0
scanned=0
while IFS= read -r rel; do
  [ -n "$rel" ] || continue
  f="$SCAN_ROOT/$rel"
  [ -f "$f" ] || continue

  case "$rel" in
    *.sh|Makefile|*/Makefile|.github/workflows/*.yml|.github/workflows/*.yaml|*Dockerfile|*Dockerfile.*) ;;
    *.*) continue ;;
    *) head -1 "$f" 2>/dev/null | grep -qE '^#!.*/(env +)?(ba)?sh' || continue ;;
  esac

  is_excluded "$rel" && continue
  scanned=$((scanned + 1))

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    # Skip comment lines. A comment that merely NAMES the anti-pattern is documentation, not an
    # execution path -- and without this, every file that explains the rule (including this
    # guard's own header) reports as a violation of it. Same belt-and-braces reasoning as
    # is_comment_line() in scripts/assert-no-ungated-migrate-down-invocation.sh:149.
    # The `N:` line-number prefix from `grep -n` is stripped before testing.
    case "${line#*:}" in
      \#*|[[:space:]]\#*|[[:space:]][[:space:]]*\#*) continue ;;
    esac
    printf '%s: %s\n' "$rel" "$line"
    violations=$((violations + 1))
  done <<EOF
$(grep -nE "$SED_RE" "$f" 2>/dev/null || true)
EOF
done <<EOF
$CANDIDATES
EOF

if [ "$violations" -gt 0 ]; then
  cat >&2 <<'MSG'

FAIL: non-portable in-place sed found (hits above).

Rejected forms (described rather than written out, so this message does not trip the
scan it belongs to):
  BSD-only:  -i followed by an empty '' argument, or -i'' with no space before it
  GNU-only:  -i followed directly by the expression, -i.SUFFIX, or --in-place

Use the portable form, which behaves identically on BSD and GNU:

  sed 'EXPR' "$file" > "$file.tmp" && cat "$file.tmp" > "$file" && rm -f "$file.tmp"

Use `cat ... > "$file"`, not `mv`: mv replaces the file and gives it the temp file's 0644
mode, silently stripping the executable bit. Redirecting into the existing path keeps it.

If a script's ONLY execution context is a Linux container, add it to DEFAULT_EXCLUDE with a
one-line reason instead.

MSG
  exit 1
fi

echo "check-portable-sed: OK ($scanned in-corpus files scanned, 0 violations)"
