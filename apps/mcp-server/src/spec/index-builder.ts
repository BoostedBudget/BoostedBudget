/**
 * Spec index builder — BUILD-TIME script, not a Worker module.
 * Runs via: bun run src/spec/index-builder.ts
 *
 * Reads api/openapi.yaml → dereferences $refs → flattens each operation
 * into a token-efficient SpecEntry → writes generated/spec-index.json.
 *
 * The generated file is committed and CI-verified for drift (same pattern
 * as api/openapi.yaml and the Orval client).
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import $RefParser from '@apidevtools/json-schema-ref-parser'
import { parse as parseYaml } from 'yaml'
import { getCapability } from '../manifest/capability-manifest'
import type { SpecEntry, SpecIndex } from './search-runtime'

// Paths relative to repo root
const __dirname = dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = resolve(__dirname, '../../../../')
const OPENAPI_PATH = resolve(REPO_ROOT, 'api/openapi.yaml')
const OUTPUT_PATH = resolve(__dirname, 'generated/spec-index.json')

interface OpenAPIOperation {
  operationId?: string
  summary?: string
  description?: string
  tags?: string[]
  security?: unknown[]
}

interface OpenAPIPathItem {
  get?: OpenAPIOperation
  post?: OpenAPIOperation
  put?: OpenAPIOperation
  patch?: OpenAPIOperation
  delete?: OpenAPIOperation
  [key: string]: unknown
}

interface OpenAPISpec {
  paths?: Record<string, OpenAPIPathItem>
  [key: string]: unknown
}

const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'] as const
export { HTTP_METHODS }

export async function buildSpecIndex(): Promise<SpecIndex> {
  // Parse YAML
  const rawYaml = readFileSync(OPENAPI_PATH, 'utf-8')
  const rawSpec = parseYaml(rawYaml) as OpenAPISpec

  // Dereference all $refs (inline — no remote refs in this spec)
  const spec = (await $RefParser.dereference(rawSpec as object)) as OpenAPISpec

  const entries: SpecEntry[] = []

  for (const [path, pathItem] of Object.entries(spec.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      const operation = pathItem[method] as OpenAPIOperation | undefined
      if (!operation?.operationId) continue

      const { operationId, summary = '', tags = [] } = operation

      // Look up capability classification; skip operations not in the manifest
      const capability = getCapability(operationId)
      if (capability === undefined) {
        // Unknown operation — omit from index (it'll surface in the CI completeness check)
        console.warn(`[spec-index-builder] Unknown operationId: ${operationId} — not in manifest`)
        continue
      }

      entries.push({
        operationId,
        method: method.toUpperCase(),
        path,
        summary,
        tags,
        manifestClass: capability.class,
        excluded: capability.excluded,
      })
    }
  }

  const index: SpecIndex = {
    version: '1',
    entries,
  }

  return index
}

// ── CLI entrypoint ──────────────────────────────────────────────────────────

if (import.meta.main) {
  const index = await buildSpecIndex()
  writeFileSync(OUTPUT_PATH, JSON.stringify(index, null, 2) + '\n')
  const total = index.entries.length
  const excluded = index.entries.filter((e) => e.excluded).length
  const allowed = total - excluded
  console.log(`✅ spec-index.json generated: ${total} operations (${allowed} allowed, ${excluded} excluded)`)
}
