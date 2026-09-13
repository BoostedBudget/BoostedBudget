/**
 * CI manifest-completeness check.
 *
 * Reads api/openapi.yaml and verifies every operationId is present in the
 * capability manifest. Any unlisted operationId causes a non-zero exit,
 * failing the build (deny-by-default holds as the backend grows).
 *
 * Run via: bun run scripts/check-manifest-completeness.ts
 */

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseYaml } from 'yaml'
import { getCapability } from '../src/manifest/capability-manifest'
import {
  ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
  extractLegacyLazyInitOccurrences,
  extractReadSideEffectOperationIds,
  findUnacknowledgedReadSideEffectOperations,
} from '../src/manifest/read-side-effect-extension'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  extractSafePostOperations,
  findSafePostDrift,
} from '../src/manifest/safe-post-extension'
import { HTTP_METHODS } from '../src/spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OPENAPI_PATH = resolve(__dirname, '../../../api/openapi.yaml')

const rawYaml = readFileSync(OPENAPI_PATH, 'utf-8')
const spec = parseYaml(rawYaml) as {
  paths?: Record<
    string,
    Record<string, {
      operationId?: string
      'x-mcp-read-side-effect'?: unknown
      'x-mcp-lazy-init'?: unknown
      'x-mcp-safe-post'?: unknown
    }>
  >
}

const missing: string[] = []

for (const [path, pathItem] of Object.entries(spec.paths ?? {})) {
  for (const method of HTTP_METHODS) {
    const op = pathItem[method]
    if (!op?.operationId) continue
    if (getCapability(op.operationId) === undefined) {
      missing.push(`${method.toUpperCase()} ${path} → ${op.operationId}`)
    }
  }
}

if (missing.length > 0) {
  console.error('❌ Manifest completeness check FAILED — unclassified operations:')
  for (const m of missing) {
    console.error(`   ${m}`)
  }
  console.error('')
  console.error('Add each operationId to apps/mcp-server/src/manifest/capability-manifest.ts')
  console.error('(classify as read/write/excluded — deny-by-default means all must be explicit)')
  process.exit(1)
}

const legacyLazyInitOccurrences = extractLegacyLazyInitOccurrences(spec)
if (legacyLazyInitOccurrences.length > 0) {
  console.error('❌ Manifest completeness check FAILED — removed x-mcp-lazy-init extension found:')
  for (const occurrence of legacyLazyInitOccurrences) {
    console.error(`   ${occurrence.method} ${occurrence.path} → ${occurrence.operationId}`)
  }
  console.error('')
  console.error('Replace x-mcp-lazy-init with x-mcp-read-side-effect, then review and add the')
  console.error('operation to ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS if it remains admitted.')
  process.exit(1)
}

// #1610: scan for a newly tagged admitted operation with a caller-scoped read side effect.
const readSideEffectTagged = extractReadSideEffectOperationIds(spec)
const unacknowledged = findUnacknowledgedReadSideEffectOperations(
  readSideEffectTagged,
  ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS,
)

if (unacknowledged.length > 0) {
  console.error('❌ Manifest completeness check FAILED — unacknowledged read-side-effect operation(s):')
  for (const id of unacknowledged) {
    console.error(`   ${id}`)
  }
  console.error('')
  console.error('This operation is tagged x-mcp-read-side-effect: true in api/openapi.yaml but')
  console.error('is not in ACKNOWLEDGED_READ_SIDE_EFFECT_OPERATIONS')
  console.error('(apps/mcp-server/src/manifest/read-side-effect-extension.ts). Review it, then add it')
  console.error('to that list AND to execute.test.ts\'s pinned expectation.')
  process.exit(1)
}

const safePostTagged = extractSafePostOperations(spec)
const safePostDrift = findSafePostDrift(safePostTagged, ACKNOWLEDGED_SAFE_POST_OPERATIONS)

if (safePostDrift.unacknowledged.length > 0) {
  console.error('❌ Manifest completeness check FAILED — unacknowledged safe-POST operation(s):')
  for (const op of safePostDrift.unacknowledged) {
    console.error(`   ${op.method} ${op.path} → ${op.operationId}`)
  }
  console.error('')
  console.error('This operation is tagged x-mcp-safe-post: true in api/openapi.yaml (it admits a')
  console.error('non-read-verb call into readExecute) but its (operationId, method, path) tuple is not')
  console.error('in ACKNOWLEDGED_SAFE_POST_OPERATIONS (apps/mcp-server/src/manifest/safe-post-operations.ts).')
  console.error('Review it — including proving by integration test that it writes nothing — then add it')
  console.error("to that list AND to execute.test.ts's pinned expectation.")
  process.exit(1)
}

// Issue #1630 Task B4: the MIRROR direction. An acknowledged tuple the spec no longer produces
// means the backend's x-mcp-safe-post tag was deleted, or the operation moved to a different
// method/path -- either way, readExecute's admission gate would otherwise keep admitting a call
// on a shape nobody currently reviews as safe, with nothing to ever flag it. This is what the
// old, one-directional check (findUnacknowledgedSafePostOperations, id-only) could not catch.
if (safePostDrift.stale.length > 0) {
  console.error('❌ Manifest completeness check FAILED — acknowledged safe-POST no longer matches the spec:')
  for (const op of safePostDrift.stale) {
    console.error(`   ${op.method} ${op.path} → ${op.operationId}`)
  }
  console.error('')
  console.error('This (operationId, method, path) tuple is listed in ACKNOWLEDGED_SAFE_POST_OPERATIONS')
  console.error('(apps/mcp-server/src/manifest/safe-post-operations.ts) but api/openapi.yaml no longer')
  console.error('tags it x-mcp-safe-post: true at that exact method+path -- the tag was removed, or the')
  console.error('operation moved to a different verb or path. Update or remove the acknowledged entry')
  console.error("(and execute.test.ts's pinned expectation) to match reality.")
  process.exit(1)
}

console.log(`✅ Manifest completeness check passed — all OpenAPI operations are classified`)
console.log(
  `✅ Read-side-effect scan passed — ${readSideEffectTagged.length} tagged operation(s), all acknowledged`,
)
console.log(`✅ Safe-POST scan passed — ${safePostTagged.length} tagged operation(s), all acknowledged, no drift`)
