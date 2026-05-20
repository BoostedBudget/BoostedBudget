/**
 * PII Coverage Gate — #1133
 *
 * Scans document.body (not container) after rendering each page so that
 * portal-rendered content (Radix filter dropdowns, Sonner toasts) is included.
 *
 * Per-sentinel waitFor guards ensure each sentinel actually rendered before
 * scanning — a test that times out on a sentinel means the page isn't showing
 * that field at all, which is a different (also important) failure.
 *
 * All tests are expected to FAIL (RED) until the corresponding remediation
 * slice (Tasks 5–14) adds data-ph-mask coverage.
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test'
import { render, screen, waitFor, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import { scanForUnmaskedPII, type PIISentinel } from './pii-masking'
import { ThemeProvider } from '@/shared/theme/ThemeProvider'

// --- Centralized mocks (preloaded via bunfig.toml) ---
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import {
  mockGetTransactions,
  mockUseGetTransactions,
} from '@/test/mocks/transactions-api'
import { mockUseGetUserTags } from '@/test/mocks/tags-api'
import {
  mockUseGetUserCategories,
  mockUseGetPlaidCategories,
  mockUseGetPlaidPrimaryCategories,
} from '@/test/mocks/categories-api'
import {
  mockUseGetSavingsGoals,
  mockUseGetActiveSavingsGoals,
} from '@/test/mocks/savings-goals-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import {
  mockUseGetSpendingByMerchant,
  mockUseGetSpendingByCategory,
} from '@/test/mocks/analytics-api'
import { mockUseGetAutomationRules } from '@/test/mocks/automation-rules-api'
import { mockUseGetUserRules } from '@/test/mocks/auto-categorization-api'
import { mockUseGetBudgets } from '@/test/mocks/budgets-api'
import { mockUseNetWorthData } from '@/test/mocks/net-worth-hooks'

// --- Silence analytics in all tests (PostHog is no-op without POSTHOG_KEY) ---
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mock(() => {}),
}))

// --- Page imports (static — mocks are preloaded before module resolution) ---
const { TransactionTable } = await import('@/features/transactions/components/TransactionTable')
const { AccountsPage } = await import('@/features/accounts/AccountsPage')
const { NetWorthPage } = await import('@/features/net-worth/NetWorthPage')
const { SavingsPage } = await import('@/features/savings/SavingsPage')
const { ProfilePage } = await import('@/features/profile/ProfilePage')
const { CategoryEditor } = await import('@/features/categories/components/CategoryEditor')
const { BudgetsPage } = await import('@/features/budgets/components/BudgetsPage')
const { RulesPage } = await import('@/features/rules/components/RulesPage')
const { AutoCategorizationPage } = await import(
  '@/features/auto-categorization/components/AutoCategorizationPage'
)
const { SpendingReportsPage } = await import('@/features/reports/SpendingReportsPage')

// ---------------------------------------------------------------
// Sentinel constants
// ---------------------------------------------------------------
const SENTINEL_MERCHANT = 'SENTINEL_MERCHANT_XYZ'
const SENTINEL_NICKNAME = 'SENTINEL_NICKNAME_ABC'
const SENTINEL_DESC = 'SENTINEL_DESC_QRS'
const SENTINEL_TAG = 'SENTINEL_TAG_LMN'
const SENTINEL_INSTITUTION = 'SENTINEL_INSTITUTION_DEF'
const SENTINEL_BUDGET_NAME = 'SENTINEL_BUDGET_UVWX'
const SENTINEL_RULE_NAME = 'SENTINEL_RULE_IJKL'
const SENTINEL_GOAL_NAME = 'SENTINEL_GOAL_NOPQ'
const SENTINEL_CATEGORY_NAME = 'SENTINEL_CATEGORY_RSTU'
const SENTINEL_EMAIL = 'sentinel@pii-test.invalid'

// ---------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------
function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <MemoryRouter>{children}</MemoryRouter>
        </QueryClientProvider>
      </ThemeProvider>
    )
  }
}

function makeAccount(overrides: Record<string, unknown> = {}) {
  return {
    id: 'acc-sentinel',
    name: 'Sentinel Checking',
    nickname: SENTINEL_NICKNAME,
    institution_name: SENTINEL_INSTITUTION,
    mask: '1234',
    current_balance: 99999.99,
    available_balance: 99999.99,
    type: 'depository',
    subtype: 'checking',
    status: 'active',
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-1',
    user_id: 'user-1',
    is_hidden: false,
    currency_code: 'USD',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

function makeTransaction(overrides: Record<string, unknown> = {}) {
  return {
    id: 'txn-sentinel',
    user_id: 'user-1',
    account_id: 'acc-sentinel',
    linked_item_id: 'item-1',
    source: 'plaid' as const,
    amount: 99999.99,
    date: '2024-01-15',
    name: SENTINEL_MERCHANT,
    merchant_name: SENTINEL_MERCHANT,
    description: SENTINEL_DESC,
    tags: [SENTINEL_TAG],
    pending: false,
    created_at: '2024-01-15T00:00:00Z',
    updated_at: '2024-01-15T00:00:00Z',
    ...overrides,
  }
}

async function waitForSentinels(sentinels: PIISentinel[]) {
  for (const s of sentinels) {
    await waitFor(
      () =>
        expect(document.body.textContent ?? '').toContain(s.value),
      { timeout: 3000 }
    )
  }
}

// ---------------------------------------------------------------
// 1. Transactions page
// ---------------------------------------------------------------
describe('PII coverage — Transactions page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [makeAccount()] },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    mockUseGetTransactions.mockReturnValue({
      data: { transactions: [makeTransaction()], total_count: 1, limit: 50, offset: 0, has_more: false },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    mockGetTransactions.mockResolvedValue({
      transactions: [makeTransaction()],
      next_cursor: undefined,
      has_more: false,
      total_count: 1,
      limit: 50,
    })
    mockUseGetUserTags.mockReturnValue({ data: { tags: [] }, isLoading: false, error: null })
    mockUseGetUserCategories.mockReturnValue({ data: { categories: [] }, isLoading: false, error: null })
    mockUseGetPlaidCategories.mockReturnValue({ data: null, isLoading: false })
    mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: null, isLoading: false })
  })

  test('masks all PII', async () => {
    const tx = makeTransaction()
    const account = makeAccount()
    const accountsMap = { [account.id]: account as any }
    render(
      <TransactionTable
        transactions={[tx as any]}
        accountsMap={accountsMap}
        loading={false}
      />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'transaction.merchant_name' },
      { value: SENTINEL_TAG, fieldDescription: 'transaction.tags' },
      { value: SENTINEL_NICKNAME, fieldDescription: 'account.nickname' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 2. Accounts page
// ---------------------------------------------------------------
describe('PII coverage — Accounts page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [makeAccount()] },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
  })

  test('masks all PII including institution filter dropdown', async () => {
    const user = userEvent.setup()
    render(<AccountsPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_NICKNAME, fieldDescription: 'account.nickname' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'account.institution_name' },
    ]
    await waitForSentinels(sentinels)

    // AccountsPage defaults to cards view; switch to table to expose filter buttons.
    await user.click(screen.getByRole('button', { name: /^table$/i }))
    await waitFor(
      () => expect(screen.getAllByRole('button', { name: /filter by institution/i }).length).toBeGreaterThan(0),
      { timeout: 3000 }
    )

    // Open institution filter so Radix portal mounts options into document.body.
    // Without this the scanner never sees portaled option labels — the gate was blind.
    const filterBtns = screen.getAllByRole('button', { name: /filter by institution/i })
    await user.click(filterBtns[0])
    await waitFor(
      () => expect(screen.getByRole('listbox')).toBeInTheDocument(),
      { timeout: 3000 }
    )

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 3. Net Worth page
// ---------------------------------------------------------------
describe('PII coverage — Net Worth page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [makeAccount()] },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    mockUseNetWorthData.mockReturnValue({
      summary: {
        as_of_date: '2024-03-31',
        net_worth: 99999.99,
        total_assets: 99999.99,
        total_liabilities: 0,
        by_category: {},
      },
      history: null,
      isLoading: false,
      isHistoryLoading: false,
      refetch: mock(() => {}),
    })
  })

  test('masks all PII including institution filter dropdown', async () => {
    const user = userEvent.setup()
    render(<NetWorthPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_NICKNAME, fieldDescription: 'account.nickname' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'account.institution_name' },
    ]
    await waitForSentinels(sentinels)

    // Open institution filter so Radix portal mounts options into document.body.
    const filterBtns = screen.getAllByRole('button', { name: /filter by institution/i })
    await user.click(filterBtns[0])
    await waitFor(
      () => expect(screen.getByRole('listbox')).toBeInTheDocument(),
      { timeout: 3000 }
    )

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 4. Savings page
// ---------------------------------------------------------------
describe('PII coverage — Savings page', () => {
  const sentinelGoal = {
    id: 'goal-sentinel',
    user_id: 'user-1',
    name: SENTINEL_GOAL_NAME,
    description: 'Sentinel description',
    target_amount: 99999.99,
    current_amount: 12345.67,
    currency_code: 'USD',
    account_id: 'acc-sentinel',
    is_completed: false,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  beforeEach(() => {
    cleanup()
    mockUseGetSavingsGoals.mockReturnValue({
      data: [sentinelGoal],
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    mockUseGetActiveSavingsGoals.mockReturnValue({
      data: [sentinelGoal],
      isLoading: false,
      error: null,
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [makeAccount()] },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
  })

  test('masks all PII', async () => {
    render(<SavingsPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_GOAL_NAME, fieldDescription: 'savings_goal.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 5. Profile page
// ---------------------------------------------------------------
describe('PII coverage — Profile page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetUserProfile.mockReturnValue({
      data: {
        id: 'user-1',
        clerk_user_id: 'clerk_1',
        email: SENTINEL_EMAIL,
        email_verified: true,
        had_trial: false,
        is_active: true,
        is_deleted: false,
        primary_region: 'us-east-1',
        subscription_status: 'active',
        subscription_tier: 'premium',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
      },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
  })

  test('masks all PII', async () => {
    render(<ProfilePage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_EMAIL, fieldDescription: 'user.email' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 6. Categories component (no dedicated route — rendered directly)
// ---------------------------------------------------------------
describe('PII coverage — CategoryEditor component', () => {
  const sentinelCategory = {
    id: 'cat-sentinel',
    user_id: 'user-1',
    name: SENTINEL_CATEGORY_NAME,
    color: '#FF0000',
    usage_count: 1,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  beforeEach(() => {
    cleanup()
    mockUseGetUserCategories.mockReturnValue({
      data: { categories: [sentinelCategory] },
      isLoading: false,
      error: null,
    })
    mockUseGetPlaidPrimaryCategories.mockReturnValue({
      data: [{ enum: 'FOOD_AND_DRINK', display: 'Food and Drink' }],
      isLoading: false,
      error: null,
    })
  })

  test('masks user-authored category names', async () => {
    render(
      <CategoryEditor
        selectedCategory={[SENTINEL_CATEGORY_NAME]}
        availableCategories={[sentinelCategory]}
        plaidPrimaries={[{ enum: 'FOOD_AND_DRINK', display: 'Food and Drink' }]}
        onChange={mock(() => {})}
      />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_CATEGORY_NAME, fieldDescription: 'custom_category.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 7. Budgets list page
// ---------------------------------------------------------------
describe('PII coverage — Budgets list page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null })
    mockUseGetBudgets.mockReturnValue({
      data: [
        {
          id: 'budget-sentinel',
          user_id: 'user-1',
          name: SENTINEL_BUDGET_NAME,
          description: 'Sentinel budget description',
          currency_code: 'USD',
          period: 'monthly',
          start_date: '2024-01-01',
          end_date: '2024-01-31',
          amount: 99999.99,
          is_active: true,
          income_amount: 0,
          income_sources: [],
          categories: [],
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
  })

  test('masks all PII', async () => {
    render(<BudgetsPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_BUDGET_NAME, fieldDescription: 'budget.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 8. Automation Rules page
// ---------------------------------------------------------------
describe('PII coverage — Rules page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetAutomationRules.mockReturnValue({
      data: [
        {
          id: 'rule-sentinel',
          user_id: 'user-1',
          name: SENTINEL_RULE_NAME,
          description: 'Sentinel rule description',
          is_enabled: true,
          priority: 1,
          trigger_type: 'transaction',
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
    })
  })

  test('masks all PII', async () => {
    render(<RulesPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_RULE_NAME, fieldDescription: 'rule.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 9. Auto-categorization rules page
// ---------------------------------------------------------------
describe('PII coverage — Auto-categorization rules page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetUserRules.mockReturnValue({
      data: [
        {
          id: 'acat-sentinel',
          user_id: 'user-1',
          name: SENTINEL_RULE_NAME,
          is_enabled: true,
          priority: 1,
          target_tags: [SENTINEL_TAG],
          conditions: [],
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
    })
  })

  test('masks all PII', async () => {
    render(<AutoCategorizationPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_RULE_NAME, fieldDescription: 'auto_cat_rule.name' },
      { value: SENTINEL_TAG, fieldDescription: 'auto_cat_rule.target_tags' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ---------------------------------------------------------------
// 10. Reports / Spending page
// ---------------------------------------------------------------
describe('PII coverage — Spending Reports page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetSpendingByMerchant.mockReturnValue({
      data: {
        merchants: [
          {
            merchant_name: SENTINEL_MERCHANT,
            total: 99999.99,
            transaction_count: 5,
            percentage: 100.0,
          },
        ],
        total_spent: 99999.99,
      },
      isLoading: false,
      error: null,
    })
    mockUseGetSpendingByCategory.mockReturnValue({
      data: { categories: [], total_spent: 0 },
      isLoading: false,
      error: null,
    })
  })

  test('masks all PII', async () => {
    render(<SpendingReportsPage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'merchant.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})
