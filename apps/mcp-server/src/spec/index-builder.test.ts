/**
 * index-builder unit tests — R9-d (#1267)
 *
 * Calls `buildSpecIndex()` against the committed `api/openapi.yaml` to lock in
 * the R8-2b param-extraction rules:
 *   1. Description-only path params are kept (guards the `p.description ||` branch).
 *   2. `schema.enum` values are extracted into the `parameters[].enum` array.
 *   3. Array-type params with description (but no items.enum) are kept without an enum field.
 *   4. Every included parameter carries description OR enum (no silent passthrough).
 */

import { describe, expect, it, beforeAll } from 'bun:test'
import { buildSpecIndex } from './index-builder'
import type { SpecEntry } from './search-runtime'

let entries: SpecEntry[]

beforeAll(async () => {
  const index = await buildSpecIndex()
  entries = index.entries
})

describe('buildSpecIndex param extraction (R8-2b regression)', () => {
  it('keeps description-only path params (description || branch)', () => {
    // getBoostExecutions.id: path param with description only, no enum.
    // Guards the `!!(p.description || ...)` filter keeping it.
    // (#1283: renamed from getRuleExecutions)
    const entry = entries.find((e) => e.operationId === 'getBoostExecutions')
    expect(entry).toBeDefined()

    const idParam = entry?.parameters?.find((p) => p.name === 'id' && p.in === 'path')
    expect(idParam).toBeDefined()
    expect(idParam?.description).toBeTruthy()
    expect(idParam?.enum).toBeUndefined()
  })

  it('extracts schema.enum values into the parameter enum array', () => {
    // getTransactions.sort_by has schema.enum: ['date','merchant','amount','category'].
    // Guards the `p.schema?.enum ?? ...` extraction branch.
    const entry = entries.find((e) => e.operationId === 'getTransactions')
    expect(entry).toBeDefined()

    const sortBy = entry?.parameters?.find((p) => p.name === 'sort_by')
    expect(sortBy).toBeDefined()
    expect(sortBy?.enum).toContain('date')
    expect(sortBy?.enum).toContain('amount')
    expect(sortBy?.enum).toContain('merchant')
  })

  it('keeps array-type params with description even when they have no items.enum', () => {
    // getBoosts.boost_type: type:array, has description, items have no enum.
    // The valid values are enforced handler-side (422); the OpenAPI schema uses plain items:{type:string}.
    // Guards that description-bearing array params are included but WITHOUT an enum field.
    // (#1283: operationId renamed from getAutomationRules; #1287: query param renamed rule_type→boost_type)
    const entry = entries.find((e) => e.operationId === 'getBoosts')
    expect(entry).toBeDefined()

    const boostTypeParam = entry?.parameters?.find((p) => p.name === 'boost_type')
    expect(boostTypeParam).toBeDefined()
    expect(boostTypeParam?.in).toBe('query')
    expect(boostTypeParam?.description).toBeTruthy()
    // No enum — handler-side validation means the spec doesn't enumerate values on items
    expect(boostTypeParam?.enum).toBeUndefined()
  })

  it('every included parameter carries at least a description or an enum (no silent passthrough)', () => {
    // The filter condition: `!!(p.description || p.schema?.enum || p.schema?.items?.enum)`
    // If a param slips through without either field, this catches it.
    for (const entry of entries) {
      for (const param of entry.parameters ?? []) {
        const hasDocumentation = !!(param.description ?? param.enum?.length)
        expect(
          hasDocumentation,
          `${entry.operationId}.${param.name} has neither description nor enum`
        ).toBe(true)
      }
    }
  })
})

// ── Issue #1503: unclassified operations must fail the build, not warn ───────

describe('issue #1503: fail-closed on unclassified operations', () => {
  it('builds without throwing, i.e. every operationId in the committed spec is classified', async () => {
    // RED before #1503: listAdminPlaidOrphanedItems was absent from the capability
    // manifest, so the builder silently dropped it and `make validate-mcp-artifacts`
    // still exited 0 (the generated index was byte-identical to the committed one).
    // buildSpecIndex now throws instead, so this assertion is the drift gate's
    // fail-closed guarantee, not a tautology.
    await expect(buildSpecIndex()).resolves.toBeDefined()
  })

  it('every entry carries a manifest classification', () => {
    for (const entry of entries) {
      expect(entry.manifestClass === 'read' || entry.manifestClass === 'write').toBe(true)
      expect(typeof entry.excluded).toBe('boolean')
    }
  })
})
