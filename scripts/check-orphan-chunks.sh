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

if ! ls "$DIST"/*.js 1>/dev/null 2>&1; then
  echo "ℹ️  No JS chunks found in $DIST. Skipping orphan check."
  exit 0
fi

# Extract budgeted chunk name prefixes from .size-limit.json (no jq needed).
# Matches lines like: "path": "apps/web/dist/assets/transactions-*.js"
# Extracts: transactions
KNOWN=$(grep -oE '"apps/web/dist/assets/[^"]+\.js"' .size-limit.json \
  | sed 's|"apps/web/dist/assets/||; s|-\*\.js"||; s|\.js"||; s|"||g')

ORPHANS=0
for f in "$DIST"/*.js; do
  base=$(basename "$f")
  # Strip Vite content-hash suffix: -[8 base64url chars].js
  # Vite hashes are 8 chars from [A-Za-z0-9_-], e.g. -DGj_IzSb.js or -Bl13-G5I.js
  prefix=$(echo "$base" | sed 's/-[A-Za-z0-9_-]\{8\}\.js$//')
  if ! printf '%s\n' "$KNOWN" | grep -qx "$prefix"; then
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
