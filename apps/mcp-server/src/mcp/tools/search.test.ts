import { describe, expect, it } from 'bun:test'
import { buildSearchResult, parseSearchInput } from './search'

describe('parseSearchInput', () => {
  it('accepts a plain query string', () => {
    const input = parseSearchInput({ query: 'spending by category' })
    expect(input.query).toBe('spending by category')
    expect(input.class).toBeUndefined()
    expect(input.tag).toBeUndefined()
    expect(input.limit).toBe(20)
  })

  it('accepts class filter', () => {
    const input = parseSearchInput({ query: 'budget', class: 'write' })
    expect(input.class).toBe('write')
  })

  it('accepts tag filter', () => {
    const input = parseSearchInput({ query: '', tag: 'Analytics' })
    expect(input.tag).toBe('Analytics')
  })

  it('accepts custom limit', () => {
    const input = parseSearchInput({ query: 'trans', limit: 5 })
    expect(input.limit).toBe(5)
  })

  it('clamps limit to max 50', () => {
    const input = parseSearchInput({ query: 'x', limit: 999 })
    expect(input.limit).toBe(50)
  })

  it('throws on invalid class value', () => {
    expect(() => parseSearchInput({ query: 'x', class: 'admin' as unknown as 'read' })).toThrow()
  })
})

describe('buildSearchResult', () => {
  it('returns a text content block', () => {
    const result = buildSearchResult([
      {
        operationId: 'getTransactions',
        method: 'GET',
        path: '/api/v1/transactions',
        summary: 'List transactions',
        tags: ['Transactions'],
        manifestClass: 'read',
        excluded: false,
      },
    ])
    expect(result.content).toHaveLength(1)
    expect(result.content[0]?.type).toBe('text')
    expect(result.content[0]?.text).toContain('getTransactions')
    expect(result.content[0]?.text).toContain('GET')
  })

  it('returns empty result message when no matches', () => {
    const result = buildSearchResult([])
    expect(result.content[0]?.text).toContain('No operations found')
  })

  it('includes operation details', () => {
    const result = buildSearchResult([
      {
        operationId: 'createBudget',
        method: 'POST',
        path: '/api/v1/budgets',
        summary: 'Create a budget',
        tags: ['Budgets'],
        manifestClass: 'write',
        excluded: false,
      },
    ])
    expect(result.content[0]?.text).toContain('createBudget')
    expect(result.content[0]?.text).toContain('write')
    expect(result.content[0]?.text).toContain('POST /api/v1/budgets')
  })

  it('does not include excluded operations in output', () => {
    const result = buildSearchResult([
      {
        operationId: 'checkRole',
        method: 'GET',
        path: '/api/v1/admin/check-role',
        summary: 'Check role',
        tags: ['Admin'],
        manifestClass: 'read',
        excluded: true,
      },
    ])
    // Even if buildSearchResult receives an excluded entry (shouldn't happen), it filters them
    expect(result.content[0]?.text).not.toContain('checkRole')
  })
})
