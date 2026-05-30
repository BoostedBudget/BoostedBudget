import { describe, expect, it } from 'bun:test'
import type { SpecEntry, SpecIndex } from './search-runtime'
import { searchSpec, getSpecEntry, listSpecEntries } from './search-runtime'

// Minimal fixture index for unit tests — the real index is built by index-builder.ts
const FIXTURE_INDEX: SpecIndex = {
  version: '1',
  generatedAt: '2026-01-01T00:00:00Z',
  entries: [
    {
      operationId: 'getTransactions',
      method: 'GET',
      path: '/api/v1/transactions',
      summary: 'Get paginated list of transactions',
      tags: ['Transactions'],
      manifestClass: 'read',
      excluded: false,
    },
    {
      operationId: 'updateTransactionCategory',
      method: 'PUT',
      path: '/api/v1/transactions/{id}/category',
      summary: 'Update the category of a transaction',
      tags: ['Transactions'],
      manifestClass: 'write',
      excluded: false,
    },
    {
      operationId: 'getCashFlowSummary',
      method: 'GET',
      path: '/api/v1/analytics/cash-flow-summary',
      summary: 'Get cash flow summary for a date range',
      tags: ['Analytics'],
      manifestClass: 'read',
      excluded: false,
    },
    {
      operationId: 'checkRole',
      method: 'GET',
      path: '/api/v1/admin/check-role',
      summary: 'Check admin role',
      tags: ['Admin'],
      manifestClass: 'read',
      excluded: true,
    },
  ],
}

describe('searchSpec', () => {
  it('finds entries by keyword in summary', () => {
    const results = searchSpec(FIXTURE_INDEX, 'transaction')
    const ids = results.map((e: SpecEntry) => e.operationId)
    expect(ids).toContain('getTransactions')
    expect(ids).toContain('updateTransactionCategory')
  })

  it('finds entries by tag', () => {
    const results = searchSpec(FIXTURE_INDEX, 'analytics')
    expect(results[0]?.operationId).toBe('getCashFlowSummary')
  })

  it('finds entries by operationId fragment', () => {
    const results = searchSpec(FIXTURE_INDEX, 'cashflow')
    expect(results.length).toBeGreaterThan(0)
  })

  it('omits excluded entries by default', () => {
    const results = searchSpec(FIXTURE_INDEX, 'admin')
    expect(results.every((e: SpecEntry) => !e.excluded)).toBe(true)
  })

  it('includes excluded entries when includeExcluded=true', () => {
    const results = searchSpec(FIXTURE_INDEX, 'admin', { includeExcluded: true })
    expect(results.some((e: SpecEntry) => e.excluded)).toBe(true)
  })

  it('returns empty array for no matches', () => {
    const results = searchSpec(FIXTURE_INDEX, 'zzznomatch')
    expect(results).toEqual([])
  })

  it('limits results', () => {
    const results = searchSpec(FIXTURE_INDEX, 'transaction', { limit: 1 })
    expect(results.length).toBe(1)
  })

  it('filters by class', () => {
    const reads = searchSpec(FIXTURE_INDEX, '', { class: 'read' })
    expect(reads.every((e: SpecEntry) => e.manifestClass === 'read')).toBe(true)
  })
})

describe('getSpecEntry', () => {
  it('returns the entry for a known operationId', () => {
    const entry = getSpecEntry(FIXTURE_INDEX, 'getTransactions')
    expect(entry?.operationId).toBe('getTransactions')
  })

  it('returns undefined for unknown operationId', () => {
    expect(getSpecEntry(FIXTURE_INDEX, 'nope')).toBeUndefined()
  })
})

describe('listSpecEntries', () => {
  it('returns only non-excluded entries by default', () => {
    const entries = listSpecEntries(FIXTURE_INDEX)
    expect(entries.every((e: SpecEntry) => !e.excluded)).toBe(true)
  })

  it('filters by tag', () => {
    const entries = listSpecEntries(FIXTURE_INDEX, { tag: 'Analytics' })
    expect(entries.length).toBe(1)
    expect(entries[0]?.operationId).toBe('getCashFlowSummary')
  })

  it('filters by class=write', () => {
    const writes = listSpecEntries(FIXTURE_INDEX, { class: 'write' })
    expect(writes.every((e: SpecEntry) => e.manifestClass === 'write')).toBe(true)
  })
})
