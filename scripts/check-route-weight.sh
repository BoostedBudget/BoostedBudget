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
# 542 is a RATCHET AT THE MEASURED VALUE, not a statement that 542 KiB is good. Measured
# 2026-09-02 at 554,030 bytes (541.04 KiB) across 11 local assets, down from 867,876 bytes
# (847.54 KiB) across 20 — a 36% cut. 542 is that value rounded up, so any growth at all trips
# it. (11, not 10: the #1601 review found the first version omitted parser-blocking classic
# scripts, so /config.js — 322 gzip bytes — is now correctly inside the total.)
#
# ⚠ Raised 541 -> 542 for +68 bytes, deliberately and once. AuthReadyGate (apps/web/src/main.tsx)
# used to render `null` while Clerk was in flight, so NOTHING painted and FCP could not fire; it
# now paints a small header shell. Those 68 gzip bytes buy first paint on every route, which is
# the constraint #1601 could not move by cutting 307 KiB of payload. This is the rare case where
# the number should go up: the guard fired, the growth was inspected, and it is the fix rather
# than a regression. Re-derive from a fresh build before ever raising it again — do not nudge it
# to make a red build green.
#
# ⚠ UNITS. This budget is BINARY KiB (1024). `.size-limit.json` next door uses `KB` which
# bytes-iec parses as DECIMAL 1000, so the two files' numbers are NOT interchangeable: 541 here is
# 553,984 bytes, while "541 KB" there would be 541,000. Compare bytes, never the labels, when
# reconciling a chunk budget against this route total.
#
# The original target was 400, and it is NOT reachable by removing eager app code, because
# `vendor` alone is 446 KiB of the remaining 541. Getting under 400 requires splitting vendor
# itself (dnd-kit and TanStack Table are only needed by lazy features), which is separate work.
# What remains here is close to the floor of the current chunking:
#   vendor 446 · shared 34 · css 18+3 · entry 14 · billing 11 · api-client 7 · shared-2 2
# billing is deliberate — SubscriptionManagementSkeleton and SubscriptionErrorState are a
# Suspense fallback and an error-boundary fallback, which must resolve synchronously.
BUDGET_KIB="${ROUTE_WEIGHT_BUDGET_KIB:-542}"

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
# Tag shapes that block or precede the first paint. #1601 review finding 2: the first version
# accepted only module scripts, modulepreloads and stylesheets, which UNDER-REPORTED the route —
# index.html also carries `/config.js` and the Paddle SDK as parser-blocking CLASSIC scripts
# (no async, no defer), and those gate discovery of our own JS. A guard that under-reports a size
# is worse than none, so classic blocking scripts and `<link rel=preload>` are in the universe too.
#
# A classic script counts only when it has NEITHER async NOR defer — those genuinely do not block
# the parser, and counting them would overstate the route just as badly as the old form
# understated it.
extract_refs() {
  tr '<' '\n' < "$INDEX_HTML" \
    | while IFS= read -r tag; do
        case "$tag" in
          script*type=\"module\"*src=\"*)  ;;
          link*rel=\"modulepreload\"*)     ;;
          link*rel=\"stylesheet\"*)        ;;
          link*rel=\"preload\"*)           ;;
          script*async*) continue ;;
          script*defer*) continue ;;
          script*src=\"*)                   ;;
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

# SECOND, INDEPENDENT extraction (#1601 review finding 3). The invariant further down proves the
# summing loop consumed every ref it was GIVEN; it cannot notice that the ref list itself was cut
# short, because a truncated list is self-consistent. A review mutant that appended `head -4` to
# extract_refs therefore reported "52 KiB across 4 assets" and exited 0 against a 10-asset tree.
#
# This counts the same thing by a deliberately DIFFERENT route — one grep pipeline over the whole
# file instead of tr + a per-tag case statement. The two must agree exactly. Truncation, a broken
# case pattern or a lost tag class in either implementation now disagrees with the other, and
# disagreement fails. Keep them independent: if you "simplify" this to call extract_refs, the
# cross-check becomes a tautology and this whole class of bug reopens.
independent_ref_count() {
  /usr/bin/grep -oE '<(script|link)[^>]*>' "$INDEX_HTML" \
    | /usr/bin/grep -vE '(async|defer)[ >=]' \
    | /usr/bin/grep -E 'rel="(modulepreload|stylesheet|preload)"|src="' \
    | /usr/bin/grep -oE '(href|src)="[^"]*"' \
    | /usr/bin/sed -e 's/^[a-z]*="//' -e 's/"$//' \
    | /usr/bin/sed '/^$/d' \
    | sort -u \
    | /usr/bin/wc -l \
    | /usr/bin/tr -d ' '
}

REFS="$(extract_refs)"

[ -n "$REFS" ] || fail "Parsed ${INDEX_HTML} but found no entry module, modulepreload or stylesheet.
       Either the build output shape changed or the file is truncated. Refusing to report 0 KiB
       as a pass — a parser that silently matches nothing looks identical to a tiny bundle."

INDEPENDENT_COUNT="$(independent_ref_count)"
PARSED_COUNT="$(printf '%s\n' "$REFS" | /usr/bin/sed '/^$/d' | /usr/bin/wc -l | /usr/bin/tr -d ' ')"
if [ "$PARSED_COUNT" -ne "$INDEPENDENT_COUNT" ]; then
  fail "Two independent parses of ${INDEX_HTML} disagree: the tag walker found ${PARSED_COUNT}
       asset reference(s), the grep pipeline found ${INDEPENDENT_COUNT}. One of them is wrong, so
       the total would be untrustworthy in an unknown direction. Refusing to report it."
fi

# Parser-blocking THIRD-PARTY origins we already ship and have accepted. A cross-origin script
# cannot be weighed from disk, so it is never part of the byte total — but a NEW one appearing is
# exactly the regression this guard should catch (a third-party connection gating discovery of our
# own JS is what Lighthouse charges 313ms of critical-path latency for), so an unlisted origin is
# a hard failure rather than a silent omission.
KNOWN_BLOCKING_THIRD_PARTY="cdn.paddle.com"

# Sum gzip bytes, collecting a breakdown as we go. Newline-delimited "bytes<TAB>path" lines
# instead of an associative array, for bash 3.2.
TOTAL=0
BREAKDOWN=""
MISSING=""
REMOTE=""
UNKNOWN_REMOTE=""
REF_COUNT=0
PROCESSED=0

# ⚠ An IFS-newline `for`, NOT `while read ... done <<EOF`. #1601 review finding 1: a here-document
# needs a temp file, and when Bash cannot create one (read-only or restricted TMPDIR, a sandbox,
# a full disk) the redirection fails WITHOUT `set -e` stopping the script — the loop body simply
# never runs and the guard printed "0 KiB gzip across 0 assets" and exited 0. That is the precise
# fail-open the empty-parse check above exists to prevent, arriving through a different door. This
# form touches no temp file, and the PROCESSED/REF_COUNT invariant below closes the class rather
# than this one instance.
OLD_IFS="$IFS"
IFS='
'
for ref in $REFS; do
  IFS="$OLD_IFS"
  [ -n "$ref" ] || continue
  REF_COUNT=$((REF_COUNT + 1))
  # Cross-origin refs (https://…, //…) cannot be weighed from disk.
  case "$ref" in
    http://*|https://*|//*)
      host="$(printf '%s' "$ref" | /usr/bin/sed -e 's|^https\{0,1\}://||' -e 's|^//||' -e 's|/.*$||')"
      REMOTE="${REMOTE}${ref}
"
      if ! printf '%s\n' "$KNOWN_BLOCKING_THIRD_PARTY" | /usr/bin/grep -Fxq "$host"; then
        UNKNOWN_REMOTE="${UNKNOWN_REMOTE}${host}	${ref}
"
      fi
      PROCESSED=$((PROCESSED + 1))
      IFS='
'
      continue
      ;;
  esac
  # Refs are absolute site paths ("/assets/x.js"); dist/ is the site root.
  rel="${ref#/}"
  file="${DIST_DIR}/${rel}"
  if [ ! -f "$file" ]; then
    MISSING="${MISSING}${ref}
"
    PROCESSED=$((PROCESSED + 1))
    IFS='
'
    continue
  fi
  # `gzip -c < "$file"`, NOT `gzip -c "$file"`. Given a named argument, gzip embeds the ORIGINAL
  # FILENAME in the member header, so the reported size would depend on how long the asset's name
  # is — a rename would move the number without a byte of code changing. Reading from stdin emits
  # no name, which makes this a measure of the content alone.
  bytes="$(gzip -c < "$file" | /usr/bin/wc -c | /usr/bin/tr -d ' ')"
  TOTAL=$((TOTAL + bytes))
  PROCESSED=$((PROCESSED + 1))
  BREAKDOWN="${BREAKDOWN}${bytes}	${ref}
"
  IFS='
'
done
IFS="$OLD_IFS"

# THE FAIL-CLOSED INVARIANT (#1601 review finding 1). Every ref parsed must have been accounted
# for — measured, recorded as missing, or recorded as cross-origin. If the loop was skipped or cut
# short for ANY reason, this is what refuses to report the shortfall as a pass. It is the
# generalisation of the here-doc bug, not a patch for it: truncation, an unreadable file, a
# `continue` added without a counter bump, all land here.
EXPECTED_REFS="$(printf '%s\n' "$REFS" | /usr/bin/sed '/^$/d' | /usr/bin/wc -l | /usr/bin/tr -d ' ')"
if [ "$PROCESSED" -ne "$EXPECTED_REFS" ]; then
  fail "Parsed ${EXPECTED_REFS} asset reference(s) but only accounted for ${PROCESSED}.
       The summing loop did not run to completion, so the total below would understate the route.
       Refusing to report it. (Historically this was a here-document whose temp file could not be
       created, which left the loop body unexecuted and still exited 0.)"
fi

# A referenced-but-absent asset is a real defect (a stale index.html, or a build that did not
# finish), and silently skipping it would UNDERSTATE the route weight — the one direction a
# size guard must never err in.
if [ -n "$MISSING" ]; then
  printf '❌ index.html references assets that do not exist in %s:\n' "$DIST_DIR" >&2
  printf '%s' "$MISSING" | /usr/bin/sed '/^$/d;s/^/       /' >&2
  fail "Refusing to report a total that omits them."
fi

# A NEW parser-blocking third-party origin is a route regression even though its bytes are not
# ours to weigh, so it fails rather than being quietly listed.
if [ -n "$UNKNOWN_REMOTE" ]; then
  printf '❌ New parser-blocking third-party script(s) on the landing route:\n' >&2
  printf '%s' "$UNKNOWN_REMOTE" | /usr/bin/sed '/^$/d;s/^/       /' >&2
  printf '   A cross-origin blocking script gates discovery of our own JS behind someone else'"'"'s\n' >&2
  printf '   connection, which is why Lighthouse charges this to the critical path. Give it async or\n' >&2
  printf '   defer if the consumer tolerates it, or add the origin to KNOWN_BLOCKING_THIRD_PARTY\n' >&2
  printf '   with a reason.\n' >&2
  fail "Refusing to accept a new third-party blocking script silently."
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

printf '✅ Landing route (/) eager download: %s KiB gzip across %s local assets (budget %s KiB)\n' \
  "$TOTAL_KIB" "$COUNT" "$BUDGET_KIB"
if [ -n "$REMOTE" ]; then
  # Stated on success too: the total is explicitly NOT the whole first-paint cost, and a reader
  # who does not know that will over-trust the number.
  printf '   Plus %s parser-blocking third-party request(s), whose bytes cannot be measured from\n' \
    "$(printf '%s' "$REMOTE" | /usr/bin/sed '/^$/d' | /usr/bin/wc -l | /usr/bin/tr -d ' ')" >&2
  printf '   disk and are NOT in the total above:\n' >&2
  printf '%s' "$REMOTE" | /usr/bin/sed '/^$/d;s/^/       /' >&2
fi
