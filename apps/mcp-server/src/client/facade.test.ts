import { describe, expect, it, mock } from 'bun:test'
import { createApiClient } from './generated/facade'
import type { FetchMutator } from './fetch-mutator'

describe('createApiClient', () => {
  it('returns a client with expected namespaces', () => {
    const mutator = mock(() => Promise.resolve({})) as unknown as FetchMutator
    const client = createApiClient(mutator)

    expect(client.analytics).toBeDefined()
    expect(client.transactions).toBeDefined()
    expect(client.budgets).toBeDefined()
    expect(client.categories).toBeDefined()
    expect(client.tags).toBeDefined()
    expect(client.savingsGoals).toBeDefined()
    expect(client.accounts).toBeDefined()
    expect(client.notifications).toBeDefined()
  })

  it('does NOT expose excluded operations', () => {
    const mutator = mock(() => Promise.resolve({})) as unknown as FetchMutator
    const client = createApiClient(mutator)

    // Admin ops — excluded
    expect((client as Record<string, unknown>).admin).toBeUndefined()

    // Plaid linking — permanently excluded
    const plaid = (client as Record<string, unknown>).plaid as Record<string, unknown> | undefined
    expect(plaid?.exchangePublicToken).toBeUndefined()
    expect(plaid?.createLinkToken).toBeUndefined()
  })

  it('calls the mutator with correct url and method for getTransactions', async () => {
    const mutator = mock(() => Promise.resolve({ transactions: [] })) as unknown as FetchMutator
    const client = createApiClient(mutator)

    await client.transactions.getTransactions()

    expect(mutator).toHaveBeenCalledWith(
      expect.objectContaining({ url: '/api/v1/transactions', method: 'GET' }),
    )
  })

  it('substitutes path params for updateTransactionCategory', async () => {
    const mutator = mock(() => Promise.resolve({})) as unknown as FetchMutator
    const client = createApiClient(mutator)

    await client.transactions.updateTransactionCategory('txn-123', { categoryId: 'cat-1' })

    expect(mutator).toHaveBeenCalledWith(
      expect.objectContaining({
        url: '/api/v1/transactions/txn-123/category',
        method: 'PUT',
      }),
    )
  })

  it('passes query params for getSpendingByCategory', async () => {
    const mutator = mock(() => Promise.resolve({})) as unknown as FetchMutator
    const client = createApiClient(mutator)

    const params = { start_date: '2026-01-01', end_date: '2026-01-31' }
    await client.analytics.getSpendingByCategory(params)

    expect(mutator).toHaveBeenCalledWith(
      expect.objectContaining({
        url: '/api/v1/analytics/spending-by-category',
        params,
      }),
    )
  })

  it('passes data for createBudget', async () => {
    const mutator = mock(() => Promise.resolve({ id: 'budget-1' })) as unknown as FetchMutator
    const client = createApiClient(mutator)

    const budgetData = { name: 'Monthly Budget', amount: 2000 }
    await client.budgets.createBudget(budgetData)

    expect(mutator).toHaveBeenCalledWith(
      expect.objectContaining({
        url: '/api/v1/budgets',
        method: 'POST',
        data: budgetData,
      }),
    )
  })

  it('contains exactly 141 allowed operation methods', () => {
    const mutator = mock(() => Promise.resolve({})) as unknown as FetchMutator
    const client = createApiClient(mutator)

    let count = 0
    for (const ns of Object.values(client)) {
      if (typeof ns === 'object' && ns !== null) {
        count += Object.values(ns as object).filter((v) => typeof v === 'function').length
      }
    }
    // 193 total ops − 60 excluded = 133 allowed; mirrors capability-manifest.test.ts
    // +7 from #957: getMerchants, getMerchantOverrides, getMerchantDuplicateSuggestions,
    //               setMerchantOverride, clearMerchantOverride, bulkSetMerchantOverrides,
    //               mergeMerchants
    // +1 from #1366: getHouseholdEntitlement
    // +12 from #1367 Phase 3 (Gap Analysis Round 2, GT-4): +4 allowed (getMyHousehold,
    //                listHouseholdMembers, listHouseholdInvites, getHouseholdInvitePreview),
    //                +8 excluded (household mutations — deliberate in-app action)
    // +1 from #1368 Phase 4: listMyAccountShares allowed (read); setHouseholdShareVisibility
    //               excluded (privacy/consent decision — deliberate in-app action)
    // +1 net from #1569 Task 16: getDeletedSavingsGoals and getUserBoostExecutions classified
    //                as read (both were already in api/openapi.yaml but in no manifest entry),
    //                and deleteSavingsGoalContribution excluded — it hard-deletes a savings
    //                ledger row, so the facade must no longer be able to reach it. purgeSavingsGoal
    //                is also newly classified but excluded, so it adds no facade method.
    // +1 from #1569: getAccountBalanceHistory (read) — the reader for balance_snapshots, which
    //                was written by every sync and had no per-account reader at all.
    expect(count).toBe(141)
  })
})
