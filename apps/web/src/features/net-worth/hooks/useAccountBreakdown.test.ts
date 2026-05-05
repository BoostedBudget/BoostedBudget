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
    const { result } = renderHook(() => useAccountBreakdown(5000))
    expect(result.current.assets).toEqual([])
    expect(result.current.liabilities).toEqual([])
    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('classifies depository accounts as assets', () => {
    const accounts = [makeAccount({ type: 'depository', current_balance: 2000 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(5000))
    expect(result.current.assets).toHaveLength(1)
    expect(result.current.liabilities).toHaveLength(0)
  })

  it('classifies credit accounts as liabilities', () => {
    const accounts = [makeAccount({ type: 'credit', current_balance: -500 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(4500))
    expect(result.current.assets).toHaveLength(0)
    expect(result.current.liabilities).toHaveLength(1)
  })

  it('classifies loan accounts as liabilities', () => {
    const accounts = [makeAccount({ type: 'loan', current_balance: -10000 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(-5000))
    expect(result.current.liabilities).toHaveLength(1)
  })

  it('filters out hidden accounts', () => {
    const accounts = [
      makeAccount({ is_hidden: true, current_balance: 1000 }),
      makeAccount({ id: 'acc-2', name: 'Savings', current_balance: 2000 }),
    ]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 2 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(2000))
    expect(result.current.assets).toHaveLength(1)
    expect(result.current.assets[0].name).toBe('Savings')
  })

  it('computes percentOfNetWorth as balance / abs(netWorth)', () => {
    const accounts = [makeAccount({ current_balance: 2500 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(5000))
    expect(result.current.assets[0].percentOfNetWorth).toBeCloseTo(50, 1)
  })

  it('returns 0% when netWorth is 0 (divide-by-zero guard)', () => {
    const accounts = [makeAccount({ current_balance: 1000 })]
    mockUseGetAccounts.mockReturnValue({ data: { accounts, total_count: 1 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(0))
    expect(result.current.assets[0].percentOfNetWorth).toBe(0)
  })

  it('propagates isLoading from useGetAccounts', () => {
    mockUseGetAccounts.mockReturnValue({ data: null, isLoading: true, error: null })
    const { result } = renderHook(() => useAccountBreakdown(0))
    expect(result.current.loading).toBe(true)
  })

  it('propagates error from useGetAccounts', () => {
    const err = new Error('Network error')
    mockUseGetAccounts.mockReturnValue({ data: null, isLoading: false, error: err })
    const { result } = renderHook(() => useAccountBreakdown(0))
    expect(result.current.error).toBe(err)
  })

  it('handles null accounts in response', () => {
    mockUseGetAccounts.mockReturnValue({ data: { accounts: null, total_count: 0 }, isLoading: false, error: null })
    const { result } = renderHook(() => useAccountBreakdown(0))
    expect(result.current.assets).toEqual([])
    expect(result.current.liabilities).toEqual([])
  })
})
