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
import type { SpecEntry, SpecIndex, SpecParameter } from './search-runtime'

// Paths relative to repo root
const __dirname = dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = resolve(__dirname, '../../../../')
const OPENAPI_PATH = resolve(REPO_ROOT, 'api/openapi.yaml')
const OUTPUT_PATH = resolve(__dirname, 'generated/spec-index.json')

interface OpenAPIParameterSchema {
  type?: string | string[]
  enum?: string[]
  items?: { type?: string; enum?: string[] }
  [key: string]: unknown
}

interface OpenAPIParameter {
  name?: string
  in?: string
  required?: boolean
  description?: string
  schema?: OpenAPIParameterSchema
  [key: string]: unknown
}

interface OpenAPIOperation {
  operationId?: string
  summary?: string
  description?: string
  tags?: string[]
  security?: unknown[]
  parameters?: OpenAPIParameter[]
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

  return indexFromSpec(spec)
}

/**
 * The flattening + classification pass, split out from buildSpecIndex so the
 * UNCLASSIFIED-OPERATION FAILURE PATH CAN BE TESTED. buildSpecIndex reads the committed
 * api/openapi.yaml, in which every operation is classified by definition, so a test that only
 * calls buildSpecIndex can never reach the throw below — it would stay green if the throw were
 * reverted to the old warn-and-drop. Tests feed a synthetic spec here instead.
 */
export function indexFromSpec(spec: OpenAPISpec): SpecIndex {
  const entries: SpecEntry[] = []
  const unclassified: string[] = []

  for (const [path, pathItem] of Object.entries(spec.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      const operation = pathItem[method] as OpenAPIOperation | undefined
      if (!operation?.operationId) continue

      const { operationId, summary = '', description, tags = [], parameters: rawParams } = operation

      // Look up capability classification. An operation absent from the manifest is a
      // BUILD FAILURE, not a warning — see the throw below.
      const capability = getCapability(operationId)
      if (capability === undefined) {
        unclassified.push(`${method.toUpperCase()} ${path} → ${operationId}`)
        continue
      }

      // Extract query + path parameters with description/enum only (token-efficient)
      const parameters: SpecParameter[] = (rawParams ?? [])
        .filter((p): p is OpenAPIParameter & { name: string; in: string } =>
          typeof p.name === 'string' && (p.in === 'query' || p.in === 'path') && !!(p.description || p.schema?.enum || p.schema?.items?.enum)
        )
        .map((p) => {
          const enumValues = p.schema?.enum ?? p.schema?.items?.enum
          const schemaType = Array.isArray(p.schema?.type) ? p.schema.type.find((t) => t !== 'null') : p.schema?.type
          return {
            name: p.name,
            in: p.in,
            ...(p.required && { required: true }),
            ...(p.description && { description: p.description }),
            ...(enumValues && { enum: enumValues.map(String) }),
            ...(schemaType && { type: schemaType }),
          }
        })

      const entry: SpecEntry = {
        operationId,
        method: method.toUpperCase(),
        path,
        summary,
        tags,
        manifestClass: capability.class,
        excluded: capability.excluded,
      }
      if (description) entry.description = description
      if (parameters.length > 0) entry.parameters = parameters

      entries.push(entry)
    }
  }

  // Issue #1503: previously an unclassified operationId was merely `console.warn`ed and
  // dropped. The drift gate (`make validate-mcp-artifacts`) compares the GENERATED index
  // against the committed one, so a dropped operation leaves the artifact byte-identical
  // and the gate exits 0 with the warning scrolling past. listAdminPlaidOrphanedItems sat
  // omitted behind exactly that green tick. Throwing makes the drift gate fail closed on
  // an unclassified operation too, instead of relying solely on
  // scripts/check-manifest-completeness.ts (which does catch it, but only runs in
  // `make test-fast` / `make test-all` / CI).
  if (unclassified.length > 0) {
    throw new Error(
      `Unclassified operations in api/openapi.yaml (add each to src/manifest/capability-manifest.ts ` +
        `as read/write/excluded — deny-by-default means all must be explicit):\n` +
        unclassified.map((o) => `   ${o}`).join('\n')
    )
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
