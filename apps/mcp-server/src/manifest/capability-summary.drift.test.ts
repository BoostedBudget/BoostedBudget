/**
 * Drift test for the public /mcp page's capability summary (Issue #1411).
 *
 * apps/web/src/features/mcp/data/capabilitySummary.json is a COMMITTED,
 * generated file (via scripts/generate-mcp-capability-summary.ts) — apps/web
 * has no build step of its own that regenerates it, and must never import
 * this package's manifest or spec index directly. This test is the tripwire:
 * it recomputes the aggregates fresh from the manifest + spec index and fails
 * if the committed JSON has drifted, so a manifest change can't silently make
 * the marketing page's numbers wrong.
 *
 * Known values at the time this page shipped (verified against the manifest,
 * not trusted from the issue brief): 192 total, 133 allowed, 87 read, 46 write,
 * 59 excluded, 9 destructive writes. Asserted as documentation below — expect
 * this test (not those literals) to be the source of truth as the manifest grows.
 */

import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { computeCapabilitySummary, readSpecIndex } from '../../scripts/generate-mcp-capability-summary'
import { ALL_OPERATION_IDS } from './capability-manifest'

const __dirname = dirname(fileURLToPath(import.meta.url))
const COMMITTED_JSON_PATH = resolve(
  __dirname,
  '../../../web/src/features/mcp/data/capabilitySummary.json',
)

describe('capability summary drift (apps/web/src/features/mcp/data/capabilitySummary.json)', () => {
  it('matches the aggregates freshly computed from the manifest + spec index', () => {
    const fresh = computeCapabilitySummary(readSpecIndex())
    const committedRaw = readFileSync(COMMITTED_JSON_PATH, 'utf-8')
    const committed = JSON.parse(committedRaw)

    expect(
      committed,
      'apps/web/src/features/mcp/data/capabilitySummary.json is stale — ' +
        'regenerate with: bun run scripts/generate-mcp-capability-summary.ts (from apps/mcp-server)',
    ).toEqual(fresh)
  })

  it('documents the known baseline values (informational — the drift check above is authoritative)', () => {
    const fresh = computeCapabilitySummary(readSpecIndex())

    expect(fresh.totalOperations).toBe(192)
    expect(fresh.allowedOperations).toBe(133)
    expect(fresh.readOperations).toBe(87)
    expect(fresh.writeOperations).toBe(46)
    expect(fresh.excludedOperations).toBe(59)
    expect(fresh.destructiveWriteOperations).toBe(9)
    expect(fresh.allowedOperations).toBe(fresh.readOperations + fresh.writeOperations)
    expect(fresh.totalOperations).toBe(fresh.allowedOperations + fresh.excludedOperations)

    const exclusionTotal = fresh.exclusionGroups.reduce((sum, g) => sum + g.count, 0)
    expect(exclusionTotal).toBe(fresh.excludedOperations)
  })

  it('committed JSON ends with a trailing newline (matches the generator script)', () => {
    const raw = readFileSync(COMMITTED_JSON_PATH, 'utf-8')
    expect(raw.endsWith('\n')).toBe(true)
  })

  // Codex review finding #8: computeCapabilitySummary() iterates the spec index, so a
  // manifest entry with no corresponding spec-index entry (e.g. added to the manifest but
  // not yet wired to an OpenAPI operation) is silently invisible to it — the drift check
  // above would stay green even though the manifest and the summary have diverged. This
  // guards that gap directly: every manifest entry must have a spec-index counterpart.
  it('every manifest entry has a corresponding spec-index entry (catches manifest-only additions)', () => {
    const specIndex = readSpecIndex()
    const specOperationIds = new Set(specIndex.entries.map((e) => e.operationId))

    const manifestOnly = ALL_OPERATION_IDS.filter((id) => !specOperationIds.has(id))

    expect(
      manifestOnly,
      'These operationIds are classified in capability-manifest.ts but have no spec-index ' +
        'entry, so they are invisible to the capabilitySummary.json aggregates above. Either ' +
        'they are stale manifest entries (remove them) or the spec index needs rebuilding ' +
        '(bun run build:spec-index).',
    ).toEqual([])

    // And the reverse count check the summary itself relies on: no spec-index entry should
    // silently outnumber the manifest (would mean an unclassified operation slipped through —
    // scripts/check-manifest-completeness.ts is the primary guard for this, this is a
    // cross-check from the drift test's own inputs).
    expect(specIndex.entries.length).toBeLessThanOrEqual(ALL_OPERATION_IDS.length)
  })
})
