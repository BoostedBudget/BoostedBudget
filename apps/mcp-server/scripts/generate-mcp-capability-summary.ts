/**
 * Capability summary generator for the public `/mcp` marketing page (Issue #1411).
 *
 * Reads THIS package's own manifest + spec index (never imported directly by
 * apps/web — see the module doc in apps/web/src/features/mcp/McpPage.tsx) and
 * writes an aggregates-only JSON file: total operations, allowed/read/write/
 * excluded/destructive-write counts, and a small set of curated public action
 * boundaries. No operation lists or internal exclusion reasons — this keeps the
 * committed file suitable for the public page.
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
import { PUBLIC_BOUNDARY_IDS, getCapability, type PublicBoundaryId } from '../src/manifest/capability-manifest'
import type { SpecIndex } from '../src/spec/search-runtime'

const __dirname = dirname(fileURLToPath(import.meta.url))
const SPEC_INDEX_PATH = resolve(__dirname, '../src/spec/generated/spec-index.json')
const OUTPUT_PATH = resolve(__dirname, '../../web/src/features/mcp/data/capabilitySummary.json')

export interface PublicBoundary {
  id: PublicBoundaryId
  title: string
  description: string
}

export interface CapabilitySummary {
  /** Every operation the manifest classifies (allowed + excluded). */
  totalOperations: number
  /** Reachable through MCP (read + write, not excluded). */
  allowedOperations: number
  /** Allowed and non-mutating. */
  readOperations: number
  /**
   * Allowed and mutating (all confirmation-gated) — manifest class 'write' OR 'destructive'.
   *
   * ⚠ #1569 Task 16 added a third manifest class. This field deliberately keeps its
   * "every allowed mutating operation" meaning rather than narrowing to class === 'write',
   * so `allowedOperations === readOperations + writeOperations` still holds and the public
   * /mcp page's sentence ("N capabilities: R for reading and W for changes") stays true.
   * destructiveWriteOperations below is the subset that permanently destroys data.
   */
  writeOperations: number
  /** Never reachable through MCP. */
  excludedOperations: number
  /**
   * Allowed mutating operations that permanently destroy data — manifest class 'destructive'.
   * A subset of writeOperations. Soft-deleting ("tombstoning") verbs are NOT counted: after
   * #1569 deleting a budget, budget category, custom category or savings goal retains the row
   * and its dependent financial history.
   */
  destructiveWriteOperations: number
  /** Curated account-action boundaries suitable for public page copy. */
  publicBoundaries: PublicBoundary[]
}

const PUBLIC_BOUNDARY_COPY: Record<PublicBoundaryId, PublicBoundary> = {
  'bank-connections': {
    id: 'bank-connections',
    title: 'Bank connections',
    description: 'Linking or disconnecting a bank connection stays in BoostedBudget.'
  },
  'household-sharing': {
    id: 'household-sharing',
    title: 'Household membership and sharing',
    description: 'Invites, membership changes, sharing, and consent stay in BoostedBudget.'
  },
  'subscription-changes': {
    id: 'subscription-changes',
    title: 'Subscription changes',
    description: 'Starting, changing, or cancelling a subscription stays in BoostedBudget.'
  },
  'account-deletion': {
    id: 'account-deletion',
    title: 'Account deletion',
    description: 'Requesting or cancelling BoostedBudget account deletion stays in BoostedBudget.'
  }
}

/**
 * Recomputes the capability summary from the committed spec index
 * (src/spec/generated/spec-index.json), cross-referencing the manifest for
 * fields the spec index doesn't carry (destructive flag, public boundary).
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
  const publicBoundaryIds = new Set<PublicBoundaryId>()

  for (const entry of specIndex.entries) {
    const capability = getCapability(entry.operationId)
    if (!capability) continue // Not classified — CI's manifest-completeness check catches this separately.

    if (entry.excluded) {
      excludedOperations++
      if (capability.publicBoundary) publicBoundaryIds.add(capability.publicBoundary)
      continue
    }

    allowedOperations++
    if (entry.manifestClass === 'read') {
      readOperations++
    } else if (entry.manifestClass === 'write' || entry.manifestClass === 'destructive') {
      writeOperations++
      // Counted off the CLASS, not off capability.destructive. Both are kept in lockstep by
      // capability-manifest.test.ts, but reading the flag made this aggregate blind to a
      // destructive→write reclassification (the flag stayed true, the op just moved buckets),
      // and an aggregate that cannot move is an aggregate the drift test cannot police.
      if (entry.manifestClass === 'destructive') destructiveWriteOperations++
    }
  }

  const publicBoundaries = PUBLIC_BOUNDARY_IDS.filter((id) => publicBoundaryIds.has(id)).map(
    (id) => PUBLIC_BOUNDARY_COPY[id]
  )

  return {
    totalOperations: specIndex.entries.length,
    allowedOperations,
    readOperations,
    writeOperations,
    excludedOperations,
    destructiveWriteOperations,
    publicBoundaries
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
      `${summary.excludedOperations} excluded, ${summary.destructiveWriteOperations} destructive)`
  )
}
