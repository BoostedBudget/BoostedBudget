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
import { HTTP_METHODS } from '../src/spec/index-builder'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OPENAPI_PATH = resolve(__dirname, '../../../api/openapi.yaml')

const rawYaml = readFileSync(OPENAPI_PATH, 'utf-8')
const spec = parseYaml(rawYaml) as { paths?: Record<string, Record<string, { operationId?: string }>> }

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

console.log(`✅ Manifest completeness check passed — all OpenAPI operations are classified`)
