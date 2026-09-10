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
import type { Account, GetAccountsParams } from '@boostedbudget/api-client/models'
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

  /**
   * The final-gate regression, and it exists because the test above CANNOT catch its own subject.
   *
   * ⚠ WHY A MOCK-RETURN TEST IS NOT ENOUGH. The case above hands the removed account straight back
   * through `mockUseGetAccounts` and never looks at what was ASKED for, so it passes whatever query
   * params the hook sends — including params that would never have returned that account from the
   * real backend. The omission this pins was exactly that shape: the hook asked for
   * `status: 'all_statuses'` but not `include_hidden: 'true'`, and the backend excludes hidden
   * accounts unless that param is present (`account_repository.go`'s `if !opts.IncludeHidden`
   * appends `is_hidden = FALSE`). A rule naming an account the user later HID therefore rendered
   * "Unknown account" in production while every test here stayed green.
   *
   * So this asserts the REQUEST, not the response — and on every call, because one un-scoped call
   * is enough to lose the account.
   */
  it('asks for hidden accounts as well as every status, so a hidden account still resolves', () => {
    mockUseGetAccounts.mockClear()
    mockUseGetAccounts.mockReturnValue(accountsResult([]))

    renderLabels()

    expect(mockUseGetAccounts).toHaveBeenCalled()
    // The preloaded factory in src/test/mocks/accounts-api.ts is declared as `mock(() => ({…}))`
    // — zero parameters — so `mock.calls` is typed as a tuple of length 0 and indexing it is
    // TS2493. The recorded args are real at runtime; only the declared type is empty.
    const calls = mockUseGetAccounts.mock.calls as unknown as Array<[GetAccountsParams | undefined]>
    for (const [params] of calls) {
      // Both keys, together: `all_statuses` widens the STATUS axis (active/replaced/expired/
      // removed) and says nothing about hiding, which is a separate per-account flag.
      expect(params).toMatchObject({ status: 'all_statuses', include_hidden: 'true' })
    }
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
