/**
 * Capability summary generator for the public `/mcp` marketing page (Issue #1411).
 *
 * Reads THIS package's own manifest + spec index (never imported directly by
 * apps/web — see the module doc in apps/web/src/features/mcp/McpPage.tsx) and
 * writes an aggregates-only JSON file: total operations, allowed/read/write/
 * excluded/destructive-write counts, per-tag read counts, and exclusion groups
 * with their reason strings. No operation lists — keeps the committed file
 * small and free of anything that could grow into a spec dump.
 *
 * Run: bun run scripts/generate-mcp-capability-summary.ts
 * (or: bun run generate:mcp-summary, once wired into package.json by a later task)
 *
 * The companion drift test (src/manifest/capability-summary.drift.test.ts)
 * recomputes this from the manifest + spec index and fails if the committed
 * apps/web JSON has drifted.
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { getCapability } from '../src/manifest/capability-manifest'
import type { SpecIndex } from '../src/spec/search-runtime'

const __dirname = dirname(fileURLToPath(import.meta.url))
const SPEC_INDEX_PATH = resolve(__dirname, '../src/spec/generated/spec-index.json')
const OUTPUT_PATH = resolve(
  __dirname,
  '../../web/src/features/mcp/data/capabilitySummary.json',
)

export interface ExclusionGroup {
  /** Human-readable exclusion reason, verbatim from the manifest. */
  reason: string
  /** Number of operations excluded for this reason. */
  count: number
}

export interface CapabilitySummary {
  /** Every operation the manifest classifies (allowed + excluded). */
  totalOperations: number
  /** Reachable through MCP (read + write, not excluded). */
  allowedOperations: number
  /** Allowed and non-mutating. */
  readOperations: number
  /** Allowed and mutating (all confirmation-gated). */
  writeOperations: number
  /** Never reachable through MCP. */
  excludedOperations: number
  /** Allowed writes that destroy or permanently remove data. */
  destructiveWriteOperations: number
  /** Allowed read-operation counts, keyed by OpenAPI tag (an op with N tags counts under each). */
  readCountByTag: Record<string, number>
  /** Exclusion reasons and how many operations cite each, largest group first. */
  exclusionGroups: ExclusionGroup[]
}

/**
 * Recomputes the capability summary from the committed spec index
 * (src/spec/generated/spec-index.json), cross-referencing the manifest for
 * fields the spec index doesn't carry (destructive flag, exclusion reason).
 *
 * Iterating the spec index (rather than the manifest's own operation list)
 * keeps `totalOperations` anchored to real, currently-specified API
 * operations — the same set apps/mcp-server itself treats as authoritative.
 */
export function computeCapabilitySummary(specIndex: SpecIndex): CapabilitySummary {
  let allowedOperations = 0
  let readOperations = 0
  let writeOperations = 0
  let excludedOperations = 0
  let destructiveWriteOperations = 0
  const readCountByTag: Record<string, number> = {}
  const exclusionCounts = new Map<string, number>()

  for (const entry of specIndex.entries) {
    const capability = getCapability(entry.operationId)
    if (!capability) continue // Not classified — CI's manifest-completeness check catches this separately.

    if (entry.excluded) {
      excludedOperations++
      const reason = capability.excludedReason ?? 'Unspecified'
      exclusionCounts.set(reason, (exclusionCounts.get(reason) ?? 0) + 1)
      continue
    }

    allowedOperations++
    if (entry.manifestClass === 'read') {
      readOperations++
      for (const tag of entry.tags ?? []) {
        readCountByTag[tag] = (readCountByTag[tag] ?? 0) + 1
      }
    } else if (entry.manifestClass === 'write') {
      writeOperations++
      if (capability.destructive) destructiveWriteOperations++
    }
  }

  const exclusionGroups: ExclusionGroup[] = [...exclusionCounts.entries()]
    .map(([reason, count]) => ({ reason, count }))
    .sort((a, b) => b.count - a.count || a.reason.localeCompare(b.reason))

  const sortedReadCountByTag = Object.fromEntries(
    Object.entries(readCountByTag).sort(([a], [b]) => a.localeCompare(b)),
  )

  return {
    totalOperations: specIndex.entries.length,
    allowedOperations,
    readOperations,
    writeOperations,
    excludedOperations,
    destructiveWriteOperations,
    readCountByTag: sortedReadCountByTag,
    exclusionGroups,
  }
}

/** Reads the committed spec index from disk. Exported so the drift test can reuse it. */
export function readSpecIndex(): SpecIndex {
  return JSON.parse(readFileSync(SPEC_INDEX_PATH, 'utf-8')) as SpecIndex
}

if (import.meta.main) {
  const summary = computeCapabilitySummary(readSpecIndex())
  writeFileSync(OUTPUT_PATH, JSON.stringify(summary, null, 2) + '\n')
  console.log(
    `✅ capabilitySummary.json generated: ${summary.totalOperations} operations ` +
      `(${summary.allowedOperations} allowed: ${summary.readOperations} read / ${summary.writeOperations} write, ` +
      `${summary.excludedOperations} excluded, ${summary.destructiveWriteOperations} destructive)`,
  )
}
