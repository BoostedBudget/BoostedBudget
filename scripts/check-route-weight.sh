#!/usr/bin/env bash
#
# check-route-weight.sh — budget the LANDING ROUTE's eager download, not individual chunks.
#
# WHY THIS EXISTS
#
# Before this guard, `make check-bundle-size` passed with 847 KiB gzip on `/`, and it was not
# broken — it simply answers a different question. It has two parts and neither models a route:
#
#   * scripts/check-orphan-chunks.sh asserts every emitted chunk HAS a budget.
#   * size-limit checks each chunk IN ISOLATION against that budget.
#
# Nothing in the repo knew which chunks a route actually downloads, so a 216 KiB charting chunk
# sat on the landing page's critical path while every per-chunk budget stayed green. That blind
# spot is how the cascade documented in #1601 grew unnoticed, and it is the reason Lighthouse
# reported FCP 5.8s with total-blocking-time 0ms: the delay was a dependency chain, not work.
#
# The cascade's actual root turned out to be invisible to every existing check and to code review
# alike: react-datepicker declares date-fns-tz as an OPTIONAL peer, it was not installed, and
# rolldown materialised a throwing resolution stub for it inside the `admin` chunk — after which
# `vendor` statically imported that chunk, giving
# entry -> vendor -> admin -> household -> accounts -> investments -> charts. Installing the peer
# removed 307 KiB from this route. Only a route-level measurement surfaces that class of bug.
#
# WHAT IT MEASURES
#
# The eager closure the browser is TOLD to fetch before it can render `/`: the entry module
# plus every `<link rel="modulepreload">` the bundler emitted for the entry's static-import
# closure, plus the blocking stylesheets. Sizes are gzip, matching how size-limit measures, so
# a number here is comparable to a number in .size-limit.json.
#
# It deliberately does NOT try to model lazy routes. `React.lazy()` chunks are absent from this
# list, which is exactly the point: a chunk appearing here means it is NOT lazy, whatever the
# route table says. `features/admin` was reached through a barrel import while every admin ROUTE
# was lazy — the route table said lazy and the HTML said otherwise. The HTML is the authority.
#
# BASH 3.2 ONLY — macOS ships 3.2.57 and `make test-scripts` runs there. No mapfile/readarray,
# no `declare -A`, no `${var^^}`.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROUTE_WEIGHT_DIST_DIR:-${REPO_ROOT}/apps/web/dist}"
INDEX_HTML="${DIST_DIR}/index.html"

# Budget in KiB of gzip. Overridable for the regression suite; the committed default is the
# ratchet. Raising it is a decision that belongs in a commit message, not an env var.
#
# 541 is a RATCHET AT THE MEASURED VALUE, not a statement that 541 KiB is good. Measured
# 2026-09-02 after #1601 Task 2 at 553,584 bytes (540.61 KiB) across 10 assets, down from
# 867,876 bytes (847.54 KiB) across 20 — a 36% cut. 541 is that value rounded up, so any growth
# at all trips it.
#
# The original target was 400, and it is NOT reachable by removing eager app code, because
# `vendor` alone is 446 KiB of the remaining 541. Getting under 400 requires splitting vendor
# itself (dnd-kit and TanStack Table are only needed by lazy features), which is separate work.
# What remains here is close to the floor of the current chunking:
#   vendor 446 · shared 34 · css 18+3 · entry 14 · billing 11 · api-client 7 · shared-2 2
# billing is deliberate — SubscriptionManagementSkeleton and SubscriptionErrorState are a
# Suspense fallback and an error-boundary fallback, which must resolve synchronously.
BUDGET_KIB="${ROUTE_WEIGHT_BUDGET_KIB:-541}"

fail() {
  printf '❌ %s\n' "$1" >&2
  exit 1
}

# A missing dist must FAIL, never pass vacuously — a guard that silently succeeds when there is
# nothing to measure is worse than no guard, because CI goes green on an unbuilt tree.
[ -f "$INDEX_HTML" ] || fail "No built HTML at ${INDEX_HTML}
       Run \`bun run build\` in apps/web first. Refusing to pass without measuring anything."

# Extract the eager set. Three tag shapes, in one pass each, then dedupe: the entry module is
# emitted BOTH as <script type="module" src> and as a <link rel="modulepreload">, so without
# dedupe it would be counted twice.
#
# tr '<' '\n' first: these tags are minified onto few lines, and a line-oriented grep -o over
# the raw file is fine, but splitting on '<' keeps each match to exactly one tag and makes the
# rel= and type= tests unambiguous.
extract_refs() {
  tr '<' '\n' < "$INDEX_HTML" \
    | while IFS= read -r tag; do
        case "$tag" in
          script*type=\"module\"*src=\"*)  ;;
          link*rel=\"modulepreload\"*)     ;;
          link*rel=\"stylesheet\"*)        ;;
          *) continue ;;
        esac
        # Pull href="..." or src="..." — whichever this tag carries.
        #
        # ⚠ `grep -oE`, NOT `sed -n 's/...\(href\|src\)...'`. BRE alternation (`\|`) is a GNU
        # extension: BSD sed matches NOTHING and prints nothing, so the sed form returns an
        # empty set and the total reads 0 KiB. That is the one-directional portability trap in
        # CLAUDE.md — it would have passed on the Ubicloud (GNU) runner and under-reported only
        # on a developer's Mac. Caught here solely because this script refuses to treat an empty
        # parse as a pass; verify changes with /bin/bash (3.2.57) and BSD tools, not GNU.
        printf '%s\n' "$tag" \
          | /usr/bin/grep -oE '(href|src)="[^"]*"' \
          | /usr/bin/sed -e 's/^[a-z]*="//' -e 's/"$//'
      done \
    | /usr/bin/sed '/^$/d' \
    | sort -u
}

REFS="$(extract_refs)"

[ -n "$REFS" ] || fail "Parsed ${INDEX_HTML} but found no entry module, modulepreload or stylesheet.
       Either the build output shape changed or the file is truncated. Refusing to report 0 KiB
       as a pass — a parser that silently matches nothing looks identical to a tiny bundle."

# Sum gzip bytes, collecting a breakdown as we go. Newline-delimited "bytes<TAB>path" lines
# instead of an associative array, for bash 3.2.
TOTAL=0
BREAKDOWN=""
MISSING=""

while IFS= read -r ref; do
  [ -n "$ref" ] || continue
  # Refs are absolute site paths ("/assets/x.js"); dist/ is the site root.
  rel="${ref#/}"
  file="${DIST_DIR}/${rel}"
  if [ ! -f "$file" ]; then
    MISSING="${MISSING}${ref}
"
    continue
  fi
  # `gzip -c < "$file"`, NOT `gzip -c "$file"`. Given a named argument, gzip embeds the ORIGINAL
  # FILENAME in the member header, so the reported size would depend on how long the asset's name
  # is — a rename would move the number without a byte of code changing. Reading from stdin emits
  # no name, which makes this a measure of the content alone.
  bytes="$(gzip -c < "$file" | /usr/bin/wc -c | /usr/bin/tr -d ' ')"
  TOTAL=$((TOTAL + bytes))
  BREAKDOWN="${BREAKDOWN}${bytes}	${ref}
"
done <<EOF
$REFS
EOF

# A referenced-but-absent asset is a real defect (a stale index.html, or a build that did not
# finish), and silently skipping it would UNDERSTATE the route weight — the one direction a
# size guard must never err in.
if [ -n "$MISSING" ]; then
  printf '❌ index.html references assets that do not exist in %s:\n' "$DIST_DIR" >&2
  printf '%s' "$MISSING" | /usr/bin/sed '/^$/d;s/^/       /' >&2
  fail "Refusing to report a total that omits them."
fi

BUDGET_BYTES=$((BUDGET_KIB * 1024))
TOTAL_KIB=$((TOTAL / 1024))
COUNT="$(printf '%s' "$BREAKDOWN" | /usr/bin/sed '/^$/d' | /usr/bin/wc -l | /usr/bin/tr -d ' ')"

print_breakdown() {
  printf '%s' "$BREAKDOWN" \
    | /usr/bin/sed '/^$/d' \
    | sort -rn \
    | while IFS="$(printf '\t')" read -r bytes ref; do
        printf '       %8s KiB  %s\n' "$((bytes / 1024))" "$ref"
      done
}

if [ "$TOTAL" -gt "$BUDGET_BYTES" ]; then
  printf '❌ Landing route (/) eager download is %s KiB gzip across %s assets — over the %s KiB budget.\n' \
    "$TOTAL_KIB" "$COUNT" "$BUDGET_KIB" >&2
  printf '\n   Everything below is fetched BEFORE / can render, largest first:\n' >&2
  print_breakdown >&2
  printf '\n   This is a ROUTE budget, so the fix is almost never "raise the number" — it is to stop\n' >&2
  printf '   the landing entry statically importing the chunk. A chunk listed here is NOT lazy,\n' >&2
  printf '   whatever the route table says: a barrel import (`from '"'"'./features/x'"'"'`) pulls in a\n' >&2
  printf '   whole feature even when every route inside it is React.lazy(). Import the concrete\n' >&2
  printf '   module instead. See docs and #1601 for the three edges this guard was written for.\n' >&2
  exit 1
fi

printf '✅ Landing route (/) eager download: %s KiB gzip across %s assets (budget %s KiB)\n' \
  "$TOTAL_KIB" "$COUNT" "$BUDGET_KIB"
