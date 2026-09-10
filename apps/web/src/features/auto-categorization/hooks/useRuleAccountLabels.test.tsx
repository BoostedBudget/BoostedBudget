/**
 * useRuleAccountLabels tests — Issue #1626 (Task K).
 *
 * The rule-condition surface is a DISPLAY/lookup path, not a selection path, so it must resolve
 * against the FULL account universe. `useAllAccounts`'s own docblock spells this out: a lookup
 * built from `useActiveAccounts` renders a retained (disconnected/replaced/expired) account as
 * "Unknown account". A rule can perfectly well name an account the user has since disconnected —
 * the rule row and the frozen execution snapshot both survive it — so the name must still resolve.
 */

import { describe, it, expect, beforeEach, afterAll } from 'bun:test'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Account } from '@boostedbudget/api-client/models'
import { mockUseGetAccounts, mockRefetchAccounts } from '@/test/mocks/accounts-api'
import { useRuleAccountLabels } from './useRuleAccountLabels'

const REMOVED_ACCOUNT_ID = '7b7b7b7b-2222-4222-8222-222222222222'
const ACTIVE_ACCOUNT_ID = '32a0e8da-8aa6-44ac-9be7-3a321e1402c7'

function accountsResult(accounts: Account[]) {
  return {
    data: { accounts },
    isLoading: false,
    isError: false,
    error: null,
    refetch: mockRefetchAccounts,
  } as unknown as ReturnType<typeof mockUseGetAccounts>
}

function renderLabels() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  return renderHook(() => useRuleAccountLabels(), {
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  })
}

describe('useRuleAccountLabels', () => {
  /** mock.module is process-global: restore the preloaded "still loading" default, never reset it
   * to undefined — `useAllAccounts` reads fields off the result. */
  const restoreLoadingDefault = () =>
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    } as unknown as ReturnType<typeof mockUseGetAccounts>)

  beforeEach(restoreLoadingDefault)
  afterAll(restoreLoadingDefault)

  it('resolves a RETAINED (removed) account name, not just active ones', () => {
    mockUseGetAccounts.mockReturnValue(
      accountsResult([
        { id: ACTIVE_ACCOUNT_ID, name: 'Everyday Spending', type: 'depository', status: 'active' },
        { id: REMOVED_ACCOUNT_ID, name: 'Old Credit Card', type: 'credit', status: 'removed' },
      ] as Account[])
    )

    const { result } = renderLabels()

    expect(result.current[ACTIVE_ACCOUNT_ID]).toBe('Everyday Spending')
    // Fails if this hook is ever narrowed to useActiveAccounts / filterActiveAccounts.
    expect(result.current[REMOVED_ACCOUNT_ID]).toBe('Old Credit Card')
  })

  it('returns an empty index while the accounts query is still loading', () => {
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    })

    const { result } = renderLabels()

    expect(result.current).toEqual({})
  })
})
