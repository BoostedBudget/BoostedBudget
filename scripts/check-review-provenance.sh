#!/usr/bin/env bash
# check-review-provenance.sh — fail-closed guard for the mandatory adversarial review.
#
# THE PROBLEM IT SOLVES
# CLAUDE.md makes an adversarial (Codex, or documented fallback) review mandatory before
# a PR. On this branch it was skipped twice in an identical way: plan mode blocked the
# launch, the review was recorded as "owed as the first implementation step", and the
# round ended without it. Round 11 existed *because* round 10 skipped it; round 11 then
# shipped its five largest batches unreviewed. Two rounds of writing "mandatory" in prose
# did not work, so the review became a mechanical gate: this script.
#
# WHAT IT CHECKS
# A tracked provenance file records the last SHA that was actually adversarially reviewed,
# plus enough attribution to audit the claim afterwards. This script fails when HEAD has
# moved past that SHA in any way that could plausibly need review.
#
# HOW TO SATISFY IT (read this before deleting the guard)
#   1. Run the adversarial review over `<reviewed_sha>..HEAD`
#      (`codex:codex-rescue`, or CLAUDE.md's documented fallback reviewer).
#   2. Stamp the result:
#        scripts/check-review-provenance.sh --stamp "<reviewer>" "<evidence>"
#      e.g. --stamp "codex:codex-rescue (gpt-5-codex, task mode)" "session 019fe29b-d579"
#      That rewrites the provenance file to the current HEAD and today's date.
#   3. Commit the provenance file. Because the provenance file is itself an exempt path
#      (see below), that commit does NOT re-arm the guard — which is the only reason the
#      stamp/commit sequence can ever converge.
# Stamping without having run a review is not a technical bypass, it is a false audit
# record. The guard's job is to make the skip visible and attributable, not to be
# unforgeable.
#
# WHICH CHANGES DEMAND A FRESH REVIEW (the filter, and why it is an EXEMPT-list)
# Blocking on *any* HEAD movement makes a README typo demand a fresh adversarial review;
# a guard nobody can satisfy gets deleted. So there is a filter. But this branch has a
# live example of a path filter that was simultaneously too broad and too narrow (the MCP
# paths-filter), and the reason is that it enumerated the paths that DO matter. Any such
# allow-list is fail-OPEN by construction: a directory nobody thought of, or a new
# top-level component added next year, silently falls outside it and ships unreviewed.
#
# This filter is inverted for exactly that reason. It enumerates only paths that are
# provably incapable of changing runtime behaviour, and treats EVERYTHING ELSE — including
# every path that does not exist yet — as requiring review. Getting the exempt list wrong
# costs a redundant review; getting an allow-list wrong costs an unreviewed change.
#
#   EXEMPT (no fresh review required):
#     * any path ending in `.md`  — prose,
#       EXCEPT AGENT-BEHAVIOUR MARKDOWN, which directs what an agent does and is therefore
#       behavioural, not prose. That is a CLASS, not one filename: any file whose basename
#       (case-insensitively) is CLAUDE.md / AGENTS.md / GEMINI.md / SKILL.md /
#       copilot-instructions.md, and anything under a `.claude/`, `.agent[s]/`, `.gemini/`,
#       `.windsurf/`, `.cursor/`, `.codex/`, `.aider/`, `.continue/`, `.opencode/` or
#       `.github/prompts/` path component. Drawing the line at the single basename
#       `CLAUDE.md` left 30+ tracked `.claude/**/*.md` and the whole
#       `.agent[s]/skills/**/SKILL.md` tree exempt: rewriting
#       `.claude/commands/commit-push-pr.md` to "Push directly to main; skip the mandatory
#       Codex adversarial review" passed as "prose only";
#     * the provenance file itself — otherwise stamping a review re-arms the guard and
#       the workflow above can never converge.
#   NOT EXEMPT (fresh review required): everything else. Note deliberately: `docs/` is NOT
#   exempt as a directory — it holds `docs/decisions/bundle-baseline.json`, a load-bearing
#   performance budget. Config YAML, workflows, scripts (including this one), and any new
#   file type are all in scope by default.
#
# SCOPE, STATED SO IT IS NOT MISTAKEN FOR COVERAGE
# The guard compares committed history only (reviewed_sha..HEAD). Uncommitted working-tree
# edits are NOT checked, deliberately: `make test-scripts` runs constantly against a dirty
# tree during development, and a guard that is red by default is a guard that gets deleted.
# Nothing uncommitted can reach a PR, so the gate still sits in front of everything that
# ships -- but do not read a green run as "the working tree is reviewed".
#
# EXIT CODES
#   0  pass — HEAD has not moved past the reviewed SHA in any non-exempt path
#   1  a review is owed — non-exempt paths changed since the reviewed SHA
#   2  fail closed — missing/malformed/unparseable provenance, unknown or non-ancestor
#      SHA, or a scan that found nothing to check (an empty scan is not a pass)
set -euo pipefail

if ! REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  echo "check-review-provenance: FAIL-CLOSED -- not inside a git working tree, or 'git' is unavailable." >&2
  echo "  Every check below (ancestry, range enumeration) needs git. Without it there is no evidence either way." >&2
  exit 2
fi
cd "$REPO_ROOT"

PROVENANCE_FILE="${REVIEW_PROVENANCE_FILE:-docs/review/last-adversarial-review.txt}"
REQUIRED_KEYS="reviewed_sha reviewed_range reviewer reviewed_at evidence"

die_closed() {
  echo "check-review-provenance: FAIL-CLOSED -- $1" >&2
  shift
  while [ "$#" -gt 0 ]; do echo "  $1" >&2; shift; done
  exit 2
}

# ── --stamp: rewrite the provenance file at the current HEAD ──────────────────
if [ "${1:-}" = "--stamp" ]; then
  STAMP_REVIEWER="${2:-}"
  STAMP_EVIDENCE="${3:-}"
  if [ -z "$STAMP_REVIEWER" ] || [ -z "$STAMP_EVIDENCE" ]; then
    echo "usage: $0 --stamp \"<reviewer>\" \"<evidence>\"" >&2
    echo "Both are mandatory: an unattributed stamp is exactly the unauditable record this guard exists to prevent." >&2
    exit 2
  fi
  STAMP_HEAD="$(git rev-parse HEAD)"
  STAMP_PREV=""
  if [ -f "$PROVENANCE_FILE" ]; then
    STAMP_PREV="$(sed -n 's/^reviewed_sha:[[:space:]]*//p' "$PROVENANCE_FILE" | head -1)"
  fi
  [ -n "$STAMP_PREV" ] || STAMP_PREV="$(git rev-list --max-parents=0 HEAD | tail -1)"
  mkdir -p "$(dirname "$PROVENANCE_FILE")"
  {
    echo "# Last adversarially-reviewed commit. See scripts/check-review-provenance.sh."
    echo "# Update ONLY after a review has actually run, with:"
    echo "#   scripts/check-review-provenance.sh --stamp \"<reviewer>\" \"<evidence>\""
    echo ""
    echo "reviewed_sha: $STAMP_HEAD"
    echo "reviewed_range: ${STAMP_PREV}..${STAMP_HEAD}"
    echo "reviewer: $STAMP_REVIEWER"
    echo "reviewed_at: $(date -u +%Y-%m-%d)"
    echo "evidence: $STAMP_EVIDENCE"
  } > "$PROVENANCE_FILE"
  echo "check-review-provenance: stamped $PROVENANCE_FILE at $STAMP_HEAD"
  echo "Commit it. The provenance file is an exempt path, so committing it does not re-arm the guard."
  exit 0
fi

# ── 1. The file must exist ────────────────────────────────────────────────────
if [ ! -f "$PROVENANCE_FILE" ]; then
  die_closed "provenance file '$PROVENANCE_FILE' does not exist." \
    "No record of an adversarial review means no review can be assumed. This is a failure, not a pass." \
    "Run the review, then: $0 --stamp \"<reviewer>\" \"<evidence>\""
fi

# ── 2. It must parse: strict `key: value`, `#` comments and blank lines only ──
BODY="$(sed -e 's/[[:space:]]*$//' "$PROVENANCE_FILE" | /usr/bin/grep -v -e '^[[:space:]]*#' -e '^[[:space:]]*$' || true)"
if [ -z "$BODY" ]; then
  die_closed "provenance file '$PROVENANCE_FILE' is empty (or contains only comments)." \
    "An unparseable provenance record proves nothing about whether a review ran."
fi

while IFS= read -r line; do
  [ -n "$line" ] || continue
  # Strict: the text before the FIRST colon must be a bare [a-z_]+ key (no spaces, no
  # punctuation), the colon must be followed by a space, and the value must be non-empty.
  # A loose "contains a colon somewhere" check would accept an English sentence as a
  # key/value pair and then silently ignore it.
  LINE_KEY="${line%%:*}"
  case "$line" in
    *": "*) : ;;
    *) die_closed "provenance file '$PROVENANCE_FILE' has an unparseable line: $line" \
         "Expected strict 'key: value' (lowercase/underscore key, colon, single space), '#' comments, or blank lines." ;;
  esac
  case "$LINE_KEY" in
    "" | *[!a-z_]*)
      die_closed "provenance file '$PROVENANCE_FILE' has an unparseable line: $line" \
        "The text before the first colon ('$LINE_KEY') is not a bare lowercase/underscore key." ;;
  esac
done <<< "$BODY"
# Note: an empty value ("reviewed_sha: ") needs no separate check -- trailing whitespace
# is stripped above, so such a line becomes "reviewed_sha:" and fails the ": " test.

field() {
  # Prints the single value for a key, or nothing. Deliberately not `local x=$(...)`:
  # `local` swallows the command's exit status.
  sed -n "s/^$1:[[:space:]]*//p" <<< "$BODY"
}

for key in $REQUIRED_KEYS; do
  COUNT="$(field "$key" | /usr/bin/grep -c . || true)"
  if [ "$COUNT" -eq 0 ]; then
    die_closed "provenance file '$PROVENANCE_FILE' is missing required key '$key'." \
      "Required keys: $REQUIRED_KEYS"
  fi
  if [ "$COUNT" -gt 1 ]; then
    die_closed "provenance file '$PROVENANCE_FILE' declares '$key' $COUNT times." \
      "A record with two answers is not a record. Exactly one of each key is required."
  fi
done

REVIEWED_SHA="$(field reviewed_sha)"
REVIEWED_RANGE="$(field reviewed_range)"
REVIEWED_AT="$(field reviewed_at)"
REVIEWER="$(field reviewer)"
EVIDENCE="$(field evidence)"

case "$REVIEWED_SHA" in
  [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) : ;;
  *) die_closed "reviewed_sha '$REVIEWED_SHA' is not a full 40-character lowercase hex SHA." \
       "Abbreviated SHAs are ambiguous across history rewrites; the full SHA is required." ;;
esac

# reviewed_range: `<start>..<end>`. It was previously REQUIRED TO EXIST and NEVER READ, so
# the live record carried `reviewed_sha: 2060ef4b` while its own evidence said the review
# had covered `..127dc9c2` — a commit that 2060ef4b comes AFTER, not before. Two real
# bypasses (the shared-variable nil-ing and the merge commit) were sitting inside exactly
# that unreviewed gap, and no mechanism could ever surface the contradiction. A required
# field nobody reads is decoration; this reads it and fails closed when the two disagree.
# `##*..` (longest prefix) rather than `#*..`: on `A..B..C` a shortest-prefix strip leaves
# `B..C` as the "endpoint", which then fails the hex check with a misleading message
# instead of being reported as the malformed range it is.
RANGE_START="${REVIEWED_RANGE%%..*}"
RANGE_END="${REVIEWED_RANGE##*..}"
case "$REVIEWED_RANGE" in
  *".."*) : ;;
  *) die_closed "reviewed_range '$REVIEWED_RANGE' is not a '<start>..<end>' range." \
       "The range is what states WHAT WAS REVIEWED; an unparseable one cannot be checked against reviewed_sha." ;;
esac
case "$RANGE_START..$RANGE_END" in
  "$REVIEWED_RANGE") : ;;
  *) die_closed "reviewed_range '$REVIEWED_RANGE' contains more than one '..' separator." \
       "A range with two separators has no unambiguous endpoint." ;;
esac
for range_sha in "$RANGE_START" "$RANGE_END"; do
  case "$range_sha" in
    [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) : ;;
    *) die_closed "reviewed_range endpoint '$range_sha' is not a full 40-character lowercase hex SHA." \
         "Both ends of the reviewed range must be unambiguous commits." ;;
  esac
done
if [ "$RANGE_END" != "$REVIEWED_SHA" ]; then
  die_closed "reviewed_range ends at $RANGE_END but reviewed_sha is $REVIEWED_SHA." \
    "The two fields describe the same review and must agree. When they disagree, reviewed_sha claims" \
    "coverage the review never had, and everything between the range's real end and reviewed_sha is" \
    "unreviewed while the guard reports green. Set reviewed_sha to the commit that was ACTUALLY" \
    "reviewed -- do not widen the range to match the claim."
fi

case "$REVIEWED_AT" in
  [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) : ;;
  *) die_closed "reviewed_at '$REVIEWED_AT' is not an ISO-8601 date (YYYY-MM-DD)." ;;
esac

for placeholder in TODO todo tbd TBD unknown none n/a - ?; do
  if [ "$REVIEWER" = "$placeholder" ] || [ "$EVIDENCE" = "$placeholder" ]; then
    die_closed "reviewer/evidence is the placeholder '$placeholder'." \
      "An unattributed record is indistinguishable from no record."
  fi
done

# ── 3. The SHA must be a commit that actually exists ──────────────────────────
if ! git cat-file -e "${REVIEWED_SHA}^{commit}" 2>/dev/null; then
  die_closed "reviewed_sha $REVIEWED_SHA is not a commit in this repository." \
    "Either the record is fabricated or the commit was garbage-collected after a history rewrite." \
    "Re-review the current HEAD and re-stamp."
fi

if ! git cat-file -e "${RANGE_START}^{commit}" 2>/dev/null; then
  die_closed "reviewed_range starts at $RANGE_START, which is not a commit in this repository." \
    "A range whose start does not exist describes no review at all."
fi
if ! git merge-base --is-ancestor "$RANGE_START" "$RANGE_END"; then
  die_closed "reviewed_range $REVIEWED_RANGE runs backwards: $RANGE_START is not an ancestor of $RANGE_END." \
    "A '<start>..<end>' range that is not ancestor-ordered enumerates commits nobody reviewed."
fi

# ── 4. …and an ancestor of HEAD. Uses rev-list/merge-base, never `git log`: ───
# `git log --oneline` has silently printed fewer commits than `rev-list --count`
# reported for the same range in this environment, losing an entire batch.
HEAD_SHA="$(git rev-parse HEAD)"
if ! git merge-base --is-ancestor "$REVIEWED_SHA" "$HEAD_SHA"; then
  die_closed "reviewed_sha $REVIEWED_SHA is NOT an ancestor of HEAD ($HEAD_SHA)." \
    "History was rewritten, or the record points at an unrelated branch. Everything reachable from HEAD is therefore unreviewed." \
    "Re-review and re-stamp."
fi

AHEAD="$(git rev-list --count "${REVIEWED_SHA}..${HEAD_SHA}")"
if [ "$AHEAD" -eq 0 ]; then
  echo "check-review-provenance: PASS -- HEAD ($HEAD_SHA) is exactly the reviewed commit."
  echo "  reviewer: $REVIEWER"
  echo "  reviewed_at: $REVIEWED_AT / evidence: $EVIDENCE"
  exit 0
fi

# ── 5. Enumerate what changed. An empty enumeration is NOT a pass. ────────────
# The scanned set is the UNION OF EACH COMMIT'S OWN DIFF, not the endpoint diff
# `git diff <reviewed> HEAD`. An adversarial review of this guard's first draft (Codex,
# this branch) demonstrated why with a working bypass: commit an unreviewed behaviour
# change, then in a second commit revert it while touching only README.md. The endpoint
# diff then shows one exempt path and the guard exits 0, while an unreviewed
# behaviour-changing commit sits in the range and is reachable by bisect and by deploy.
#
# --no-renames deliberately, on each commit: with rename detection ON, `--name-only`
# prints only the DESTINATION path, so `git mv scripts/runtime.sh docs/runtime.md`
# surfaces one exempt .md and hides the executable's disappearance (Codex's other
# demonstrated bypass). --no-renames prints both sides.
#
# core.quotePath is left at its default (on): a path with non-ASCII or control bytes is
# printed C-quoted with a trailing '"', which can never match the `.md` suffix test, so
# such paths land in the requires-review bucket. That bias is deliberate -- quoting can
# only ever make a path look LESS exempt, never more.
#
# Commits are enumerated with `git rev-list`, never `git log`, which has silently
# printed fewer commits than its own `--count` reported for the same range here.
COMMITS="$(git rev-list "${REVIEWED_SHA}..${HEAD_SHA}")"
COMMIT_COUNT="$(/usr/bin/grep -c . <<< "$COMMITS" || true)"
if [ "$COMMIT_COUNT" -ne "$AHEAD" ]; then
  die_closed "'git rev-list' enumerated $COMMIT_COUNT commit(s) but 'git rev-list --count' says $AHEAD." \
    "The two disagree, so neither can be trusted. Failing closed rather than scanning a partial range."
fi

#
# MERGE COMMITS ARE DIFFED AGAINST EVERY PARENT, NOT LEFT TO THE DEFAULT.
# `git diff-tree <merge>` with no -m/-c/--cc prints NOTHING at all, so the loop below used
# to `continue` past every merge and a merge commit's OWN content was never scanned. That
# is a working bypass, demonstrated end to end: branch, commit a docs-only change, then
# `git merge --no-ff --no-commit` and rewrite a .go file inside the merge commit itself.
# The guard reported `PASS ... all 2 exempt`, exit 0, while `git diff <reviewed>..HEAD`
# proved the .go change had landed.
#
# The fix scans a merge as the UNION of its diff against EACH parent, rather than with
# `--cc`. `--cc`/`-c` print only paths that differ from ALL parents and then prune further
# on "uninteresting" hunks; relying on those semantics is exactly the kind of clever filter
# this guard keeps finding holes in. A union over parents cannot be fail-open: any path the
# merge resolved differently from any parent is surfaced. It over-reports — an ordinary
# merge lists everything the other side brought in — and that is the correct direction to
# err in, per the exempt-list rationale above: a redundant review, never a silent one.
CHANGED=""
while IFS= read -r commit; do
  [ -n "$commit" ] || continue
  # `<sha> <parent1> [<parent2> ...]`; strip the commit's own sha to get the parent list.
  COMMIT_PARENTS="$(git rev-list --parents -n 1 "$commit")"
  COMMIT_PARENTS="${COMMIT_PARENTS#"$commit"}"
  PARENT_COUNT=0
  # Unquoted on purpose: the value is a space-separated list of SHAs, and word splitting
  # is exactly what is wanted. SHAs cannot contain spaces or glob characters.
  for parent in $COMMIT_PARENTS; do
    PARENT_COUNT=$((PARENT_COUNT + 1))
  done
  if [ "$PARENT_COUNT" -eq 0 ]; then
    # A PARENTLESS commit prints NOTHING without --root — the same silent-skip class as
    # the merge bypass, one level over, and reachable the same way: `git checkout --orphan`
    # plus `git merge --allow-unrelated-histories` puts a second root inside the range.
    # Verified: `git diff-tree --no-commit-id --name-only -r <root>` prints an empty string;
    # with --root it prints the root's whole tree.
    PER_COMMIT="$(git diff-tree --no-commit-id --name-only -r --no-renames --root "$commit")"
  elif [ "$PARENT_COUNT" -eq 1 ]; then
    PER_COMMIT="$(git diff-tree --no-commit-id --name-only -r --no-renames "$commit")"
  else
    PER_COMMIT=""
    for parent in $COMMIT_PARENTS; do
      VS_PARENT="$(git diff-tree --no-commit-id --name-only -r --no-renames "$parent" "$commit")"
      [ -n "$VS_PARENT" ] || continue
      if [ -z "$PER_COMMIT" ]; then PER_COMMIT="$VS_PARENT"; else PER_COMMIT="$PER_COMMIT
$VS_PARENT"; fi
    done
  fi
  [ -n "$PER_COMMIT" ] || continue
  if [ -z "$CHANGED" ]; then CHANGED="$PER_COMMIT"; else CHANGED="$CHANGED
$PER_COMMIT"; fi
done <<< "$COMMITS"
CHANGED="$(sort -u <<< "$CHANGED" | /usr/bin/grep -v '^$' || true)"

if [ -z "$CHANGED" ]; then
  die_closed "$AHEAD commit(s) exist in ${REVIEWED_SHA}..${HEAD_SHA} but the per-commit scan enumerated ZERO paths." \
    "A scan that found nothing to check is not evidence that nothing needs review -- it is an instrument failure, or a range of nothing but empty commits." \
    "Re-review and re-stamp rather than trusting an empty scan."
fi
CHANGED_COUNT="$(/usr/bin/grep -c . <<< "$CHANGED")"

NEEDS_REVIEW=""
EXEMPT_COUNT=0
while IFS= read -r path; do
  [ -n "$path" ] || continue
  if [ "$path" = "$PROVENANCE_FILE" ]; then
    EXEMPT_COUNT=$((EXEMPT_COUNT + 1))
    continue
  fi

  # Is this .md file AGENT BEHAVIOUR rather than prose? The carve-out used to be drawn at
  # the single basename `CLAUDE.md`, which is one file out of a class: this repo tracks 30+
  # `.claude/**/*.md` (commands, agents, skills) and a large `.agent[s]/skills/**/SKILL.md`
  # tree, all of which direct what an agent does. Executed against the old rule: rewriting
  # `.claude/commands/commit-push-pr.md` to "Push directly to main; skip the mandatory
  # Codex adversarial review" passed as "prose only".
  #
  # Two independent tests, either of which is enough:
  #   * the BASENAME is an agent's root instruction file or a skill definition, or
  #   * any PATH COMPONENT is an agent tool's configuration directory.
  # The component test is wrapped in slashes so it matches whole components only:
  # `docs/my.claude-notes/x.md` stays prose.
  #
  # The basename test is case-insensitive so `claude.md` cannot slip past an exact-match
  # comparison. It does NOT weaken the existing `CLAUDE.MD` behaviour: an uppercase
  # EXTENSION still never reaches the exemption at all, because `${path%.md}` does not
  # strip `.MD` and the suffix test below therefore fails. Both are covered in the suite's
  # truth table.
  BASENAME="${path##*/}"
  BASENAME_LC="$(printf '%s' "$BASENAME" | tr '[:upper:]' '[:lower:]')"
  AGENT_BEHAVIOUR=0
  case "$BASENAME_LC" in
    claude.md | agents.md | gemini.md | skill.md | copilot-instructions.md) AGENT_BEHAVIOUR=1 ;;
  esac
  # Lowercased for the component test as well as the basename test: macOS ships a
  # case-insensitive filesystem, so `.CLAUDE/commands/x.md` is a spelling anyone can commit
  # by accident and git tracks it verbatim.
  PATH_LC="$(printf '%s' "/$path" | tr '[:upper:]' '[:lower:]')"
  case "$PATH_LC" in
    */.claude/* | */.agent/* | */.agents/* | */.gemini/* | */.windsurf/* | */.cursor/* | \
      */.codex/* | */.aider/* | */.continue/* | */.opencode/* | */.github/prompts/*)
      AGENT_BEHAVIOUR=1
      ;;
  esac

  if [ "$AGENT_BEHAVIOUR" -eq 0 ] && [ "${path%.md}" != "$path" ]; then
    EXEMPT_COUNT=$((EXEMPT_COUNT + 1))
    continue
  fi
  if [ -z "$NEEDS_REVIEW" ]; then NEEDS_REVIEW="$path"; else NEEDS_REVIEW="$NEEDS_REVIEW
$path"; fi
done <<< "$CHANGED"

if [ -n "$NEEDS_REVIEW" ]; then
  NEEDS_COUNT="$(/usr/bin/grep -c . <<< "$NEEDS_REVIEW")"
  echo "check-review-provenance: FAIL -- $NEEDS_COUNT of $CHANGED_COUNT changed path(s) since the last" >&2
  echo "adversarially-reviewed commit require a fresh review ($EXEMPT_COUNT exempt)." >&2
  echo "" >&2
  echo "  last reviewed: $REVIEWED_SHA ($REVIEWED_AT, $REVIEWER)" >&2
  echo "  HEAD:          $HEAD_SHA ($AHEAD commit(s) ahead)" >&2
  echo "" >&2
  while IFS= read -r offender; do echo "  $offender" >&2; done <<< "$NEEDS_REVIEW"
  echo "" >&2
  echo "Run the adversarial review over ${REVIEWED_SHA}..${HEAD_SHA}, then:" >&2
  echo "  $0 --stamp \"<reviewer>\" \"<evidence>\"" >&2
  exit 1
fi

echo "check-review-provenance: PASS -- $AHEAD commit(s) since $REVIEWED_SHA touched $CHANGED_COUNT path(s), all $EXEMPT_COUNT exempt (prose/provenance only)."
echo "  reviewer: $REVIEWER"
echo "  reviewed_at: $REVIEWED_AT / evidence: $EVIDENCE"
exit 0
