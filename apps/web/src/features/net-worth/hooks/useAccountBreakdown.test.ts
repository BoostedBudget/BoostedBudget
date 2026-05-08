import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook } from '@testing-library/react'
import type { Account } from '@boostedbudget/api-client/models/account'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'

const { useAccountBreakdown } = await import('./useAccountBreakdown')

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 'acc-1',
    name: 'Checking',
    type: 'depository',
    current_balance: 1000,
    currency_code: 'USD',
    is_hidden: false,
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-1',
    created_at: '2024-01-01',
    updated_at: '2024-01-01',
    user_id: 'user-1',
    status: 'active',
    ...overrides,
  }
}

describe('useAccountBreakdown', () => {
  beforeEach(() => {
    mockUseGetAccounts.mockReset()
  })

  it('returns empty arrays when no accounts', () => {
    mockUseGetAccounts.mockReturnValue({ data: { accounts: [], total_count: 0 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toEqual([])
    expect(result.current.liabilities).toEqual([])
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('classifies depository accounts as assets', () => {
    const accounts = [makeAccount({ type: 'depository', current_balance: 2000 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toHaveLength(1)
    expect(result.current.liabilities).toHaveLength(0)
  })

  it('classifies credit accounts as liabilities', () => {
    const accounts = [makeAccount({ type: 'credit', current_balance: -500 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toHaveLength(0)
    expect(result.current.liabilities).toHaveLength(1)
  })

  it('classifies loan accounts as liabilities', () => {
    const accounts = [makeAccount({ type: 'loan', current_balance: -10000 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.liabilities).toHaveLength(1)
  })

  it('filters out hidden accounts', () => {
    const accounts = [
      makeAccount({ is_hidden: true, current_balance: 1000 }),
      makeAccount({ id: 'acc-2', name: 'Savings', current_balance: 2000 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toHaveLength(1)
    expect(result.current.assets[0].name).toBe('Savings')
  })

  // --- Percent formula: side-specific denominators ---

  it('computes percentOfSide as balance / sum-of-asset-balances for assets', () => {
    const accounts = [
      makeAccount({ id: 'acc-1', current_balance: 1000 }),
      makeAccount({ id: 'acc-2', current_balance: 2000 }),
      makeAccount({ id: 'acc-3', current_balance: 7000 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 3 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    const percs = result.current.assets.map((r) => r.percentOfSide)
    expect(percs[0]).toBeCloseTo(10, 1)
    expect(percs[1]).toBeCloseTo(20, 1)
    expect(percs[2]).toBeCloseTo(70, 1)
  })

  it('computes percentOfSide as abs(balance) / sum-of-abs-liability-balances for liabilities', () => {
    // Mix positive and negative signs — Plaid may return either
    const accounts = [
      makeAccount({ id: 'acc-1', type: 'credit', current_balance: -100 }),
      makeAccount({ id: 'acc-2', type: 'credit', current_balance: 300 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    const percs = result.current.liabilities.map((r) => r.percentOfSide)
    expect(percs[0]).toBeCloseTo(25, 1)
    expect(percs[1]).toBeCloseTo(75, 1)
  })

  it('percent never exceeds 100% even when net worth is small/negative (regression: 219.8% scenario)', () => {
    // Reproduce: liabilities total $184k, assets total $154k, net worth −$30k
    // Single liability of $65k — old formula: 65000/30000 * 100 = 216.7%; new: 65000/184000 * 100 ≈ 35.3%
    const assets = [
      makeAccount({ id: 'a-1', type: 'depository', current_balance: 110 }),
      makeAccount({ id: 'a-2', type: 'depository', current_balance: 43200 }),
      makeAccount({ id: 'a-3', type: 'investment', current_balance: 23632 }),
      makeAccount({ id: 'a-4', type: 'depository', current_balance: 6009 }),
      makeAccount({ id: 'a-5', type: 'depository', current_balance: 12060 }),
      makeAccount({ id: 'a-6', type: 'investment', current_balance: 23632 }),
      makeAccount({ id: 'a-7', type: 'investment', current_balance: 321 }),
      makeAccount({ id: 'a-8', type: 'depository', current_balance: 43200 }),
      makeAccount({ id: 'a-9', type: 'depository', current_balance: 1000 }),
      makeAccount({ id: 'a-10', type: 'depository', current_balance: 210 }),
      makeAccount({ id: 'a-11', type: 'depository', current_balance: 110 }),
      makeAccount({ id: 'a-12', type: 'depository', current_balance: 210 }),
      makeAccount({ id: 'a-13', type: 'depository', current_balance: 110 }),
      makeAccount({ id: 'a-14', type: 'depository', current_balance: 210 }),
    ]
    const liabilities = [
      makeAccount({ id: 'l-1', type: 'loan', current_balance: 56302 }),
      makeAccount({ id: 'l-2', type: 'credit', current_balance: 410 }),
      makeAccount({ id: 'l-3', type: 'credit', current_balance: 5020 }),
      makeAccount({ id: 'l-4', type: 'loan', current_balance: 56302 }),
      makeAccount({ id: 'l-5', type: 'loan', current_balance: 65262 }),
      makeAccount({ id: 'l-6', type: 'credit', current_balance: 410 }),
    ]
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [...assets, ...liabilities], total_count: assets.length + liabilities.length },
      isLoading: false,
      error: null,
    })
    const { result } = renderHook(() => useAccountBreakdown())
    for (const row of result.current.assets) {
      expect(row.percentOfSide).toBeLessThanOrEqual(100)
      expect(row.percentOfSide).toBeGreaterThanOrEqual(0)
    }
    for (const row of result.current.liabilities) {
      expect(row.percentOfSide).toBeLessThanOrEqual(100)
      expect(row.percentOfSide).toBeGreaterThanOrEqual(0)
    }
  })

  it('clamps negative-balance asset rows to 0% and excludes them from assetsTotal', () => {
    // Assets [100, -50] — the -50 row should render 0%, not -100% / 200%
    const accounts = [
      makeAccount({ id: 'acc-1', type: 'depository', current_balance: 100 }),
      makeAccount({ id: 'acc-2', type: 'depository', current_balance: -50 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    const byId = Object.fromEntries(result.current.assets.map((r) => [r.id, r]))
    expect(byId['acc-1'].percentOfSide).toBeCloseTo(100, 1)
    expect(byId['acc-2'].percentOfSide).toBe(0)
  })

  it('returns 0% for all rows when the side total is 0', () => {
    // All asset balances are 0 — no divide-by-zero
    const accounts = [
      makeAccount({ id: 'acc-1', type: 'depository', current_balance: 0 }),
      makeAccount({ id: 'acc-2', type: 'depository', current_balance: 0 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    for (const row of result.current.assets) {
      expect(row.percentOfSide).toBe(0)
    }
  })

  it('reports assetsTotal and liabilitiesTotal', () => {
    const accounts = [
      makeAccount({ id: 'acc-1', type: 'depository', current_balance: 3000 }),
      makeAccount({ id: 'acc-2', type: 'credit', current_balance: -1000 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assetsTotal).toBe(3000)
    expect(result.current.liabilitiesTotal).toBe(1000)
  })

  // --- Institution ---

  it('populates institutionName from account.institution_name', () => {
    const accounts = [makeAccount({ institution_name: 'Chase' })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets[0].institutionName).toBe('Chase')
  })

  it('falls back institutionName to "Unknown" when institution_name is missing', () => {
    const accounts = [makeAccount({ institution_name: undefined })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets[0].institutionName).toBe('Unknown')
  })

  // --- Name / mask ---

  it('uses nickname as row name and drops mask from row name', () => {
    const accounts = [
      makeAccount({ nickname: 'My Checking', name: 'Plaid Checking', mask: '0000' }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    const row = result.current.assets[0]
    expect(row.name).toBe('My Checking')
    // mask must not appear anywhere in the row name
    expect(row.name).not.toContain('0000')
    expect(row.name).not.toContain('••••')
  })

  it('falls back to account name when nickname is absent', () => {
    const accounts = [makeAccount({ nickname: undefined, name: 'Plaid Savings' })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets[0].name).toBe('Plaid Savings')
  })

  // --- Re-link chain (Issue #1109) ---

  it('filters out replaced accounts from breakdown', () => {
    // Issue #1109: Now that backend returns active-only by default and the
    // filterActiveAccounts helper is wired in, replaced accounts should not
    // appear in the net-worth breakdown.
    const accounts = [
      makeAccount({ id: 'active', name: 'Active Checking', status: 'active', current_balance: 1000 }),
      makeAccount({ id: 'replaced', name: 'Old Checking', status: 'replaced', current_balance: 500 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toHaveLength(1)
    expect(result.current.assets[0].id).toBe('active')
    expect(result.current.assets.some((r) => r.id === 'replaced')).toBe(false)
  })

  // --- Loading / error / null propagation ---

  it('propagates isLoading from useGetAccounts', () => {
    mockUseGetAccounts.mockReturnValue({ data: null, isLoading: true, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.loading).toBe(true)
  })

  it('propagates error from useGetAccounts', () => {
    const err = new Error('Network error')
    mockUseGetAccounts.mockReturnValue({ data: null, isLoading: false, error: err })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.error).toBe(err)
  })

  it('handles null accounts in response', () => {
    mockUseGetAccounts.mockReturnValue({ data: { accounts: null, total_count: 0 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown())
    expect(result.current.assets).toEqual([])
    expect(result.current.liabilities).toEqual([])
  })
})
