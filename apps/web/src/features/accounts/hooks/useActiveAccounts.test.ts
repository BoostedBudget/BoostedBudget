/**
 * useActiveAccounts Hook Tests
 *
 * Tests for the active accounts filter hook and helpers (Issue #1109).
 * Ensures replaced accounts are excluded across all consumers.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { filterActiveAccounts, isReplaced, useActiveAccounts } from './useActiveAccounts'
import type { Account } from '@boostedbudget/api-client/models'
import {
  mockUseGetAccounts,
  mockRefetchAccounts,
} from '@/test/mocks/accounts-api'
import { mockPosthogCapture } from '@/test/mocks/posthog'
import { mockUseUser } from '@/test/mocks/clerk'

// Re-assert the Clerk mock using the preloaded mockUseUser so that any inline
// mock.module('@clerk/react-router', ...) call in another test file (e.g.
// usePostHogIdentify.test.tsx) does not shadow mockUseUser here.
mock.module('@clerk/react-router', () => ({
  useUser: mockUseUser,
}))

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

const makeAccount = (overrides: Partial<Account>): Account => ({
  id: 'acc-default',
  linked_item_id: 'item-1',
  user_id: 'user-1',
  plaid_account_id: 'plaid-1',
  name: 'Test Account',
  type: 'depository',
  subtype: 'checking',
  currency_code: 'USD',
  is_hidden: false,
  status: 'active',
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
  ...overrides,
})

const activeAccount = makeAccount({ id: 'acc-active', status: 'active' })
const replacedAccount = makeAccount({ id: 'acc-replaced', status: 'replaced', replaced_by_id: 'acc-active' })
// legacyAccount: see legacyNoStatusAccount for the real undefined-status test

// Backward-compat: account created before status field was added — omit key entirely
const legacyNoStatusAccount: Account = (() => {
  const acc = makeAccount({ id: 'acc-no-status' })
  // Remove the status key to simulate undefined
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { status: _s, ...rest } = acc as Account & { status?: string }
  return rest as Account
})()

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(QueryClientProvider, { client: queryClient }, children)
  }
}

// ---------------------------------------------------------------------------
// filterActiveAccounts
// ---------------------------------------------------------------------------

describe('filterActiveAccounts', () => {
  it('filters out accounts with status "replaced"', () => {
    const result = filterActiveAccounts([activeAccount, replacedAccount])
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('acc-active')
  })

  it('keeps accounts with status "active"', () => {
    const result = filterActiveAccounts([activeAccount])
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('acc-active')
  })

  it('keeps accounts with null/undefined status for backward compatibility', () => {
    const result = filterActiveAccounts([legacyNoStatusAccount])
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('acc-no-status')
  })

  it('handles chain depth >= 2: two replaced accounts + one active, returns only active', () => {
    const replacedFirst = makeAccount({
      id: 'acc-replaced-1',
      status: 'replaced',
      replaced_by_id: 'acc-replaced-2',
    })
    const replacedSecond = makeAccount({
      id: 'acc-replaced-2',
      status: 'replaced',
      replaced_by_id: 'acc-active-final',
    })
    const activeFinal = makeAccount({ id: 'acc-active-final', status: 'active' })

    const result = filterActiveAccounts([replacedFirst, replacedSecond, activeFinal])
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe('acc-active-final')
  })

  it('returns empty array when all accounts are replaced', () => {
    const result = filterActiveAccounts([replacedAccount])
    expect(result).toHaveLength(0)
  })

  it('returns empty array for empty input', () => {
    expect(filterActiveAccounts([])).toHaveLength(0)
  })
})

// ---------------------------------------------------------------------------
// isReplaced
// ---------------------------------------------------------------------------

describe('isReplaced', () => {
  it('returns true for status "replaced"', () => {
    expect(isReplaced(replacedAccount)).toBe(true)
  })

  it('returns false for status "active"', () => {
    expect(isReplaced(activeAccount)).toBe(false)
  })

  it('returns false when status is undefined (legacy account)', () => {
    expect(isReplaced(legacyNoStatusAccount)).toBe(false)
  })
})

// ---------------------------------------------------------------------------
// useActiveAccounts hook
// ---------------------------------------------------------------------------

describe('useActiveAccounts', () => {
  beforeEach(() => {
    mockUseGetAccounts.mockClear()
    mockRefetchAccounts.mockClear()
    mockPosthogCapture.mockClear()
    // Reset to a known default so tests that don't call mockReturnValue explicitly
    // still get a predictable (signed-out) state.
    mockUseUser.mockReset()
    mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
    sessionStorage.clear()
  })

  it('returns only active accounts, excluding replaced ones', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [activeAccount, replacedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.accounts).toHaveLength(1)
    expect(result.current.accounts![0].id).toBe('acc-active')
  })

  it('preserves loading state when query is loading', () => {
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.isLoading).toBe(true)
    expect(result.current.accounts).toBeUndefined()
  })

  it('preserves error state when query errors', () => {
    const testError = new Error('Network failure')
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: testError,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.isError).toBe(true)
    expect(result.current.error).toBe(testError)
    expect(result.current.accounts).toBeUndefined()
  })

  it('keeps accounts with undefined status (backward compat)', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [legacyNoStatusAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.accounts).toHaveLength(1)
    expect(result.current.accounts![0].id).toBe('acc-no-status')
  })

  it('passes opts through to useGetAccounts', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    renderHook(() => useActiveAccounts({ include_hidden: 'true' }), {
      wrapper: createWrapper(),
    })

    expect(mockUseGetAccounts).toHaveBeenCalledWith({ include_hidden: 'true' }, undefined)
  })

  it('returns empty accounts array when data has no accounts', () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.accounts).toHaveLength(0)
  })
  it("returns undefined accounts when backend returns null accounts", () => {
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: null, total_count: 0 },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderHook(() => useActiveAccounts(), {
      wrapper: createWrapper(),
    })

    expect(result.current.accounts).toBeUndefined()
  })


  it('sets the sessionStorage sentinel after first render with replaced accounts', async () => {
    // The sentinel in sessionStorage is the mechanism that prevents the PostHog event
    // from firing more than once per session per user (Issue #1109 W4-D).
    // When no user is signed in, the sentinel key is 'ph_acct_replaced_filtered:anon'.
    mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
    const SENTINEL_KEY = 'ph_acct_replaced_filtered:anon'
    sessionStorage.removeItem(SENTINEL_KEY)

    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [activeAccount, replacedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    await act(async () => {
      renderHook(() => useActiveAccounts(), { wrapper: createWrapper() })
    })

    expect(sessionStorage.getItem(SENTINEL_KEY)).toBe('1')
  })

  it('does not set the sessionStorage sentinel when no accounts are replaced', async () => {
    mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
    const SENTINEL_KEY = 'ph_acct_replaced_filtered:anon'
    sessionStorage.removeItem(SENTINEL_KEY)

    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [activeAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    await act(async () => {
      renderHook(() => useActiveAccounts(), { wrapper: createWrapper() })
    })

    expect(sessionStorage.getItem(SENTINEL_KEY)).toBeNull()
  })

  it('does not reset the sentinel on second render (sentinel persists across hook instances)', async () => {
    // If the sentinel is already set, a second hook instance must not clear it.
    mockUseUser.mockReturnValue({ user: { id: 'user-persist-test' }, isLoaded: true, isSignedIn: true })
    const SENTINEL_KEY = 'ph_acct_replaced_filtered:user-persist-test'
    sessionStorage.setItem(SENTINEL_KEY, '1')

    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [activeAccount, replacedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    await act(async () => {
      renderHook(() => useActiveAccounts(), { wrapper: createWrapper() })
    })

    expect(sessionStorage.getItem(SENTINEL_KEY)).toBe('1')
  })

  it('fires the event separately for two different users in the same session (per-user sentinel)', async () => {
    // W4-D: The sentinel must be keyed by userId so a shared-browser session
    // (e.g., QA switch or household account) fires the event for each user independently.
    const accountsWithReplaced = {
      data: { accounts: [activeAccount, replacedAccount] },
      isLoading: false,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    }

    // User 1 renders the hook — their sentinel is set
    mockUseUser.mockReturnValue({ user: { id: 'user-alpha' }, isLoaded: true, isSignedIn: true })
    mockUseGetAccounts.mockReturnValue(accountsWithReplaced)
    await act(async () => {
      renderHook(() => useActiveAccounts(), { wrapper: createWrapper() })
    })
    expect(sessionStorage.getItem('ph_acct_replaced_filtered:user-alpha')).toBe('1')

    // User 2 now renders the hook — their separate sentinel must ALSO be set,
    // even though user-1's sentinel is already in sessionStorage
    mockUseUser.mockReturnValue({ user: { id: 'user-beta' }, isLoaded: true, isSignedIn: true })
    mockUseGetAccounts.mockReturnValue(accountsWithReplaced)
    await act(async () => {
      renderHook(() => useActiveAccounts(), { wrapper: createWrapper() })
    })
    expect(sessionStorage.getItem('ph_acct_replaced_filtered:user-beta')).toBe('1')
  })
})
