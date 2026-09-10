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
  ACKNOWLEDGED_LAZY_INIT_OPERATIONS,
  extractLazyInitOperationIds,
  findUnacknowledgedLazyInitOperations,
} from '../src/manifest/lazy-init-extension'
import {
  ACKNOWLEDGED_SAFE_POST_OPERATIONS,
  extractSafePostOperationIds,
  findUnacknowledgedSafePostOperations,
} from '../src/manifest/safe-post-extension'
import { HTTP_METHODS } from '../src/spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OPENAPI_PATH = resolve(__dirname, '../../../api/openapi.yaml')

const rawYaml = readFileSync(OPENAPI_PATH, 'utf-8')
const spec = parseYaml(rawYaml) as {
  paths?: Record<
    string,
    Record<string, { operationId?: string; 'x-mcp-lazy-init'?: unknown; 'x-mcp-safe-post'?: unknown }>
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

// #1610: scan for a NEW admitted-read-that-writes operation. A GET tagged x-mcp-lazy-init in
// api/openapi.yaml that isn't in ACKNOWLEDGED_LAZY_INIT_OPERATIONS is a lazy-init write nobody
// has reviewed yet — see src/manifest/lazy-init-extension.ts for the full mechanism.
const lazyInitTagged = extractLazyInitOperationIds(spec)
const unacknowledged = findUnacknowledgedLazyInitOperations(lazyInitTagged, ACKNOWLEDGED_LAZY_INIT_OPERATIONS)

if (unacknowledged.length > 0) {
  console.error('❌ Manifest completeness check FAILED — unacknowledged lazy-init operation(s):')
  for (const id of unacknowledged) {
    console.error(`   ${id}`)
  }
  console.error('')
  console.error('This operation is tagged x-mcp-lazy-init: true in api/openapi.yaml (it lazily')
  console.error('writes on an admitted GET) but is not in ACKNOWLEDGED_LAZY_INIT_OPERATIONS')
  console.error('(apps/mcp-server/src/manifest/lazy-init-extension.ts). Review it, then add it')
  console.error('to that list AND to execute.test.ts\'s pinned expectation.')
  process.exit(1)
}

const safePostTagged = extractSafePostOperationIds(spec)
const unacknowledgedSafePosts = findUnacknowledgedSafePostOperations(safePostTagged, ACKNOWLEDGED_SAFE_POST_OPERATIONS)

if (unacknowledgedSafePosts.length > 0) {
  console.error('❌ Manifest completeness check FAILED — unacknowledged safe-POST operation(s):')
  for (const id of unacknowledgedSafePosts) {
    console.error(`   ${id}`)
  }
  console.error('')
  console.error('This operation is tagged x-mcp-safe-post: true in api/openapi.yaml (it admits a')
  console.error('non-read-verb call into readExecute) but is not in ACKNOWLEDGED_SAFE_POST_OPERATIONS')
  console.error('(apps/mcp-server/src/manifest/safe-post-extension.ts). Review it — including proving')
  console.error('by integration test that it writes nothing — then add it to that list AND to')
  console.error("execute.test.ts's pinned expectation.")
  process.exit(1)
}

console.log(`✅ Manifest completeness check passed — all OpenAPI operations are classified`)
console.log(`✅ Lazy-init scan passed — ${lazyInitTagged.length} tagged operation(s), all acknowledged`)
console.log(`✅ Safe-POST scan passed — ${safePostTagged.length} tagged operation(s), all acknowledged`)
