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

import { describe, test, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, within, waitFor, cleanup, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import { scanForUnmaskedPII, type PIISentinel } from './pii-masking'
import { ThemeProvider } from '@/shared/theme/ThemeProvider'

// --- Centralized mocks (preloaded via bunfig.toml) ---
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import {
  mockGetTransactions,
  mockUseGetTransactions,
  mockUseGetTransactionReviewSummary,
  mockUseSetTransactionReviewed,
  mockSetTransactionReviewedMutateAsync,
  mockUseBulkSetTransactionReviewed,
  mockBulkSetTransactionReviewedMutateAsync,
  mockUseBulkUpdateTransactionCategory,
  mockBulkUpdateTransactionCategoryMutateAsync,
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
  mockUseGetCashFlowSummary,
  mockUseGetPortfolioAllocationBreakdown,
} from '@/test/mocks/analytics-api'
import { mockUseGetBoosts } from '@/test/mocks/boosts-api'
import { mockUseGetUserRules } from '@/test/mocks/auto-categorization-api'
import {
  mockUseGetBudgets,
  mockUseGetCurrentBudget,
  mockUseGetCategoryTransactions,
} from '@/test/mocks/budgets-api'
import { mockUseNetWorthData } from '@/test/mocks/net-worth-hooks'
import {
  mockUseGetHoldings,
  mockUseGetPortfolioSummary,
  mockUseGetPortfolioHistory,
  mockUseInfiniteInvestmentTransactions,
  mockFetchNextInvestmentPage,
} from '@/test/mocks/investments-api'
import { mockUseSavingsImpactData } from '@/test/mocks/savings-impact-hooks'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'
import { mockUsePreviewPaddleSubscriptionUpdate } from '@/test/mocks/subscription-api'
import {
  mockUseGetAdminUserSubscription,
  mockUseRemoveAdminUserTierOverride,
} from '@/test/mocks/admin-subscriptions-api'
// Round 15: admin operator surfaces (#1497 / Z1).
import {
  mockUseListAdminPlaidOrphanedItems,
  mockUseRecordAdminPlaidOrphanedItemOperatorResolution,
  mockUseGetAdminPaddleWebhookDetail,
  mockUseReplayAdminPaddleWebhook,
} from '@/test/mocks/admin-api'
// EChartsChart mock — must be imported before any chart component dynamic imports
// so that mock.module('@/shared/charts') is registered at load time.
import '@/test/mocks/shared-charts'
import { mockUseDarkMode } from '@/test/mocks/shared-charts'

// --- Page imports (static — mocks are preloaded before module resolution) ---
const { TransactionTable } = await import('@/features/transactions/components/TransactionTable')
const { AccountsPage } = await import('@/features/accounts/AccountsPage')
const { NetWorthPage } = await import('@/features/net-worth/NetWorthPage')
const { SavingsPage } = await import('@/features/savings/SavingsPage')
const { ProfilePage } = await import('@/features/profile/ProfilePage')
const { CategoryEditor } = await import('@/features/categories/components/CategoryEditor')
const { BudgetsPage } = await import('@/features/budgets/components/BudgetsPage')
const { BoostsManagePage } = await import('@/features/boosts/components/BoostsManagePage')
const { AutoCategorizationPage } = await import(
  '@/features/auto-categorization/components/AutoCategorizationPage'
)
const { SpendingReportsPage } = await import('@/features/reports/SpendingReportsPage')
const { SavingsImpactWidget } = await import(
  '@/features/dashboard/widgets/SavingsImpactWidget'
)

const { CashFlowSummaryWidget } = await import('@/features/dashboard/widgets/CashFlowSummaryWidget')
const { BudgetOverviewWidget } = await import('@/features/dashboard/widgets/BudgetOverviewWidget')
const { AccountBalanceCards } = await import('@/features/dashboard/components/AccountBalanceCards')
const { BudgetTransactionList } = await import('@/features/budgets/components/transaction-link/BudgetTransactionList')
const { SavingsGoalActionForm } = await import('@/features/boosts/components/actions/SavingsGoalActionForm')
const { IncomeSourceItem } = await import('@/features/budgets/components/income/IncomeSourceItem')
const { IncomeSourceConfig } = await import('@/features/budgets/components/income/IncomeSourceConfig')
const { DashboardContext } = await import('@/features/dashboard/context/DashboardContext')

const { AccountTypeFilter } = await import('@/features/dashboard/components/AccountTypeFilter')
const { BudgetIncomeSection } = await import('@/features/budgets/components/budget-detail/BudgetIncomeSection')
const { BudgetCategoryForm } = await import('@/features/budgets/components/category-management/BudgetCategoryForm')

const { BalanceOverview } = await import('@/features/dashboard/components/BalanceOverview')
const { NetWorthSparkline } = await import('@/features/dashboard/components/NetWorthSparkline')

const { CurrentSubscriptionDisplay } = await import('@/features/billing/components/CurrentSubscriptionDisplay')
const { PlanChangeModal } = await import('@/features/billing/components/PlanChangeModal')
const { SubscriptionOverrideModal } = await import('@/features/admin/components/SubscriptionOverrideModal')

const { AutoCategorizationRuleTable } = await import(
  '@/features/auto-categorization/components/AutoCategorizationRuleTable'
)

const { ChangeIndicator } = await import('@/features/net-worth/components/ChangeIndicator')

const { SearchableSelect } = await import('@/shared/components/SearchableSelect/SearchableSelect')

// --- Round 13 component imports (#1296: Portfolio monetary surfaces) ---
const { PortfolioSummaryCard } = await import('@/features/investments/components/PortfolioSummaryCard')
const { HoldingsTable } = await import('@/features/investments/components/HoldingsTable')
const { InvestmentTransactionsTable } = await import('@/features/investments/components/InvestmentTransactionsTable')
const { PortfolioValueChart } = await import('@/features/investments/components/PortfolioValueChart')
const { AllocationCard } = await import('@/features/investments/components/AllocationCard')

// --- Round 14: Investment dashboard widgets (#1297) ---
const { PortfolioSummaryWidget } = await import('@/features/dashboard/widgets/PortfolioSummaryWidget')
const { PortfolioHoldingsWidget } = await import('@/features/dashboard/widgets/PortfolioHoldingsWidget')
const { AssetAllocationWidget } = await import('@/features/dashboard/widgets/AssetAllocationWidget')

const { CashFlowSankeyChart } = await import('@/features/reports/components/CashFlowSankeyChart')
const { AccountCustomizationModal } = await import('@/features/accounts/components/AccountCustomizationModal')
const { ReviewInboxPage } = await import('@/features/transactions/review/ReviewInboxPage')
const { TransferActionForm } = await import('@/features/boosts/components/actions/TransferActionForm')
const { CreateSavingsGoalDialog } = await import('@/features/savings/components/CreateSavingsGoalDialog')

// --- Round 15: admin operator surfaces that render free text (#1497 / Z1) ---
const { PlaidOrphanedItemsPage } = await import('@/features/admin/components/PlaidOrphanedItemsPage')
const { UserDetailModal } = await import('@/features/admin/components/UserDetailModal')
const { WebhookDetailModal } = await import('@/features/admin/components/WebhookDetailModal')

const SENTINEL_MERCHANT = 'SENTINEL_MERCHANT_XYZ'
const SENTINEL_NICKNAME = 'SENTINEL_NICKNAME_ABC'
const SENTINEL_DESC = 'SENTINEL_DESC_QRS'
const SENTINEL_TAG = 'SENTINEL_TAG_LMN'
const SENTINEL_INSTITUTION = 'SENTINEL_INSTITUTION_DEF'
const SENTINEL_BUDGET_NAME = 'SENTINEL_BUDGET_UVWX'
const SENTINEL_RULE_NAME = 'SENTINEL_RULE_IJKL'
const SENTINEL_RULE_DESC = 'SENTINEL_RULE_DESC_MNOP'
const SENTINEL_GOAL_NAME = 'SENTINEL_GOAL_NOPQ'
const SENTINEL_CATEGORY_NAME = 'SENTINEL_CATEGORY_RSTU'
const SENTINEL_EMAIL = 'sentinel@pii-test.invalid'
const SENTINEL_ACCOUNT_NAME = 'SENTINEL_ACCOUNT_ZXCV'
const SENTINEL_SECURITY_NAME = 'SENTINEL_SECURITY_QRST'

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
    // Known limitation: TransactionTable is rendered directly (not TransactionsPage),
    // so the account filter dropdown portal on TransactionsPage is not exercised here.
    // Table-cell masking (maskedColumnIds) is verified above.
  })
})

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

  // Switching to table view writes 'accounts-list-view' to localStorage; remove it
  // so this test file doesn't pollute AccountsPage.test.tsx when the suite runs together.
  afterEach(() => {
    localStorage.removeItem('accounts-list-view')
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
    const listbox = await waitFor(
      () => screen.getByRole('listbox'),
      { timeout: 3000 }
    )
    // Confirm the sentinel actually appears inside the portal (not just on the page body).
    expect(within(listbox).getByText(SENTINEL_INSTITUTION)).toBeInTheDocument()

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

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
    // Without this the scanner never sees portaled option labels — the gate was blind.
    const filterBtns = screen.getAllByRole('button', { name: /filter by institution/i })
    await user.click(filterBtns[0])
    const listbox = await waitFor(
      () => screen.getByRole('listbox'),
      { timeout: 3000 }
    )
    // Confirm the sentinel actually appears inside the portal (not just on the page body).
    expect(within(listbox).getByText(SENTINEL_INSTITUTION)).toBeInTheDocument()

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

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

describe('PII coverage — Rules page', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetBoosts.mockReturnValue({
      data: [
        {
          id: 'rule-sentinel',
          user_id: 'user-1',
          name: SENTINEL_RULE_NAME,
          description: 'Sentinel rule description',
          enabled: true,
          priority: 1,
          boost_type: 'transfer_suggestion',
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
    })
  })

  test('masks all PII', async () => {
    render(<BoostsManagePage />, { wrapper: createWrapper() })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_RULE_NAME, fieldDescription: 'rule.name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

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

describe('PII coverage — Cash Flow Sankey (G6 — Codex fix #2 regression guard)', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
  })

  test('currency amounts in sr-only table are covered by data-ph-mask; ECharts wrapper carries data-ph-mask', async () => {
    const incomeData = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_income: 5000,
      sources: [
        { category: 'INCOME_WAGES', pfc_primary_display: 'Wages', total: 5000, percentage: 100, transaction_count: 2 },
      ],
    }
    const categoryData = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_spent: 3000,
      categories: [
        { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food & Drink', total: 1800, percentage: 60, transaction_count: 20 },
        { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 1200, percentage: 40, transaction_count: 8 },
      ],
    }

    render(
      <CashFlowSankeyChart
        incomeData={incomeData}
        categoryData={categoryData}
        loading={false}
        privacyMode={false}
        startDate="2024-01-01"
        endDate="2024-01-31"
      />,
      { wrapper: createWrapper() },
    )

    // Wait for the sr-only table (rendered when there are links to display)
    await waitFor(() => { expect(document.querySelector('table')).not.toBeNull() }, { timeout: 3000 })

    // ECharts wrapper must carry data-ph-mask (chart canvas)
    const echartsWrapper = document.querySelector('[data-testid="echarts-chart"]')
    expect(echartsWrapper).not.toBeNull()
    expect(echartsWrapper?.getAttribute('data-ph-mask')).toBe('')

    // No currency text should be visible outside a masked ancestor.
    // The sr-only table has data-ph-mask — all its currency cells should be covered.
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

describe('PII coverage — SavingsImpactWidget destination name', () => {
  beforeEach(() => {
    cleanup()
    mockUseDashboardContext.mockReturnValue({
      filters: { startDate: '2024-01-01', endDate: '2024-01-31', accountTypes: undefined },
      setFilters: mock(() => {}),
      timeframe: 'this_month' as const,
      setTimeframe: mock(() => {}),
      isEditing: false,
      setIsEditing: mock(() => {}),
      hasAccounts: true,
      accountsLoading: false,
    })
    mockUseSavingsImpactData.mockReturnValue({
      data: {
        total_amount: 500,
        completed_count: 1,
        month_amount: 500,
        prev_month_amount: undefined,
        recent_items: [
          {
            id: 'pii-gate-item',
            amount: 500,
            actual_amount: 500,
            completed_at: '2024-04-01T12:00:00Z',
            // Real user-controlled account name — must be masked (GAP CC).
            to_account_name: SENTINEL_ACCOUNT_NAME,
          },
        ],
      },
      loading: false,
      error: null,
    })
  })

  test('masks destination account name', async () => {
    render(
      <SavingsImpactWidget widgetId="pii-test-savings" onRemove={() => {}} />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'recent_item.to_account_name' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — currency surfaces (widgets + sub-components)', () => {
  beforeEach(() => {
    cleanup()
  })

  test('AccountBalanceCards masks account type balance and individual balances', async () => {
    const summary = {
      total_assets: 99999.99,
      total_liabilities: 0,
      net_worth: 99999.99,
      by_type: {
        depository: {
          balance: 99999.99,
          account_count: 1,
          accounts: [{ id: 'acc-1', name: 'Checking', balance: 99999.99 }],
        },
      },
    }
    render(<AccountBalanceCards summary={summary as any} />, { wrapper: createWrapper() })
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('CashFlowSummaryWidget masks income, expenses, and net cash flow', async () => {
    // Mock at the API layer (analytics-api.ts preload) so useCashFlowSummary picks it up.
    // The API hook returns queryData; useCashFlowSummary casts it to CashFlowSummary.
    // formatCurrency uses maximumFractionDigits: 0, so 12345.67 → '$12,346'
    mockUseGetCashFlowSummary.mockReturnValue({
      data: {
        income: 12345.67,
        expenses: 8888.88,
        net_cash_flow: 3456.79,
        income_count: 2,
        expense_count: 3,
        period_comparison: null,
      },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })
    // CashFlowSummaryWidget reads DashboardContext directly via useContext — wrap in provider
    const dashCtxValue = {
      filters: { startDate: '2024-01-01', endDate: '2024-01-31', accountTypes: undefined },
      setFilters: mock(() => {}),
      timeframe: 'this_month' as const,
      setTimeframe: mock(() => {}),
      isEditing: false,
      setIsEditing: mock(() => {}),
      hasAccounts: true,
      accountsLoading: false,
      scope: 'personal' as const,
      setScope: mock(() => {}),
    }
    function CashFlowWrapper({ children }: { children: ReactNode }) {
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
      return (
        <ThemeProvider>
          <QueryClientProvider client={qc}>
            <MemoryRouter>
              <DashboardContext.Provider value={dashCtxValue}>
                {children}
              </DashboardContext.Provider>
            </MemoryRouter>
          </QueryClientProvider>
        </ThemeProvider>
      )
    }
    render(<CashFlowSummaryWidget widgetId="pii-cf-test" onRemove={() => {}} />, { wrapper: CashFlowWrapper })
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$12,346'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('BudgetOverviewWidget masks total budget and remaining amounts', async () => {
    // Mock at the API layer (budgets-api.ts preload) so useCurrentBudget picks it up.
    // useCurrentBudget calls useGetCurrentBudget, casts data to Budget, and calls computeBudgetMetrics.
    // With empty categories, computeBudgetMetrics gives remaining_amount = budget.amount - 0 = amount.
    const mockBudgetData = {
      id: 'budget-1', user_id: 'user-1', name: 'Test Budget', description: '',
      currency_code: 'USD', period: 'monthly', start_date: '2024-01-01', end_date: '2024-01-31',
      amount: 99999.99, expected_income: null, is_active: true, income_amount: 0,
      income_sources: [], categories: [],
      created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
    }
    mockUseGetCurrentBudget.mockReturnValue({
      data: mockBudgetData,
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })
    render(<BudgetOverviewWidget widgetId="pii-budget-test" onRemove={() => {}} />, { wrapper: createWrapper() })
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('BudgetTransactionList masks transaction total', async () => {
    // Mock at API layer (budgets-api.ts preload): useBudgetTransactions calls useGetCategoryTransactions.
    // The hook's useEffect syncs queryData.total_amount → totalAmount state.
    mockUseGetCategoryTransactions.mockReturnValue({
      data: {
        transactions: [
          {
            id: 'txn-1', user_id: 'user-1', account_id: 'acc-1', linked_item_id: 'item-1',
            source: 'plaid', amount: 99999.99, date: '2024-01-15',
            name: 'Test Transaction', merchant_name: 'Test',
            tags: [], pending: false,
            created_at: '2024-01-15T00:00:00Z', updated_at: '2024-01-15T00:00:00Z',
          }
        ],
        total_count: 1,
        total_amount: 99999.99,
        has_more: false,
      },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    })
    render(<BudgetTransactionList budgetId="budget-1" categoryId="cat-1" />, { wrapper: createWrapper() })
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('SavingsGoalActionForm masks goal current/target amounts and goal/account names in <option> text', async () => {
    // SavingsGoalActionForm uses useGetSavingsGoals (savings-goals-api.ts preload)
    // and useActiveAccounts which calls useGetAccounts (accounts-api.ts preload).
    // Sentinel names are now injected so the gate catches unmasked <option> text (R9.5).
    mockUseGetSavingsGoals.mockReturnValue({
      data: [
        { id: 'goal-1', name: SENTINEL_GOAL_NAME, target_amount: 99999.99, current_amount: 12345.67 }
      ],
      isLoading: false,
    })
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [{ id: 'acc-1', name: SENTINEL_ACCOUNT_NAME, status: 'active', user_account_id: 'ua-1' }] },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    const action = {
      type: 'savings_goal' as const,
      savings_goal_id: 'goal-1',
      from_account_id: '',
      amount_type: 'fixed' as const,
      amount: 100,
    }
    render(
      <SavingsGoalActionForm action={action} onChange={mock(() => {})} />,
      { wrapper: createWrapper() }
    )
    // Wait for the selected goal's currency stats to render (goal pre-selected → shows amounts)
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_GOAL_NAME, fieldDescription: 'savings_goal.name in SavingsGoalActionForm <option>' },
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account.name in SavingsGoalActionForm <option>' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('IncomeSourceItem masks manual income source amount and free-text description', async () => {
    const source = { type: 'manual' as const, value: '99999.99', description: SENTINEL_DESC }
    render(
      <IncomeSourceItem source={source} onRemove={mock(() => {})} />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_DESC, fieldDescription: 'income source free-text description (PII_PROPERTY_DENYLIST member)' },
    ]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('IncomeSourceItem masks free-text description unconditionally (tag source type)', () => {
    // Verifies data-ph-mask is NOT guarded behind source.type==='manual';
    // description is PII_PROPERTY_DENYLIST for all source types.
    const source = { type: 'tag' as const, value: 'groceries', description: SENTINEL_DESC }
    render(
      <IncomeSourceItem source={source} onRemove={mock(() => {})} />,
      { wrapper: createWrapper() }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_DESC, fieldDescription: 'income source free-text description on tag source type' },
    ]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('IncomeSourceConfig masks expected total when showTotal=true', async () => {
    const sources = [{ type: 'manual' as const, value: '99999.99', description: 'Salary' }]
    render(
      <IncomeSourceConfig
        sources={sources}
        onChange={mock(() => {})}
        availableCategories={[]}
        availableTags={[]}
        showTotal={true}
      />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

describe('PII coverage — Round 4: AccountTypeFilter / BudgetIncomeSection / BudgetCategoryForm', () => {
  beforeEach(() => {
    cleanup()
  })

  test('AccountTypeFilter masks per-type account balance in open dropdown', async () => {
    const user = userEvent.setup()
    // Use a whole-dollar amount — AccountTypeFilter uses the 0dp formatCurrency from lib/utils
    const accountTypes = {
      types: [{ key: 'depository', label: 'Checking & Savings', count: 2, total_balance: 12345 }],
    }
    render(
      <AccountTypeFilter accountTypes={accountTypes} selectedTypes={[]} onChange={mock(() => {})} />,
      { wrapper: createWrapper() }
    )
    // Balance renders inside DropdownMenuContent portal — open the menu first
    await user.click(screen.getByRole('button', { name: /all account types/i }))
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$12,345'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('BudgetIncomeSection masks Difference and per-source expected/partial amounts', async () => {
    // expectedIncome/actualIncome headline spans ARE masked; the Difference span and
    // per-source expected/actual spans (lines 107-115, 202-209) are currently unmasked.
    const incomeSources = [{ type: 'manual' as const, value: '99999.99', description: 'Main Salary' }]
    render(
      <BudgetIncomeSection
        expectedIncome={99999.99}
        actualIncome={50000}
        incomeSources={incomeSources}
        currencyCode="USD"
        sourceStatuses={{ 'Main Salary': 'partial' }}
        sourceActuals={{ 'Main Salary': 50000 }}
      />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('$99,999'),
      { timeout: 3000 }
    )
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('BudgetCategoryForm masks over-allocation amount in warning Alert', async () => {
    // Edit mode: allocated_amount=175, totalAllocatedExcludingCurrent=0, expectedIncome=100
    // → overage = (0+175)-100 = 75 → Alert renders "…by $75.00."
    // JSX splits "$" and "75.00" into separate text nodes, so we use a sentinel for "75.00"
    // (the numeric text node) to verify it gains a data-ph-mask ancestor.
    // No budgetStartDate/End → previewEnabled=false → no preview API calls fired.
    mockUseGetUserCategories.mockReturnValue({ data: undefined, isLoading: false, error: null })
    const initialData = {
      id: 'cat-1',
      budget_id: 'budget-1',
      category_kind: 'pfc',
      pfc_primary: 'FOOD_AND_DRINK',
      allocated_amount: 175,
      spent_amount: 0,
      tags: [],
      match_logic: 'category_only',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    }
    render(
      <BudgetCategoryForm
        isOpen={true}
        onClose={mock(() => {})}
        onSubmit={mock(async () => {})}
        mode="edit"
        initialData={initialData as any}
        availableTags={[]}
        expectedIncome={100}
        totalAllocatedExcludingCurrent={0}
      />,
      { wrapper: createWrapper() }
    )
    // "75.00" is the allocationOverage.toFixed(2) text node; wait for it to appear
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain('75.00'),
      { timeout: 3000 }
    )
    const sentinels = [{ value: '75.00', fieldDescription: 'over-allocation currency amount in BudgetCategoryForm Alert' }]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})
describe('PII coverage — Round 8: billing components (personal charge surfaces)', () => {
  beforeEach(() => {
    cleanup()
  })

  test('CurrentSubscriptionDisplay masks subscription amount in text and aria-label', () => {
    render(
      <CurrentSubscriptionDisplay
        tier="premium"
        tierName="Premium"
        status="active"
        billingInterval="month"
        amount="8.99"
        currency="USD"
        features={['Unlimited accounts', 'Boost rules']}
      />,
      { wrapper: createWrapper() }
    )
    // Confirm the personal charge is actually in the DOM before scanning
    expect(document.body.textContent).toContain('8.99')
    // The subscription amount (user's actual charge) must sit inside a data-ph-mask element;
    // the aria-label containing the amount must not expose it unmasked (pii-masking.ts:80-94
    // scans attributes verbatim, not skipping masked ancestors for attributes).
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('PlanChangeModal masks proration_amount in upgrade preview section', () => {
    // Provide mock tiers where 'premium' (selected) costs more than 'standard' (current)
    // so isUpgrade=true and the proration section renders.
    const tiers = {
      standard: {
        display_name: 'Standard',
        pricing: { price_monthly: 4.99, price_yearly: 49.99, users_included: 1 },
        limits: {},
        features: [],
      },
      premium: {
        display_name: 'Premium',
        pricing: { price_monthly: 8.99, price_yearly: 89.99, users_included: 2 },
        limits: {},
        features: [],
      },
    }

    // Override the preloaded mock to return a success state with proration data so the
    // upgrade preview section renders synchronously on the first render.
    mockUsePreviewPaddleSubscriptionUpdate.mockReturnValueOnce({
      mutateAsync: mock(),
      isPending: false,
      isSuccess: true,
      isError: false,
      error: null,
      reset: mock(),
      data: { proration_amount: '3.50', currency: 'USD', next_charge_date: '2026-07-01' },
    })

    render(
      <PlanChangeModal
        open={true}
        currentTier="standard"
        currentBillingInterval="month"
        tiers={tiers as any}
        isUpdating={false}
        onChangePlan={mock(() => {})}
        onClose={mock(() => {})}
        initialTier="premium"
        initialInterval="month"
      />,
      { wrapper: createWrapper() }
    )
    // Confirm the proration amount is actually in the DOM before asserting.
    expect(document.body.textContent).toContain('3.50')
    // NOTE: PlanChangeModal also renders public catalog tier prices ($8.99 etc.) in its tier
    // selection grid — these are NOT PII. Scope the scan to just the charge <p> so catalog prices
    // in the tier grid are not false-positives (plan fallback: "focused render of just the
    // isUpgrade preview section"). Inside the charge paragraph, the ONLY currency-shaped text is
    // the proration amount "USD 3.50" — if unmasked, CURRENCY_RE catches it; if masked, passes.
    const chargePara = Array.from(document.body.querySelectorAll('p')).find(
      (p) => p.textContent?.includes("You'll be charged")
    ) as HTMLElement | undefined
    expect(chargePara).not.toBeUndefined()
    // scanForUnmaskedPII on just the charge paragraph covers both text nodes and attributes
    // (aria-label, title, etc.) within that element — stronger than a structural assertion.
    expect(scanForUnmaskedPII(chargePara!, [])).toEqual([])
  })
})

describe('PII coverage — SubscriptionOverrideModal (G7 Fix 7)', () => {
  // The modal was absent from this centralized sweep entirely, and the Remove Override
  // confirmation dialog's email had no masking assertion anywhere — both data-ph-mask
  // placements are already correct in source; this section is coverage, not a behavior fix.
  const adminSubDetail = {
    base_tier: 'premium',
    effective_tier: 'household_4',
    effective_status: 'active',
    subscription_status: 'active',
    manual_tier_override: 'household_4',
    manual_tier_ends_at: null,
    limits: { transaction_history_days: 730 },
    features: { api_access: true },
  }

  beforeEach(() => {
    cleanup()
    mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: adminSubDetail })
    mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mock(() => {}), isPending: false })
  })

  test('masks the target user email in the main modal (DialogDescription)', async () => {
    render(
      <SubscriptionOverrideModal userId="u1" userEmail={SENTINEL_EMAIL} open={true} onClose={() => {}} />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_EMAIL, fieldDescription: 'admin override modal target user email (DialogDescription)' },
    ]
    await waitForSentinels(sentinels)
    // Scoped to the DialogDescription element itself (id="sub-override-desc"): the modal's own
    // "New Tier" <select> statically enumerates every configured tier's display label —
    // including parenthetical seat counts like "Household (4)" — regardless of which tier is
    // selected, which the scanner's currency regex false-flags (a bracketed number reads like a
    // parenthesized negative amount). That's unrelated to email masking, so scan only the
    // element under test rather than the whole document.
    const description = document.getElementById('sub-override-desc')
    expect(description).not.toBeNull()
    expect(scanForUnmaskedPII(description as HTMLElement, sentinels)).toEqual([])
  })

  test('masks the target user email in the Remove Override confirmation dialog', async () => {
    // Use a tier with no parenthetical seat count (Premium, not a household_N tier) so the
    // confirmation copy itself ("This will remove the Premium override...") doesn't trip the
    // scanner's currency-regex false positive on "(4)" — same false-positive source as above,
    // orthogonal to what this test actually verifies (email masking).
    mockUseGetAdminUserSubscription.mockReturnValue({
      isLoading: false,
      data: { ...adminSubDetail, base_tier: 'premium', effective_tier: 'premium', manual_tier_override: 'premium' },
    })
    render(
      <SubscriptionOverrideModal userId="u1" userEmail={SENTINEL_EMAIL} open={true} onClose={() => {}} />,
      { wrapper: createWrapper() },
    )
    await waitForSentinels([
      { value: SENTINEL_EMAIL, fieldDescription: 'admin override modal target user email' },
    ])

    fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
    fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
    const alertDialog = await screen.findByRole('alertdialog')

    const sentinels: PIISentinel[] = [
      {
        value: SENTINEL_EMAIL,
        fieldDescription: 'admin override modal target user email (ConfirmationDialog description)',
      },
    ]
    // Scoped to the alertdialog itself — the main modal (still mounted behind it, per G7 Fix 8a)
    // carries the same statically-enumerated tier <select> noted above.
    expect(scanForUnmaskedPII(alertDialog, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 7: BalanceOverview / NetWorthSparkline', () => {
  beforeEach(() => {
    cleanup()
  })

  test('BalanceOverview masks total balance and change amount', () => {
    render(
      <BalanceOverview summary={{
        total_balance: 15000.50,
        as_of_date: '2025-11-15',
        period_comparison: { change_amount: 250.75, change_percent: 1.7 },
      }} />,
      { wrapper: createWrapper() }
    )
    // Confirm the currency values are actually in the DOM before scanning
    expect(document.body.textContent).toContain('$15,000.50')
    expect(document.body.textContent).toContain('$250.75')
    // total_balance ($15,000.50) and change_amount ($250.75) must be masked;
    // change_percent (1.70%) is a percentage, not currency — not flagged by the scanner.
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('NetWorthSparkline masks min/max net-worth currency labels', () => {
    render(
      <NetWorthSparkline data={[
        { date: '2025-09-01', net_worth: 10000, total_assets: 12000, total_liabilities: 2000 },
        { date: '2025-10-01', net_worth: 25000, total_assets: 27000, total_liabilities: 2000 },
      ]} />,
      { wrapper: createWrapper() }
    )
    // Confirm the currency values are actually in the DOM before scanning
    expect(document.body.textContent).toContain('$10,000')
    expect(document.body.textContent).toContain('$25,000')
    // min ($10,000) and max ($25,000) net-worth labels must sit inside a data-ph-mask element
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

describe('PII coverage — Round 9: account/institution/merchant names in text, attributes, <option> text', () => {
  beforeEach(() => {
    cleanup()
  })

  afterEach(() => {
    // Restore default happy-dom viewport width (1920) so later tests aren't affected
    // by the innerWidth=1024 set in the ReviewInboxPage test.
    Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1920 })
  })

  test('AccountCustomizationModal masks displayName, institution_name; de-PIIs placeholder attribute', async () => {
    // R9.1: displayName (= account.nickname) renders unmasked at line 132.
    // R9.2: account.institution_name renders unmasked at line 141.
    // R9.3: account.name leaks into the placeholder attribute at line 157.
    //        Attribute PII cannot be masked with data-ph-mask ancestors — must be de-PII'd (generic string).
    const account = makeAccount({
      nickname: SENTINEL_ACCOUNT_NAME,      // → displayName (R9.1)
      name: SENTINEL_ACCOUNT_NAME,          // → placeholder when official_name absent (R9.3)
      // institution_name: SENTINEL_INSTITUTION is already the default in makeAccount() (R9.2)
      // official_name is absent → placeholder falls back to account.name
    })
    render(
      <AccountCustomizationModal
        account={account as any}
        open={true}
        onOpenChange={mock(() => {})}
        onSave={mock(async () => {})}
        isSaving={false}
      />,
      { wrapper: createWrapper() }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account displayName/name in AccountCustomizationModal text + placeholder' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'account.institution_name in AccountCustomizationModal text' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('ReviewInboxPage select checkbox aria-label does not expose merchant_name', async () => {
    // R9.4: aria-label={`Select ${tx.merchant_name || tx.name}`} at line 303 exposes the merchant name.
    //        Attribute PII cannot be masked — must be de-PII'd to a generic "Select transaction".
    //        The merchant cell (line 316) IS masked via maskedColumnIds — only the aria-label leaks.

    // Simulate desktop viewport so the DataTable renders (not TriageDetailView on mobile).
    Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: 1024 })

    // Set up review mutation mocks (required by ReviewInboxPage even for read-only render).
    mockUseSetTransactionReviewed.mockReturnValue({
      mutateAsync: mockSetTransactionReviewedMutateAsync,
      isPending: false,
    })
    mockUseBulkSetTransactionReviewed.mockReturnValue({
      mutateAsync: mockBulkSetTransactionReviewedMutateAsync,
      isPending: false,
    })
    mockUseBulkUpdateTransactionCategory.mockReturnValue({
      mutateAsync: mockBulkUpdateTransactionCategoryMutateAsync,
      isPending: false,
    })
    mockUseGetTransactionReviewSummary.mockReturnValue({
      data: { unreviewed_count: 1, reviewed_by_rule_count: 0, reviewed_by_manual_count: 0, reviewed_by_plaid_count: 0 },
      isLoading: false,
      isError: false,
    })

    // Provide a transaction with the sentinel merchant name via the raw fetcher
    // (useInfiniteTransactions calls getTransactions from the mocked api-client).
    mockGetTransactions.mockResolvedValue({
      transactions: [makeTransaction()],
      next_cursor: undefined,
      has_more: false,
      limit: 30,
    })

    render(<ReviewInboxPage />, { wrapper: createWrapper() })

    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'merchant_name in ReviewInboxPage select checkbox aria-label' },
    ]
    // The merchant cell is masked via maskedColumnIds — but the aria-label is not.
    // Wait for the transaction row to render (text appears in the masked cell).
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('TransferActionForm masks account name and institution_name in <option> text', async () => {
    // R9.5: <option> text renders account.name + institution_name unmasked.
    //        Fix: add data-ph-mask to each <option> element.
    mockUseGetAccounts.mockReturnValue({
      data: {
        accounts: [makeAccount({
          name: SENTINEL_ACCOUNT_NAME,
          institution_name: SENTINEL_INSTITUTION,
        })],
      },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    const action = {
      type: 'transfer' as const,
      from_account_id: '',
      to_account_id: '',
      amount_type: 'fixed' as const,
      amount: 100,
    }
    render(
      <TransferActionForm action={action as any} onChange={mock(() => {})} />,
      { wrapper: createWrapper() }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account.name in TransferActionForm <option> text' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'account.institution_name in TransferActionForm <option> text' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('CreateSavingsGoalDialog masks account name, mask, and institution_name in <option> text', async () => {
    // R9.5: <option> text renders account.name + "••••{mask}" + institution_name unmasked.
    //        "••••1234" is ACCOUNT_MASK_RE-catchable even without a named sentinel.
    //        Fix: add data-ph-mask to each <option> element.
    mockUseGetAccounts.mockReturnValue({
      data: {
        accounts: [makeAccount({
          name: SENTINEL_ACCOUNT_NAME,
          institution_name: SENTINEL_INSTITUTION,
          mask: '1234',     // → "••••1234" in option text (ACCOUNT_MASK_RE)
          type: 'depository', // CreateSavingsGoalDialog filters by type=depository
        })],
      },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    render(
      <CreateSavingsGoalDialog
        isOpen={true}
        onCancel={mock(() => {})}
        onGoalCreated={mock(() => {})}
      />,
      { wrapper: createWrapper() }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account.name in CreateSavingsGoalDialog <option> text' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'account.institution_name in CreateSavingsGoalDialog <option> text' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 10: AutoCategorizationRuleTable name + description', () => {
  beforeEach(() => {
    cleanup()
  })

  test('AutoCategorizationRuleTable masks rule name and description (match card)', async () => {
    // maskedColumnIds={['name']} makes DataTable apply data-ph-mask to the entire name cell,
    // covering both rule name and description — matching AutoCategorizationRuleCard masking.
    const rule = {
      id: 'acat-table-sentinel',
      user_id: 'user-1',
      name: SENTINEL_RULE_NAME,
      description: SENTINEL_RULE_DESC,
      enabled: true,
      priority: 1,
      conditions: [],
      condition_logic: 'and' as const,
      target_kind: 'pfc' as const,
      target_pfc_primary: 'INCOME',
      tag_behavior: 'add' as const,
      source: 'manual' as const,
      match_count: 0,
      override_count: 0,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    }
    render(
      <AutoCategorizationRuleTable rules={[rule as any]} />,
      { wrapper: createWrapper() }
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_RULE_NAME, fieldDescription: 'auto_cat_rule.name in table view' },
      { value: SENTINEL_RULE_DESC, fieldDescription: 'auto_cat_rule.description in table view (denylist member)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 11: ChangeIndicator aria-label', () => {
  beforeEach(() => {
    cleanup()
  })

  test('ChangeIndicator does not leak net-worth change amount in aria-label', () => {
    render(
      <ChangeIndicator variant="positive" amount={123456} percent={5.2} />,
      { wrapper: createWrapper() }
    )
    // Visible spans are already data-ph-mask'd.
    // The scanner reads aria-label text verbatim — no currency must appear there.
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

describe('PII coverage — Portfolio: PortfolioSummaryCard', () => {
  beforeEach(() => {
    cleanup()
  })

  test('masks total_value, total_gain_loss, and day_change', async () => {
    const summary = {
      total_value: 99999.99,
      total_cost_basis: 80000.0,
      total_gain_loss: 19999.99,
      total_gain_loss_pct: 24.99,
      day_change: 1234.56,
      day_change_pct: 1.23,
      holdings_count: 5,
    }
    render(<PortfolioSummaryCard summary={summary as any} />, { wrapper: createWrapper() })
    await waitFor(() => expect(document.body.textContent ?? '').toContain('Total Value'))
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('null day_change renders "--" without leaking currency', async () => {
    const summary = {
      total_value: 99999.99,
      total_cost_basis: 80000.0,
      total_gain_loss: 19999.99,
      total_gain_loss_pct: 24.99,
      day_change: null,
      day_change_pct: null,
      holdings_count: 5,
    }
    render(<PortfolioSummaryCard summary={summary as any} />, { wrapper: createWrapper() })
    await waitFor(() => expect(document.body.textContent ?? '').toContain('Total Value'))
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

describe('PII coverage — Round 12: SearchableSelect maskOptions', () => {
  beforeEach(() => {
    cleanup()
  })

  test('SearchableSelect masks option label and sublabel when maskOptions is set', async () => {
    // The option label carries the user's account name (∈ PII_PROPERTY_DENYLIST).
    // The sublabel carries the account mask (••••9999, catchable by ACCOUNT_MASK_RE).
    // Without maskOptions these render unmasked in the open <li>; with it they are
    // wrapped in <span data-ph-mask>.
    const user = userEvent.setup()
    render(
      <SearchableSelect
        options={[{ value: 'acc-r12', label: SENTINEL_ACCOUNT_NAME, sublabel: 'checking ••••9999' }]}
        value={null}
        onChange={mock(() => {})}
        aria-label="Select account"
        maskOptions
      />,
      { wrapper: createWrapper() }
    )
    // Open the dropdown so the <ul role="listbox"> renders.
    await user.click(screen.getByRole('combobox'))
    await screen.findByRole('listbox')
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account name in SearchableSelect option label' },
    ]
    // SENTINEL_ACCOUNT_NAME must be masked; ••••9999 is auto-flagged by ACCOUNT_MASK_RE.
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 13 extended: HoldingsTable monetary surfaces', () => {
  beforeEach(() => {
    cleanup()
  })

  const holdingFixture = {
    id: 'h-pii-1',
    account_id: 'acct-1',
    user_id: 'user-1',
    security_id: 'sec-1',
    quantity: 10,
    security_name: 'Apple Inc',
    ticker_symbol: 'AAPL',
    security_type: 'equity',
    institution_value: 99999.99,
    institution_price: 9999.99,
    cost_basis: 88888.88,
    gain_loss: 11111.11,
    gain_loss_pct: 12.5,
    day_change: 222.22,
    day_change_pct: 0.22,
  }

  test('monetary holding columns are inside data-ph-mask td cells', async () => {
    mockUseGetHoldings.mockReturnValue({
      data: { count: 1, holdings: [holdingFixture] },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />, { wrapper: createWrapper() })
    await waitFor(() => expect(document.body.textContent ?? '').toContain('AAPL'))
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })

  test('account name in Account cell and filter dropdown option is masked', async () => {
    // HoldingsTable must mask account names — convention (all 3 other name-bearing
    // DataTables do: AccountBreakdownTable, TransactionTable, AccountTable).
    // Seed accounts so accountNameById resolves 'acct-1' → SENTINEL_ACCOUNT_NAME.
    const user = userEvent.setup()
    mockUseGetAccounts.mockReturnValue({
      data: {
        accounts: [{ id: 'acct-1', name: SENTINEL_ACCOUNT_NAME, status: 'active', user_account_id: 'ua-1' }],
      },
      isLoading: false,
      error: null,
      refetch: mock(() => {}),
    })
    mockUseGetHoldings.mockReturnValue({
      data: { count: 1, holdings: [holdingFixture] },
      isLoading: false,
      isError: false,
    })
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_ACCOUNT_NAME, fieldDescription: 'account name in HoldingsTable Account cell' },
    ]
    render(<HoldingsTable />, { wrapper: createWrapper() })
    await waitForSentinels(sentinels)

    // Open account filter so Radix portal mounts option labels into document.body.
    // Without this the scanner never sees portaled filter labels.
    await user.click(screen.getByRole('button', { name: /filter by account/i }))
    const listbox = await waitFor(
      () => screen.getByRole('listbox'),
      { timeout: 3000 },
    )
    // Confirm sentinel is present in the portal before asserting it is masked.
    expect(within(listbox).getByText(SENTINEL_ACCOUNT_NAME)).toBeInTheDocument()

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
    // Dismiss dropdown so Radix portal unmounts cleanly before bun worker teardown.
    await user.keyboard('{Escape}')
  })
})

describe('PII coverage — Round 13 extended: InvestmentTransactionsTable', () => {
  beforeEach(() => {
    cleanup()
    mockFetchNextInvestmentPage.mockClear()
    mockUseInfiniteInvestmentTransactions.mockClear()
  })

  test('financial cells (quantity, price, amount, fees) are masked and no sentinel leaks unmasked', async () => {
    // CRITICAL: seed the WRAPPER hook mock (not the raw API fetcher).
    // InvestmentTransactionsTable imports useInfiniteInvestmentTransactions which is
    // globally preloaded-mocked (investments-hooks.ts). Default returns transactions: [].
    // The raw API fetcher (mockGetInvestmentTransactions) is never called by the component.
    mockUseInfiniteInvestmentTransactions.mockReturnValue({
      transactions: [
        {
          id: 'pii-txn-1',
          user_id: 'user-1',
          account_id: 'acct-1',
          plaid_investment_transaction_id: 'plaid-1',
          date: '2024-06-01',
          name: 'AAPL Buy',
          // Sentinel values — must NOT appear as raw text in the DOM
          amount: 99999.99,      // → '$99,999.99'
          quantity: 123.4567,    // → '123.4567' (not currency-formatted)
          price: 810.45,         // → '$810.45'
          fees: 4.99,            // → '$4.99'
          type: 'buy',
          created_at: '2024-06-01T10:00:00Z',
        },
      ],
      fetchNextPage: mockFetchNextInvestmentPage,
      hasNextPage: false,
      isFetchingNextPage: false,
      isLoading: false,
      isError: false,
      isSuccess: true,
    })

    // disableVirtualization=true forces DataTable to render real rows (no jsdom layout needed).
    // This is the documented test seam for the pii-coverage suite; production always uses
    // virtualization (disableVirtualization defaults to false).
    render(
      <InvestmentTransactionsTable
        dateRange={{ start_date: '2024-01-01', end_date: '2024-12-31' }}
        disableVirtualization
      />,
      { wrapper: createWrapper() },
    )

    // Wait for the seeded row to actually appear in the DOM (suite rule: per-sentinel waitFor).
    await waitFor(() => screen.getByText('AAPL Buy'))

    // Sentinels for PII scan. quantity renders as toFixed(4) = '123.4567' (not $-formatted),
    // so it needs an explicit sentinel. price/amount/fees are $-formatted and auto-caught
    // by CURRENCY_RE. Test fails if any sentinel appears unmasked in the DOM.
    const sentinels: PIISentinel[] = [
      { value: '123.4567', fieldDescription: 'quantity (raw toFixed(4))' },
    ]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 13 extended: PortfolioValueChart', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
  })

  // PortfolioValueChart is pure-canvas: currency values render in ECharts canvas, not DOM text.
  // Guard: verify EChartsChart wrapper has data-ph-mask (EChartsChart.tsx:31).
  test('chart canvas is wrapped in data-ph-mask element (light mode)', async () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 99999.99 },
          { date: '2024-06-01', total_value: 109999.99 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(
      <PortfolioValueChart dateRange={{ start_date: '2024-01-01', end_date: '2024-06-01' }} />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })
})

describe('PII coverage — Round 13 extended: PortfolioValueChart (dark mode)', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(true)
  })

  // Verify data-ph-mask persists in dark mode (isDark=true should not strip it).
  test('chart canvas is wrapped in data-ph-mask element (dark mode)', async () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 99999.99 },
          { date: '2024-06-01', total_value: 109999.99 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(
      <PortfolioValueChart dateRange={{ start_date: '2024-01-01', end_date: '2024-06-01' }} />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })
})

describe('PII coverage — Round 13 extended: AllocationCard', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 99999.99,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 79999.99, percentage: 80, holding_count: 5 },
          { security_type: 'Fixed Income', asset_class: 'bond', total_value: 19999.99, percentage: 20, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
  })

  // AllocationCard is pure-canvas via ECharts. Guard: verify data-ph-mask wraps the canvas.
  test('donut chart canvas is wrapped in data-ph-mask element (light mode)', async () => {
    render(
      <AllocationCard
        config={{ include: [], group_by: 'security_type', basis: 'assets' }}
        onConfigChange={() => {}}
      />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })
})

describe('PII coverage — Round 13 extended: AllocationCard (dark mode)', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(true)
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 99999.99,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 79999.99, percentage: 80, holding_count: 5 },
          { security_type: 'Fixed Income', asset_class: 'bond', total_value: 19999.99, percentage: 20, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
  })

  // Verify data-ph-mask persists in dark mode (isDark=true should not strip it).
  test('donut chart canvas is wrapped in data-ph-mask element (dark mode)', async () => {
    render(
      <AllocationCard
        config={{ include: [], group_by: 'security_type', basis: 'assets' }}
        onConfigChange={() => {}}
      />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })
})

// ── Round 14: Investment dashboard widgets (#1297) ──────────────────────────

describe('PII coverage — Round 14: PortfolioSummaryWidget', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
    mockUseGetPortfolioSummary.mockReturnValue({
      data: {
        total_value: 99999.99,
        total_gain_loss: 9999.99,
        total_gain_loss_pct: 10.01,
        day_change: 999.99,
        day_change_pct: 1.01,
        holdings_count: 3,
      },
      isLoading: false,
      isError: false,
    })
  })

  // PortfolioSummaryWidget wraps PortfolioSummaryCard (already data-ph-masked in Round 13).
  // This test ensures the widget layer does not strip those masks.
  test('monetary values in PortfolioSummaryCard are masked (no unmasked PII)', async () => {
    render(
      <PortfolioSummaryWidget widgetId="w-psw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    // "View portfolio" link appears only in the success state
    await waitFor(
      () => expect(document.body.textContent).toContain('View portfolio'),
      { timeout: 3000 }
    )
    const violations = scanForUnmaskedPII(document.body, [])
    expect(violations).toEqual([])
  })
})

describe('PII coverage — Round 14: PortfolioHoldingsWidget (hand-built table)', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
    mockUseGetHoldings.mockReturnValue({
      data: {
        holdings: [
          {
            id: 'hold-1',
            user_id: 'user-1',
            account_id: 'acc-1',
            security_id: 'sec-1',
            ticker_symbol: null,
            security_name: SENTINEL_SECURITY_NAME,
            quantity: 10,
            institution_value: 99999.99,
            day_change_pct: 1.23,
          },
        ],
      },
      isLoading: false,
      isError: false,
    })
  })

  // The compact holdings table is hand-built (not DataTable-wrapped), so each cell
  // requires its own data-ph-mask. This test scans the full body with the security-name
  // sentinel to ensure nothing leaks outside a masked ancestor.
  test('scanForUnmaskedPII returns [] — security name and currency are all masked', async () => {
    render(
      <PortfolioHoldingsWidget widgetId="w-phw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.textContent ?? '').toContain(SENTINEL_SECURITY_NAME),
      { timeout: 3000 }
    )
    const violations = scanForUnmaskedPII(document.body, [
      { value: SENTINEL_SECURITY_NAME, fieldDescription: 'security name' },
    ])
    expect(violations).toEqual([])
  })

  test('holding-value cell has data-ph-mask attribute', async () => {
    render(
      <PortfolioHoldingsWidget widgetId="w-phw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.querySelector('[data-testid="holding-value"]')).not.toBeNull(),
      { timeout: 3000 }
    )
    expect(document.body.querySelector('[data-testid="holding-value"][data-ph-mask]')).not.toBeNull()
  })

  test('holding-name cell has data-ph-mask attribute', async () => {
    render(
      <PortfolioHoldingsWidget widgetId="w-phw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    await waitFor(
      () => expect(document.body.querySelector('[data-testid="holding-name"]')).not.toBeNull(),
      { timeout: 3000 }
    )
    expect(document.body.querySelector('[data-testid="holding-name"][data-ph-mask]')).not.toBeNull()
  })
})

describe('PII coverage — Round 14: AssetAllocationWidget (ECharts donut)', () => {
  beforeEach(() => {
    cleanup()
    mockUseDarkMode.mockReturnValue(false)
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 99999.99,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 79999.99, percentage: 80, holding_count: 5 },
          { security_type: 'Fixed Income', asset_class: 'bond', total_value: 19999.99, percentage: 20, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
  })

  test('donut chart canvas is wrapped in data-ph-mask element (light mode)', async () => {
    render(
      <AssetAllocationWidget widgetId="w-aaw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })

  test('donut chart canvas is wrapped in data-ph-mask element (dark mode)', async () => {
    mockUseDarkMode.mockReturnValue(true)
    render(
      <AssetAllocationWidget widgetId="w-aaw" onRemove={() => {}} />,
      { wrapper: createWrapper() }
    )
    await waitFor(() =>
      expect(document.body.querySelector('[data-testid="echarts-chart"]')).not.toBeNull()
    )
    expect(document.body.querySelector('[data-testid="echarts-chart"][data-ph-mask]')).not.toBeNull()
  })
})

// ── Round 15: admin operator surfaces that render free text (#1497 / Z1 item 1) ──────────────
//
// The admin pages were outside this gate entirely, and one of them carried a documented
// mitigation that did not exist: migration 000137 states TWICE that the orphaned-Item operator
// resolution note is masked in session replay, and names it as the ONE column where an erased
// user's PII can re-enter by accident. It was not masked. Two sibling admin surfaces were
// leaking as well — the user detail modal's email and the Paddle webhook payload.
//
// Admins consent to replay; the DATA SUBJECTS in these payloads did not, and one of them has
// already been erased.

describe('PII coverage — Round 15: PlaidOrphanedItemsPage operator resolution note', () => {
  const ORPHAN_ATTESTER = 'user_2attestingStaffMember'

  beforeEach(() => {
    cleanup()
    mockUseListAdminPlaidOrphanedItems.mockReturnValue({
      data: {
        items: [
          {
            id: 'orphan-pii',
            plaid_item_id: 'item-pii-AAA',
            admin_state: 'operator_attested',
            institution_id: 'ins_900',
            institution_name: SENTINEL_INSTITUTION,
            erasure_error_code: 'ITEM_LOGIN_REQUIRED',
            retry_attempt_count: 3,
            orphaned_at: '2026-06-15T10:00:00Z',
            token_expires_at: '2026-07-15T10:00:00Z',
            token_retained: false,
            operator_resolved_at: '2026-07-20T09:00:00Z',
            operator_resolved_by: ORPHAN_ATTESTER,
            // The exact accident migration 000137 warns about: staff free text naming the
            // erased person and their bank.
            operator_resolution_note: `Removed ${SENTINEL_NICKNAME}'s ${SENTINEL_INSTITUTION} Item by hand.`,
          },
        ],
        total: 1,
        manual_intervention_required: 0,
        auto_retrying: 0,
        resolved: 0,
        operator_attested: 1,
        limit: 50,
        offset: 0,
        as_of: '2026-08-07T12:00:00Z',
      },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    } as never)
    mockUseRecordAdminPlaidOrphanedItemOperatorResolution.mockReturnValue({
      mutate: () => {},
      isPending: false,
    } as never)
  })

  test('scanForUnmaskedPII returns [] — the staff-written note is masked', async () => {
    render(<PlaidOrphanedItemsPage />, { wrapper: createWrapper() })
    const sentinels = [
      { value: SENTINEL_NICKNAME, fieldDescription: 'erased subject name inside operator_resolution_note' },
      { value: SENTINEL_INSTITUTION, fieldDescription: 'institution inside operator_resolution_note' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // Sibling, failing in the opposite direction. The attester is a STAFF Clerk id, not the erased
  // subject's data, and it must stay legible: masking the whole column would satisfy the test
  // above while turning the attestation anonymous — the exact outcome #1497 exists to prevent.
  test('the attesting staff id is NOT masked', async () => {
    render(<PlaidOrphanedItemsPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText(ORPHAN_ATTESTER)).toBeInTheDocument(), {
      timeout: 3000,
    })
    expect(screen.getByText(ORPHAN_ATTESTER).closest('[data-ph-mask]')).toBeNull()
  })
})

describe('PII coverage — Round 15: UserDetailModal target email', () => {
  beforeEach(() => {
    cleanup()
  })

  test('scanForUnmaskedPII returns [] — the admin-viewed user email is masked', async () => {
    render(
      <UserDetailModal
        user={{
          id: 'user-pii',
          email: SENTINEL_EMAIL,
          subscription_tier: 'premium',
          subscription_status: 'active',
          primary_region: 'iad',
          created_at: '2024-01-15T10:00:00Z',
          last_login_at: '2024-03-15T14:30:00Z',
          is_active: true,
          linked_accounts_count: 5,
          boosts_count: 10,
          transactions_count: 1500,
          budgets_count: 4,
        }}
        loading={false}
        open={true}
        onClose={() => {}}
      />,
      { wrapper: createWrapper() }
    )
    const sentinels = [{ value: SENTINEL_EMAIL, fieldDescription: 'admin user detail modal email' }]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // Sibling: the opaque user id is deliberately NOT masked — it is the identifier an operator
  // needs to correlate the record, and it carries no personal data.
  test('the user id is NOT masked', async () => {
    render(
      <UserDetailModal
        user={{
          id: 'user-pii',
          email: SENTINEL_EMAIL,
          subscription_tier: 'premium',
          subscription_status: 'active',
          primary_region: 'iad',
          created_at: '2024-01-15T10:00:00Z',
          last_login_at: '2024-03-15T14:30:00Z',
          is_active: true,
          linked_accounts_count: 5,
          boosts_count: 10,
          transactions_count: 1500,
          budgets_count: 4,
        }}
        loading={false}
        open={true}
        onClose={() => {}}
      />,
      { wrapper: createWrapper() }
    )
    await waitFor(() => expect(screen.getByText(/User ID: user-pii/)).toBeInTheDocument(), {
      timeout: 3000,
    })
    expect(screen.getByText(/User ID: user-pii/).closest('[data-ph-mask]')).toBeNull()
  })
})

describe('PII coverage — Round 15: WebhookDetailModal Paddle payload', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({
      data: {
        id: 'wh-pii',
        event_id: 'evt_pii',
        event_type: 'subscription.updated',
        status: 'processed',
        created_at: '2026-08-01T10:00:00Z',
        processed_at: '2026-08-01T10:00:01Z',
        // A real Paddle payload carries the customer's email and the billed amount.
        payload: { data: { customer: { email: SENTINEL_EMAIL }, totals: { total: '99999.99' } } },
      },
      isLoading: false,
      error: null,
    } as never)
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
      isSuccess: false,
      isError: false,
    } as never)
  })

  test('scanForUnmaskedPII returns [] — the raw payload is masked', async () => {
    render(<WebhookDetailModal webhookId="wh-pii" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })
    const sentinels = [
      { value: SENTINEL_EMAIL, fieldDescription: 'customer email inside the Paddle webhook payload' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // Sibling: the event type is operational metadata and stays legible, so a blanket mask over
  // the whole modal would fail here rather than pass the test above by brute force.
  test('the event type is NOT masked', async () => {
    render(<WebhookDetailModal webhookId="wh-pii" onClose={() => {}} />, {
      wrapper: createWrapper(),
    })
    await waitFor(() => expect(screen.getByText('subscription.updated')).toBeInTheDocument(), {
      timeout: 3000,
    })
    expect(screen.getByText('subscription.updated').closest('[data-ph-mask]')).toBeNull()
  })
})
