#!/usr/bin/env bash
# Fails CI if any built JS chunk in apps/web/dist/assets/ has no matching
# entry in .size-limit.json. Prevents regressions from hiding in Rollup-emitted
# common chunks that size-limit ignores.
set -euo pipefail

DIST="apps/web/dist/assets"

if [ ! -d "$DIST" ]; then
  echo "❌ No dist directory found at $DIST. Run 'bun run build' first."
  exit 1
fi

# ⚠ FLOOR, not a skip (#1569 r17). An empty assets directory used to `exit 0` here, which made
# this guard an unconditional pass for the single worst build outcome it could see: a build that
# emitted nothing at all. Every caller runs this immediately after `bun run build`, so zero chunks
# is never a legitimate state — it means the build silently produced no output, and every orphan
# the loop below would have caught is vacuously absent. MIN_JS_CHUNKS is overridable so a caller
# can demand a stronger floor than "at least one".
MIN_JS_CHUNKS="${MIN_JS_CHUNKS:-1}"

CHUNK_COUNT=0
for f in "$DIST"/*.js; do
  [ -e "$f" ] || continue
  CHUNK_COUNT=$((CHUNK_COUNT + 1))
done

if [ "$CHUNK_COUNT" -lt "$MIN_JS_CHUNKS" ]; then
  echo "❌ Only $CHUNK_COUNT JS chunk(s) in $DIST, below the floor of $MIN_JS_CHUNKS."
  echo "   A build that emits (almost) nothing must not pass the orphan check by having nothing to check."
  echo "   Run 'bun run build' and confirm it succeeded, or raise/lower MIN_JS_CHUNKS deliberately."
  exit 1
fi

# Extract budgeted glob patterns from .size-limit.json (no jq needed).
# Matches lines like: "path": "apps/web/dist/assets/transactions-*.js"
# Extracts the filename glob: transactions-*.js
GLOBS=$(grep '"apps/web/dist/assets/[^"]*\.js"' .size-limit.json \
  | sed 's|.*"apps/web/dist/assets/||; s|".*||')

ORPHANS=0
for f in "$DIST"/*.js; do
  base=$(basename "$f")
  matched=0
  while IFS= read -r glob; do
    # shellcheck disable=SC2254
    case "$base" in $glob) matched=1; break ;; esac
  done <<EOF
$GLOBS
EOF
  if [ $matched -eq 0 ]; then
    echo "❌ ORPHAN CHUNK (no size-limit entry): $base"
    ORPHANS=$((ORPHANS + 1))
  fi
done

if [ $ORPHANS -gt 0 ]; then
  echo ""
  echo "FAIL: $ORPHANS unbudgeted chunk(s). Add entries to .size-limit.json or adjust manualChunks in apps/web/vite.config.ts."
  exit 1
fi

echo "✅ All JS chunks have size-limit budget entries."
