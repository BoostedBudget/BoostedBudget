/**
 * Unit tests for investmentTransactionsTable.constants.ts
 * Issue #1296 — R11-2 (onScrollEnd wiring guard).
 * Issue #1299 — formatTransactionType retired; ActivityTypeChip handles type rendering.
 *
 * These tests are the primary guards for:
 *  - MASKED_COLUMN_IDS identity (R11-1)
 *  - buildInvestmentVirtualization prop wiring: onScrollEnd === fetchNextPage (R11-2)
 */

import { describe, it, expect, mock } from 'bun:test'
import {
  MASKED_COLUMN_IDS,
  TXN_COLUMN_IDS,
  buildInvestmentVirtualization,
} from './investmentTransactionsTable.constants'

describe('MASKED_COLUMN_IDS', () => {
  it('equals the expected set of financial columns', () => {
    expect(MASKED_COLUMN_IDS).toEqual(['security', 'quantity', 'price', 'amount', 'fees'])
  })
})

describe('TXN_COLUMN_IDS', () => {
  it('contains all 7 expected column ids in display order', () => {
    expect([...TXN_COLUMN_IDS]).toEqual([
      'date',
      'type',
      'security',
      'quantity',
      'price',
      'amount',
      'fees',
    ])
  })
})

describe('buildInvestmentVirtualization', () => {
  it('wires onScrollEnd to fetchNextPage (R11-2)', () => {
    const fetchNextPage = mock(() => Promise.resolve())
    const config = buildInvestmentVirtualization({
      fetchNextPage,
      hasNextPage: true,
      isFetchingNextPage: false,
    })
    // PRIMARY GUARD: removing/no-op-ing onScrollEnd in InvestmentTransactionsTable
    // would break this assertion. fetch page == scroll end.
    expect(config.onScrollEnd).toBe(fetchNextPage)
  })

  it('wires hasMore to hasNextPage', () => {
    const config = buildInvestmentVirtualization({
      fetchNextPage: () => Promise.resolve(),
      hasNextPage: true,
      isFetchingNextPage: false,
    })
    expect(config.hasMore).toBe(true)

    const configFalse = buildInvestmentVirtualization({
      fetchNextPage: () => Promise.resolve(),
      hasNextPage: false,
      isFetchingNextPage: false,
    })
    expect(configFalse.hasMore).toBe(false)
  })

  it('wires isFetchingMore to isFetchingNextPage', () => {
    const config = buildInvestmentVirtualization({
      fetchNextPage: () => Promise.resolve(),
      hasNextPage: false,
      isFetchingNextPage: true,
    })
    expect(config.isFetchingMore).toBe(true)
  })

  it('always sets enabled: true', () => {
    const config = buildInvestmentVirtualization({
      fetchNextPage: () => Promise.resolve(),
      hasNextPage: false,
      isFetchingNextPage: false,
    })
    expect(config.enabled).toBe(true)
  })
})
