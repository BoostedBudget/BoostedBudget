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
import type { CategoryHierarchyNode, CategoryHierarchyResponse } from '@boostedbudget/api-client/models'

import { scanForUnmaskedPII, sentinelsFromFixture, type PIISentinel } from './pii-masking'
import { ThemeProvider } from '@/shared/theme/ThemeProvider'

// --- Centralized mocks (preloaded via bunfig.toml) ---
import { mockUseGetAccounts, mockUseGetAccountBalanceHistory } from '@/test/mocks/accounts-api'
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
import {
  mockUseGetDeletedSavingsGoals,
  mockUsePurgeSavingsGoal,
  mockPurgeSavingsGoalMutateAsync,
  mockUseListDeletedSavingsGoalContributions,
} from '@/test/mocks/savings-goals-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import {
  mockUseGetSpendingByMerchant,
  mockUseGetSpendingByCategory,
  mockUseGetCashFlowSummary,
  mockUseGetPortfolioAllocationBreakdown,
} from '@/test/mocks/analytics-api'
import {
  mockUseGetBoosts,
  mockUseGetUserBoostExecutions,
  mockRefetchUserBoostExecutions,
} from '@/test/mocks/boosts-api'
import {
  mockUseGetUserRules,
  mockUseGetUserRuleExecutions,
  mockRefetchUserRuleExecutions,
  mockPreviewRuleMutateAsync,
  mockUsePreviewRule,
} from '@/test/mocks/auto-categorization-api'
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
  mockUseGetAccountHoldingsHistory,
} from '@/test/mocks/investments-api'
import { mockUseSavingsImpactData } from '@/test/mocks/savings-impact-hooks'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'
import { mockUsePreviewPaddleSubscriptionUpdate } from '@/test/mocks/subscription-api'
import {
  mockUseRequestDeletion,
  mockRequestDeletionMutateAsync,
  mockCancelDeletion,
} from '@/test/mocks/user-api'
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
// Round 16 (batch EC): the five admin surfaces that were not imported by this gate at all.
import {
  mockUseListAdminDeadLetterJobs,
  mockUseGetAdminDeadLetterJobStats,
  mockUseGetAdminDeadLetterJobDetail,
  mockUseGetAdminDeadLetterJobArgs,
  mockUseRetryAdminDeadLetterJob,
  mockUseAddAdminDeadLetterJobNotes,
  mockUseDeleteAdminDeadLetterJob,
  mockUseBulkDeleteAdminDeadLetterJobs,
  mockUseListAdminUsers,
  mockUseListAdminPaddleWebhooks,
  mockUseGetAdminPaddleWebhookStats,
  mockUseGetSystemHealth,
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
const { DeletedGoalsPanel } = await import(
  '@/features/savings/components/DeletedGoalsPanel'
)
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

// --- Round 16 (batch EC): the five admin components the gate never imported ---
const { DeadLetterPage } = await import('@/features/admin/components/DeadLetterPage')
const { DeadLetterDetailModal } = await import('@/features/admin/components/DeadLetterDetailModal')
const { UsersPage } = await import('@/features/admin/components/UsersPage')
const { WebhooksPage } = await import('@/features/admin/components/WebhooksPage')
const { HealthDashboardPage } = await import('@/features/admin/components/HealthDashboardPage')

// --- Round 17 (#1514): AutoCategorizationRuleEditor target-tags + global chrome (name/email) ---
const { AutoCategorizationRuleEditor } = await import(
  '@/features/auto-categorization/components/AutoCategorizationRuleEditor'
)
// --- #1625: the rule dry-run panel. Every match row it renders is a raw transaction — merchant,
// description, amount, current category — so it belongs in the central sweep, not only in its own
// unit test. Membership here is not derivable; a new surface joins only because someone adds it.
const { RulePreviewPanel } = await import(
  '@/features/auto-categorization/components/RulePreviewPanel'
)
const { MobileNav } = await import('@/components/layout/MobileNav')
const { UserDropdown } = await import('@/components/layout/UserDropdown')

// --- Round 18 (#1514 fix round 2): six sites an independent review found after the
// scanner was widened to see data-* attributes (see pii-masking.ts's SAFE_ATTRS comment).
const { TopMerchantsTable } = await import('@/features/reports/components/TopMerchantsTable')
const { IncomeSourcePicker } = await import('@/features/budgets/components/income/IncomeSourcePicker')

// --- Round 19 (#1514 fix round 3): budget/category-name <option> text found in the same
// sweep. Only ConditionBuilder is added here — it takes `budgets` as a direct prop, so it
// needs no hook mock. BudgetAlertActionForm and RolloverActionForm call useBudgets()
// internally instead, and budgets-wrapper.ts's own comment is explicit that this hook is
// deliberately NOT globally mocked (mock.module is process-global; wiring it here would
// intercept BudgetsPage's identical import and silently empty its budget list — exactly
// the interference that comment warns about, confirmed by running this file with that
// wiring added: it broke "PII coverage — Budgets list page"). Those two forms keep their
// scanner-based coverage in their own test files instead (see AutoCategorizationRuleEditor
// for the same reasoning already applied to SearchableSelect elsewhere in this file).
const { ConditionBuilder } = await import('@/features/boosts/components/ConditionBuilder')

// --- Round 20 (#1514 fix round 4): custom-category-name <option>/<optgroup> leaks. ---
const { CustomCategoryForm } = await import('@/features/categories/components/CustomCategoryForm')

// --- Round 21 (#1514 fix round 5): account-deletion cancellation token — a bearer
// CAPABILITY/SECRET, not PII. Included here as the centralized regression gate for
// attribute/text exposure even though the risk class differs from the rest of this file.
const { DeleteAccountSection } = await import('@/features/deletion/components/DeleteAccountSection')

// --- #1569 T8.6: the three retained-history surfaces this gate could not see. ---
// ClosedAccountHoldingsHistory is NOT a standalone module: it is the branch HoldingsTable renders
// for a closed account, so it is reached by rendering HoldingsTable in history mode (see the block
// at the bottom of this file), not by importing it.
const { ClosedAccountBalanceHistory } = await import(
  '@/features/accounts/components/ClosedAccountBalanceHistory'
)
const { BoostAutomationHistoryPanel } = await import(
  '@/features/boosts/components/BoostAutomationHistoryPanel'
)
// --- #1569 T9.4: the RULE twin of the surface above. Added here by hand, per the note further
// down this file: membership is not derivable, so a new retained-history surface joins the sweep
// only because someone adds it.
const { RuleAutomationHistoryPanel } = await import(
  '@/features/auto-categorization/components/RuleAutomationHistoryPanel'
)

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
const SENTINEL_USER_NAME = 'Sentinel Username Person'
const SENTINEL_BOOST_NAME = 'SENTINEL_BOOST_WXYZ'
const SENTINEL_BOOST_ERROR = 'SENTINEL_BOOST_ERROR_HIJK'

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

    // Scan while still in the default cards view (AccountCard), BEFORE switching to table.
    // Issue #1514 (G5): the table-view scan below never exercised AccountCard at all, so it
    // never caught the account nickname/institution name leaking through AccountCard's
    // aria-label — an attribute, which data-ph-mask cannot cover (PostHog records attributes
    // verbatim). This assertion closes that blind spot structurally.
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])

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
    // #1626: the chart now consumes the category-hierarchy shape, one response per direction.
    // The custom category below is the case that matters here — its name is user-authored text,
    // so it must reach the DOM as text and never as an attribute value.
    const hierarchy = (levels: CategoryHierarchyNode[], direction: string): CategoryHierarchyResponse => ({
      levels,
      total: 3000,
      direction,
      start_date: '2024-01-01',
      end_date: '2024-01-31',
    })
    const incomeData = hierarchy(
      [{ category: 'INCOME_WAGES', display: 'Wages', total: 5000, direct_total: 5000, percentage: 100, transaction_count: 2, children: [] }],
      'income',
    )
    const expenseData = hierarchy(
      [
        {
          category: 'FOOD_AND_DRINK', display: 'Food & Drink', total: 1800, direct_total: 1000, percentage: 60, transaction_count: 20,
          children: [{ category: 'custom:abc', display: 'Therapy sessions', total: 800, direct_total: 800, percentage: 26, transaction_count: 4, children: [] }],
        },
        { category: 'TRANSPORTATION', display: 'Transportation', total: 1200, direct_total: 1200, percentage: 40, transaction_count: 8, children: [] },
      ],
      'expense',
    )

    render(
      <CashFlowSankeyChart
        incomeData={incomeData}
        expenseData={expenseData}
        loading={false}
        privacyMode={false}
        viewMode={{ kind: 'expanded' }}
        onFocus={() => {}}
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

    // #1626: a custom category's name is user-authored and can be sensitive. data-ph-mask covers
    // TEXT, not attribute values — session replay records those verbatim — so the name must never
    // be interpolated into aria-label, title, or any other attribute.
    //
    // ⚠ THE PRECONDITION IS THE GUARD. The sweep below only means anything while the name is
    // actually rendered: a ladder change, a prune bug or a fixture drifting to total:0 would drop
    // the node entirely and make every not.toContain trivially true, and the sweep would stop
    // guarding without a single test going red. So assert the name IS in the DOM first.
    expect(document.body.textContent).toContain('Therapy sessions')

    for (const el of Array.from(document.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.value).not.toContain('Therapy sessions')
      }
    }
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
            // HoldingView.ticker_symbol is `string | undefined`, never null — a security with
            // no ticker is an absent field, and the widget must still mask its name.
            ticker_symbol: undefined,
            security_name: SENTINEL_SECURITY_NAME,
            quantity: 10,
            institution_value: 99999.99,
            day_change_pct: 1.23,
          },
        ],
        count: 1,
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
        // Distinctive so it can serve as a sentinel: the modal rendered the event id
        // unmasked while WebhooksPage masked the same field in its table.
        event_id: 'evt_SENTINEL_pii01',
        event_type: 'subscription.updated',
        status: 'processed',
        created_at: '2026-08-01T10:00:00Z',
        processed_at: '2026-08-01T10:00:01Z',
        // The processing error is a verbatim handler message about a specific customer —
        // it routinely quotes the Paddle payload it failed on. Omitting it from this fixture
        // is why the gate passed 18/18 while the modal rendered it unmasked.
        processing_error: `charge failed for ${SENTINEL_EMAIL}`,
        processing_attempts: 3,
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
      // Batch EC / C1: the same three fields WebhooksPage masks in its table were bare in this
      // modal one click deeper, and untruncated. The email sentinel above covers
      // processing_error, which quotes it; event_id needs its own.
      { value: 'evt_SENTINEL_pii01', fieldDescription: 'Paddle event id in the detail modal' },
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

// ── Round 16 (batch EC): the five admin components the gate never imported ────────────────────
//
// THE DENOMINATOR WAS THE DEFECT. The scanner is sound, but which components it saw was a
// hand-maintained, opt-in list, and DeadLetterPage, DeadLetterDetailModal, UsersPage,
// WebhooksPage and HealthDashboardPage were not in it at all. Worse, coverage inside a listed
// component was per-FIELD and equally opt-in: WebhookDetailModal WAS listed and still missed
// its `processing_error` leak because the fixture omitted the field.
//
// These sections use sentinelsFromFixture(), which makes every string in the fixture a sentinel
// by default and requires a visible skipKeys entry to opt one OUT. Adding a field to a fixture
// now widens coverage automatically; forgetting to list a field no longer silently narrows it.
//
// ⚠ WHAT THIS STILL CANNOT CATCH, stated plainly so nobody reads a green gate as proof of
// absence:
//   1. A field absent from the fixture entirely — the API can return keys these objects do not
//      have, and an absent key renders nothing to scan.
//   2. A component nobody imported here. There is still no import sweep and no lint rule; a new
//      admin page displaying PII is caught by nothing until someone adds a section below.
//   3. A value the component reformats before rendering (truncation past the sentinel, currency
//      formatting, date parsing) — string inclusion misses the transformed text.
//   4. Attribute text the component CONSTRUCTS rather than copies (a template literal that
//      derives an identifier), and any PII that only appears after an interaction no test drives.
//   5. Anything outside the DOM: network payloads, console logs, and PostHog event properties
//      are not what this scanner looks at.

const SENTINEL_DLQ_SUBJECT = 'user_2SENTINELsubjectID'

describe('PII coverage — Round 16: DeadLetterPage list', () => {
  const dlqJob = {
    id: 4242,
    original_job_id: 900100,
    kind: 'sync_transactions',
    queue: 'default',
    max_attempts: 3,
    created_at: '2026-04-01T10:00:00Z',
    failed_at: '2026-04-01T11:00:00Z',
    user_id: SENTINEL_DLQ_SUBJECT,
    notified: false,
    retried_at: undefined,
    retry_job_id: undefined,
    // Free text a staff member typed about one failing job, on a screen filtered by user_id.
    // Kept short: the column truncates at 40 chars, so a sentinel pushed past that would make
    // this test pass for the wrong reason.
    notes: `Called ${SENTINEL_EMAIL}`,
  }

  // Structural / operational fields that carry no personal data and must stay legible to an
  // operator. Every OTHER string in the fixture is a sentinel automatically.
  const DLQ_SKIP = ['kind', 'queue', 'created_at', 'failed_at', 'retried_at'] as const

  beforeEach(() => {
    cleanup()
    mockUseListAdminDeadLetterJobs.mockReturnValue({
      data: { jobs: [dlqJob], total: 1, page: 1, page_size: 20, total_pages: 1, has_more: false },
      isLoading: false,
      error: null,
    } as never)
    mockUseGetAdminDeadLetterJobStats.mockReturnValue({
      data: { total: 1, unnotified: 1, retried: 0, last_24_hours: 1, by_kind: { sync_transactions: 1 } },
      isLoading: false,
      error: null,
    } as never)
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false } as never)
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false } as never)
    mockUseBulkDeleteAdminDeadLetterJobs.mockReturnValue({ mutate: () => {}, isPending: false } as never)
  })

  test('scanForUnmaskedPII returns [] — subject id and staff notes are masked', async () => {
    render(<DeadLetterPage />, { wrapper: createWrapper() })
    const sentinels = sentinelsFromFixture(dlqJob, { skipKeys: DLQ_SKIP, label: 'dlq_job' })
    // Guard the derivation itself: if the helper stopped producing sentinels this test would
    // pass vacuously.
    expect(sentinels.map((s) => s.value).sort()).toEqual(
      [SENTINEL_DLQ_SUBJECT, `Called ${SENTINEL_EMAIL}`].sort()
    )
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // Sibling, failing in the opposite direction: the job kind is the operator's whole reason for
  // being on this page. A blanket mask over the table would satisfy the test above and blind the
  // operator — round 11 established masking at the element, never at the column, for this reason.
  test('the job kind is NOT masked in the table', async () => {
    render(<DeadLetterPage />, { wrapper: createWrapper() })
    // Scoped to the TABLE. The kind filter <select> also renders "sync_transactions" as an
    // <option>, and it is never inside a masked cell — asserting on the first document-wide
    // match would grab that option and pass even with the kind column fully masked.
    const table = await waitFor(() => {
      const t = document.body.querySelector('table')
      expect(t).not.toBeNull()
      return t as HTMLElement
    }, { timeout: 3000 })
    const kindCell = within(table).getByText('sync_transactions')
    expect(kindCell.closest('[data-ph-mask]')).toBeNull()
  })
})

describe('PII coverage — Round 16: DeadLetterDetailModal (the surface one click deeper)', () => {
  const dlqDetail = {
    id: 77,
    original_job_id: 900101,
    kind: 'plaid_sync',
    queue: 'default',
    max_attempts: 3,
    created_at: '2026-04-01T10:00:00Z',
    failed_at: '2026-04-01T11:00:00Z',
    user_id: SENTINEL_DLQ_SUBJECT,
    notified: false,
    // MaskJobArgs redacts a fixed denylist of TOP-LEVEL string keys only — `user_id` and a
    // free-text `reason` survive it, so the "masked" args view is not PII-free.
    args: { user_id: SENTINEL_DLQ_SUBJECT, reason: `manual replay for ${SENTINEL_EMAIL}` },
    errors: [{ message: `no account found for ${SENTINEL_EMAIL}` }],
    notes: `Spoke to ${SENTINEL_EMAIL} on the phone`,
  }
  const rawArgs = { user_id: SENTINEL_DLQ_SUBJECT, access_token: '[REDACTED]', note: SENTINEL_EMAIL }
  const DETAIL_SKIP = ['kind', 'queue', 'created_at', 'failed_at', 'retried_at', 'access_token'] as const

  beforeEach(() => {
    cleanup()
    mockUseGetAdminDeadLetterJobDetail.mockReturnValue({
      data: dlqDetail,
      isLoading: false,
      error: null,
    } as never)
    mockUseGetAdminDeadLetterJobArgs.mockReturnValue({
      data: rawArgs,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    } as never)
    mockUseRetryAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false } as never)
    mockUseAddAdminDeadLetterJobNotes.mockReturnValue({ mutate: () => {}, isPending: false } as never)
    mockUseDeleteAdminDeadLetterJob.mockReturnValue({ mutate: () => {}, isPending: false } as never)
  })

  test('scanForUnmaskedPII returns [] — notes, server-masked args and errors are all masked', async () => {
    render(<DeadLetterDetailModal jobId={77} onClose={() => {}} />, { wrapper: createWrapper() })
    const sentinels = sentinelsFromFixture(dlqDetail, { skipKeys: DETAIL_SKIP, label: 'dlq_detail' })
    expect(sentinels.length).toBeGreaterThanOrEqual(4)
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('scanForUnmaskedPII returns [] after Reveal — the raw args block is masked too', async () => {
    const user = userEvent.setup()
    render(<DeadLetterDetailModal jobId={77} onClose={() => {}} />, { wrapper: createWrapper() })
    await user.click(screen.getByRole('button', { name: /reveal/i }))
    const sentinels = sentinelsFromFixture(rawArgs, { skipKeys: DETAIL_SKIP, label: 'dlq_raw_args' })
    expect(sentinels.map((s) => s.value)).toContain(SENTINEL_EMAIL)
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // Sibling: the queue is operational routing metadata and must stay legible, so a blanket mask
  // over the whole dialog would fail here rather than pass the tests above by brute force.
  test('the queue name is NOT masked', async () => {
    render(<DeadLetterDetailModal jobId={77} onClose={() => {}} />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByText('default')).toBeInTheDocument(), { timeout: 3000 })
    expect(screen.getByText('default').closest('[data-ph-mask]')).toBeNull()
  })
})

describe('PII coverage — Round 16: UsersPage list', () => {
  const adminUser = {
    id: 'user-pii-1',
    email: SENTINEL_EMAIL,
    subscription_tier: 'premium',
    subscription_status: 'active',
    manual_tier_override: null,
    manual_tier_ends_at: null,
    primary_region: 'iad',
    created_at: '2024-01-15T10:00:00Z',
    last_login_at: '2024-03-15T14:30:00Z',
    is_active: true,
    is_admin: false,
    linked_accounts_count: 5,
    boosts_count: 10,
    referrals_count: 2,
  }
  const USERS_SKIP = [
    'id',
    'subscription_tier',
    'subscription_status',
    'primary_region',
    'created_at',
    'last_login_at',
  ] as const

  beforeEach(() => {
    cleanup()
    mockUseListAdminUsers.mockReturnValue({
      data: { users: [adminUser], total_count: 1, page: 1, page_size: 20, total_pages: 1, has_more: false },
      isLoading: false,
      error: null,
    } as never)
  })

  test('scanForUnmaskedPII returns [] — the user email is masked in the table', async () => {
    render(<UsersPage />, { wrapper: createWrapper() })
    const sentinels = sentinelsFromFixture(adminUser, { skipKeys: USERS_SKIP, label: 'admin_user' })
    expect(sentinels.map((s) => s.value)).toEqual([SENTINEL_EMAIL])
    await waitForSentinels(sentinels)
    // Scoped to the results table. UsersPage's tier filter <select> statically enumerates every
    // configured tier's display label, including parenthetical seat counts like "Household (4)",
    // which the scanner's currency regex reads as a parenthesized negative amount — the same
    // documented false positive as the SubscriptionOverrideModal sections above, and unrelated to
    // email masking. Every field of a user RECORD renders inside this table, so scoping here does
    // not narrow what is under test; the assertion below proves the sentinel is actually in it.
    const table = document.body.querySelector('table') as HTMLElement | null
    expect(table).not.toBeNull()
    expect(within(table as HTMLElement).getByText(SENTINEL_EMAIL)).toBeInTheDocument()
    expect(scanForUnmaskedPII(table as HTMLElement, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 16: WebhooksPage list', () => {
  const webhookEvent = {
    id: 'wh-pii-1',
    event_id: 'evt_SENTINEL_01hxyz',
    event_type: 'subscription.updated',
    payload: { data: {} },
    processed: false,
    processed_at: null,
    processing_error: `charge failed for ${SENTINEL_EMAIL}`,
    processing_attempts: 3,
    created_at: '2026-03-22T08:00:00Z',
    updated_at: '2026-03-22T08:30:00Z',
  }
  const WEBHOOK_SKIP = ['id', 'event_type', 'created_at', 'updated_at', 'processed_at'] as const

  beforeEach(() => {
    cleanup()
    mockUseListAdminPaddleWebhooks.mockReturnValue({
      data: { Events: [webhookEvent], Total: 1, Page: 1, PageSize: 20, TotalPages: 1, HasMore: false },
      isLoading: false,
      error: null,
    } as never)
    mockUseGetAdminPaddleWebhookStats.mockReturnValue({
      data: {
        Total: 1,
        Processed: 0,
        Failed: 1,
        Unprocessed: 0,
        AvgProcessingTimeMs: 42,
        EventTypeBreakdown: null,
        TimeRangeHours: 24,
      },
      isLoading: false,
      error: null,
    } as never)
    mockUseGetAdminPaddleWebhookDetail.mockReturnValue({ data: undefined, isLoading: false, error: null } as never)
    mockUseReplayAdminPaddleWebhook.mockReturnValue({
      mutate: () => {},
      isPending: false,
      isSuccess: false,
      isError: false,
    } as never)
  })

  test('scanForUnmaskedPII returns [] — event id and processing error are masked', async () => {
    render(<WebhooksPage />, { wrapper: createWrapper() })
    const sentinels = sentinelsFromFixture(webhookEvent, { skipKeys: WEBHOOK_SKIP, label: 'webhook' })
    expect(sentinels.map((s) => s.value).sort()).toEqual(
      ['evt_SENTINEL_01hxyz', `charge failed for ${SENTINEL_EMAIL}`].sort()
    )
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // C4: the Replay button's aria-label used to interpolate the Paddle event id. Session replay
  // records attributes VERBATIM — data-ph-mask covers text nodes only and cannot reach an
  // attribute — so the remedy is a structural label, not a mask. scanForUnmaskedPII already
  // scans aria-label, but this asserts the structural intent directly so a future edit that
  // reintroduces the identifier fails with an obvious message.
  test('the Replay button aria-label carries no event identifier', async () => {
    render(<WebhooksPage />, { wrapper: createWrapper() })
    // Queried by the loose /replay/i accessible name, NOT by the exact structural label — an
    // exact-name query would make this test fail on "element not found" when the identifier is
    // reintroduced, i.e. pass its mutation for the wrong reason.
    const replay = (await screen.findAllByRole('button', { name: /replay/i }))[0]
    expect(replay.getAttribute('aria-label')).not.toContain('evt_SENTINEL_01hxyz')
    expect(replay.getAttribute('aria-label')).toContain('Replay')
  })
})

describe('PII coverage — Round 16: HealthDashboardPage', () => {
  // This page renders aggregate operational counters only — no per-subject field exists to
  // sentinel, so its value here is (a) being inside the gate at all, and (b) the scanner's
  // currency and account-mask regexes, which fire on any monetary or masked-account text that
  // a future metric might introduce.
  beforeEach(() => {
    cleanup()
    mockUseGetSystemHealth.mockReturnValue({
      data: {
        status: 'healthy',
        timestamp: '2026-01-25T12:00:00Z',
        services: {
          database: {
            status: 'up',
            connections: { active: 10, idle: 5, max: 100, constructing: 2, total: 17 },
            latency_ms: 5,
          },
          clerk: { status: 'up', latency_ms: 100 },
          plaid: { status: 'up', environment: 'sandbox' },
        },
        job_queues: {
          default: { pending: 5, running: 2, failed: 0, completed_last_hour: 100 },
        },
        sync_status: { active_syncs: 3, failed_last_hour: 1, average_duration_ms: 3500 },
        dead_letter_queue: { count: 2, oldest_job_age_hours: 5 },
        paddle_status: {
          active_subscriptions: 150,
          webhook_success_rate_24h: 98.5,
          failed_webhooks_24h: 2,
          total_webhooks24h: 134,
          last_reconciliation_at: '2026-03-22T10:00:00Z',
          reconciliation_discrepancies: 0,
        },
        job_backlog: {
          plaid_cleanup_outstanding_users: 0,
          plaid_cleanup_oldest_outstanding_seconds: 0,
          manual_tier_override_oldest_uncleared_expired_seconds: 0,
          available: true,
        },
        plaid_orphaned_items: {
          auto_retrying: 0,
          manual_intervention_required: 0,
          resolved: 0,
          operator_attested: 0,
          max_unresolved_retry_attempt_count: 0,
          available: true,
        },
      },
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    } as never)
  })

  test('scanForUnmaskedPII returns [] — no currency or account-mask text escapes unmasked', async () => {
    render(<HealthDashboardPage />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByRole('heading', { name: /^Services$/ })).toBeInTheDocument(), {
      timeout: 3000,
    })
    expect(scanForUnmaskedPII(document.body, [])).toEqual([])
  })
})

// --- Round 17 (#1514): three call sites found unmasked/leaking after fourteen prior review
// rounds because none of them was imported into this gate. This is the per-call-site
// stopgap; issue #1521 tracks the structural fix (a lint rule / component-level default) so
// this class of gap stops depending on someone remembering to add an import here. ---

describe('PII coverage — #1625: RulePreviewPanel dry-run results', () => {
  beforeEach(() => {
    cleanup()
    mockPreviewRuleMutateAsync.mockClear()
    mockUsePreviewRule.mockReturnValue({
      mutateAsync: mockPreviewRuleMutateAsync,
      isPending: false,
      error: null,
    } as never)
  })

  // The panel renders RAW transactions — the very rows the rule would touch. That is as
  // sensitive as the transactions table itself, so it is swept centrally rather than trusted
  // to its own unit test.
  test('preview match rows never leak transaction data into an unmasked node or attribute', async () => {
    mockPreviewRuleMutateAsync.mockResolvedValueOnce({
      scanned_count: 200,
      match_count: 1,
      matches: [
        {
          transaction_id: 'tx-pii-1625',
          date: '2026-09-01T00:00:00Z',
          name: SENTINEL_DESC,
          merchant_name: SENTINEL_MERCHANT,
          amount: 1234.56,
          current_category: SENTINEL_CATEGORY_NAME,
        },
      ],
    } as never)

    render(
      <RulePreviewPanel
        name="Sentinel preview rule"
        conditions={[{ type: 'expression', field: 'name', operator: 'contains', value: 'x' }]}
        conditionLogic="AND"
      />,
      { wrapper: createWrapper() },
    )
    fireEvent.click(screen.getByRole('button', { name: /preview matches/i }))

    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'rule_preview.matches[].merchant_name (#1625)' },
      { value: SENTINEL_CATEGORY_NAME, fieldDescription: 'rule_preview.matches[].current_category (#1625)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 17: AutoCategorizationRuleEditor target tags', () => {
  beforeEach(() => {
    cleanup()
    mockUseGetUserCategories.mockReturnValue({ data: [], isLoading: false, error: null })
    mockUseGetPlaidPrimaryCategories.mockReturnValue({ data: [], isLoading: false, error: null })
  })

  const ruleWithTag = {
    id: 'acat-pii-17',
    user_id: 'user-1',
    name: 'Sentinel Editor Rule',
    enabled: true,
    priority: 1,
    conditions: [],
    condition_logic: 'AND' as const,
    target_kind: null,
    target_tags: [SENTINEL_TAG],
    tag_behavior: 'add' as const,
    source: 'manual' as const,
    match_count: 0,
    override_count: 0,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  test('target tag remove button never leaks the tag value into a DOM attribute', async () => {
    render(
      <AutoCategorizationRuleEditor
        mode="edit"
        rule={ruleWithTag}
        onSave={mock(async () => {})}
        onCancel={mock(() => {})}
      />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_TAG, fieldDescription: 'auto_cat_rule.target_tags remove-button label (#1514)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 17: MobileNav / UserDropdown global chrome (name + email)', () => {
  // #1514 fix round 2: the earlier version of this block set `picture: undefined`, so
  // Radix's AvatarImage never mounted an <img> at all and the `alt` attribute path was
  // never exercised here — a gap identical in kind to the one this whole round exists to
  // close. Provide a real picture URL and stub `window.Image` (Radix's AvatarImage only
  // renders an <img> once its internal loading probe resolves "loaded", which never
  // happens against a fake URL in happy-dom) so the <img alt> actually renders.
  const sentinelUser = { name: SENTINEL_USER_NAME, email: SENTINEL_EMAIL, picture: 'https://img.clerk.com/sentinel-avatar' }
  const sentinels: PIISentinel[] = [
    { value: SENTINEL_USER_NAME, fieldDescription: 'authenticated user.name in global nav chrome' },
    { value: SENTINEL_EMAIL, fieldDescription: 'authenticated user.email in global nav chrome' },
  ]

  let OriginalImage: typeof window.Image

  beforeEach(() => {
    cleanup()
    OriginalImage = window.Image
    class ImmediatelyLoadedImage {
      complete = true
      naturalWidth = 1
      src = ''
      addEventListener() {}
      removeEventListener() {}
    }
    // @ts-expect-error -- test stub, not a full HTMLImageElement
    window.Image = ImmediatelyLoadedImage
  })

  afterEach(() => {
    window.Image = OriginalImage
  })

  test('MobileNav masks the signed-in user name/email and uses a structural avatar alt', async () => {
    const user = userEvent.setup()
    render(
      <MobileNav
        isAuthenticated={true}
        user={sentinelUser}
        onLogout={mock(() => {})}
        onLogin={mock(() => {})}
      />,
      { wrapper: createWrapper() },
    )
    await user.click(screen.getByRole('button', { name: /open menu/i }))
    await waitForSentinels(sentinels)

    const avatarImg = document.body.querySelector('img[data-slot="avatar-image"]')
    expect(avatarImg).not.toBeNull()

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('UserDropdown masks the signed-in user name/email and uses a structural avatar alt', async () => {
    const user = userEvent.setup()
    render(
      <UserDropdown user={sentinelUser} onLogout={mock(() => {})} />,
      { wrapper: createWrapper() },
    )
    await user.click(screen.getByRole('button', { name: /user menu/i }))
    await waitForSentinels(sentinels)

    const avatarImg = document.body.querySelector('img[data-slot="avatar-image"]')
    expect(avatarImg).not.toBeNull()

    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 18 (#1514 fix round 2): six sites found by an independent review after the scanner was widened to see data-* attributes', () => {
  beforeEach(() => {
    cleanup()
  })

  test('TopMerchantsTable never puts a merchant name into data-row-id', async () => {
    const data = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_n: 10,
      merchants: [
        { merchant_name: SENTINEL_MERCHANT, total: 150, transaction_count: 15, percentage: 100 },
      ],
    }
    render(
      <TopMerchantsTable data={data} loading={false} sort={null} onSortChange={mock(() => {})} />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'merchant.name in TopMerchantsTable data-row-id (#1514 round 2)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('IncomeSourcePicker masks user tag names in text and <option value>', async () => {
    const user = userEvent.setup()
    render(
      <IncomeSourcePicker
        availableCategories={[]}
        availableTags={[{ id: 'tag-1', user_id: 'user-1', name: SENTINEL_TAG, color: '#000', usage_count: 1, created_at: '', updated_at: '' }]}
        existingSources={[]}
        onAdd={mock(() => {})}
      />,
      { wrapper: createWrapper() },
    )
    await user.selectOptions(screen.getByRole('combobox', { name: /source type/i }), 'tag')
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_TAG, fieldDescription: 'user_tag.name in IncomeSourcePicker <option> (#1514 round 2)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('AutoCategorizationRuleEditor masks custom category and tag names in SearchableSelect option dropdowns', async () => {
    const user = userEvent.setup()
    mockUseGetUserCategories.mockReturnValue({
      data: [{ id: 'cat-sentinel', name: SENTINEL_CATEGORY_NAME }],
      isLoading: false,
      error: null,
    })
    render(
      <AutoCategorizationRuleEditor
        mode="create"
        onSave={mock(async () => {})}
        onCancel={mock(() => {})}
        tags={[{ id: 'tag-sentinel', name: SENTINEL_TAG }]}
      />,
      { wrapper: createWrapper() },
    )
    await user.click(screen.getByRole('combobox', { name: /use a custom category target/i }))
    await screen.findByRole('listbox')
    const categorySentinels: PIISentinel[] = [
      { value: SENTINEL_CATEGORY_NAME, fieldDescription: 'custom_category.name in AutoCategorizationRuleEditor SearchableSelect (#1514 round 2)' },
    ]
    expect(scanForUnmaskedPII(document.body, categorySentinels)).toEqual([])
    await user.keyboard('{Escape}')

    await user.click(screen.getByRole('combobox', { name: /select tag/i }))
    await screen.findByRole('listbox')
    const tagSentinels: PIISentinel[] = [
      { value: SENTINEL_TAG, fieldDescription: 'user_tag.name in AutoCategorizationRuleEditor SearchableSelect (#1514 round 2)' },
    ]
    expect(scanForUnmaskedPII(document.body, tagSentinels)).toEqual([])
  })

  test('AutoCategorizationPage masks the rule name in the delete confirmation dialog', async () => {
    mockUseGetUserRules.mockReturnValue({
      data: [
        {
          id: 'acat-round18',
          user_id: 'user-1',
          name: SENTINEL_RULE_NAME,
          is_enabled: true,
          priority: 1,
          conditions: [],
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
    })
    const user = userEvent.setup()
    render(<AutoCategorizationPage />, { wrapper: createWrapper() })
    await user.click(await screen.findByRole('button', { name: /table/i }))
    const deleteBtn = await screen.findByRole('button', { name: /delete rule/i })
    await user.click(deleteBtn)
    const dialog = await screen.findByRole('alertdialog')
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_RULE_NAME, fieldDescription: 'auto_cat_rule.name in delete confirmation dialog (#1514 round 2)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(dialog, sentinels)).toEqual([])
  })
})

describe('PII coverage — Round 19 (#1514 fix round 3): budget/category-name <option> text in boost condition/action forms', () => {
  // Budget names and formatBudgetCategoryLabel()'s user-authored savings-goal name were
  // rendered as unmasked <option> TEXT (value attributes were already opaque ids).
  // <option> cannot contain a <span>, so the fix is data-ph-mask on the <option> itself.
  const SENTINEL_BUDGET = 'SENTINEL_BUDGET_R19'
  const SENTINEL_SAVINGS_GOAL = 'SENTINEL_SAVINGS_GOAL_R19'

  const budgetsWithSavingsCategory = [
    {
      id: 'budget-r19',
      user_id: 'user-1',
      name: SENTINEL_BUDGET,
      period: 'monthly' as const,
      start_date: '2024-01-01',
      amount: 1000,
      currency_code: 'USD',
      is_active: true,
      income_amount: 0,
      income_sources: [],
      categories: [
        {
          id: 'cat-r19-savings',
          budget_id: 'budget-r19',
          category_kind: 'savings',
          savings_goal_name: SENTINEL_SAVINGS_GOAL,
          allocated_amount: 100,
          spent_amount: 0,
          created_at: '2024-01-01T00:00:00Z',
          updated_at: '2024-01-01T00:00:00Z',
          tags: [],
          match_logic: 'category_only' as const,
        },
      ],
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    },
  ]

  const sentinels: PIISentinel[] = [
    { value: SENTINEL_BUDGET, fieldDescription: 'budget.name in <option> (#1514 round 3)' },
    { value: SENTINEL_SAVINGS_GOAL, fieldDescription: 'savings_goal_name in <option> (#1514 round 3)' },
  ]

  beforeEach(() => {
    cleanup()
  })

  test('ConditionBuilder masks budget and savings-goal names in its budget/category pickers', async () => {
    render(
      <ConditionBuilder
        conditions={[{
          field: 'budget_category.percent_used',
          operator: 'gte',
          value: 80,
          budget_id: 'budget-r19',
        }]}
        logic="AND"
        onConditionsChange={mock(() => {})}
        onLogicChange={mock(() => {})}
        triggerEvent="budget_updated"
        budgets={budgetsWithSavingsCategory as any}
      />,
      { wrapper: createWrapper() },
    )
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // BudgetAlertActionForm and RolloverActionForm are NOT added here — see the import
  // comment above ConditionBuilder for why (useBudgets() would need a process-global
  // mock.module that breaks "PII coverage — Budgets list page" in this same file).
  // Their scanner-based RED/GREEN coverage lives in their own component test files:
  // BudgetAlertActionForm.test.tsx and RolloverActionForm.test.tsx (describe('PII masking
  // (#1514)')).
})

describe('PII coverage — Round 20 (#1514 fix round 4): custom-category-name <option>/<optgroup> leaks', () => {
  beforeEach(() => {
    cleanup()
  })

  test('CustomCategoryForm masks the custom parent category name in the "Nest under" options', () => {
    const parent = {
      id: 'cat-parent-r20',
      user_id: 'user-1',
      name: SENTINEL_CATEGORY_NAME,
      usage_count: 0,
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    }
    render(
      <CustomCategoryForm allCategories={[parent as any]} onSubmit={mock(async () => {})} onCancel={mock(() => {})} />,
      { wrapper: createWrapper() },
    )
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_CATEGORY_NAME, fieldDescription: 'custom_category.name in "Nest under" <option> (#1514 round 4)' },
    ]
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('BudgetCategoryForm masks custom category names and puts no parent name into an <optgroup label>', async () => {
    const parent = { id: 'cat-parent-r20', user_id: 'user-1', name: SENTINEL_CATEGORY_NAME, usage_count: 0, created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }
    const child = { ...parent, id: 'cat-child-r20', name: 'SENTINEL_CHILD_CAT_R20', parent_id: 'cat-parent-r20' }
    mockUseGetUserCategories.mockReturnValue({ data: [parent, child], isLoading: false, error: null })

    const user = userEvent.setup()
    render(
      <BudgetCategoryForm
        isOpen={true}
        onClose={mock(() => {})}
        onSubmit={mock(async () => {})}
        mode="create"
        availableTags={[]}
      />,
      { wrapper: createWrapper() },
    )
    await user.click(screen.getByRole('button', { name: /custom categories/i }))

    const sentinels: PIISentinel[] = [
      { value: SENTINEL_CATEGORY_NAME, fieldDescription: 'custom_category.name in BudgetCategoryForm <option> (#1514 round 4)' },
      { value: 'SENTINEL_CHILD_CAT_R20', fieldDescription: 'custom_category.name (child) in BudgetCategoryForm <option> (#1514 round 4)' },
    ]
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])

    // No <optgroup> may carry the parent's name as its `label` attribute (impossible to
    // mask — see BudgetCategoryForm.tsx's customCategoryOptionGroups doc comment).
    const categorySelect = screen.getByRole('combobox', { name: /^category$/i })
    for (const grp of categorySelect.querySelectorAll('optgroup')) {
      expect(grp.getAttribute('label')).not.toBe(SENTINEL_CATEGORY_NAME)
    }
  })
})

describe('PII coverage — Round 21 (#1514 fix round 5): account-deletion cancellation token', () => {
  // The cancellation token is a bearer CAPABILITY, not PII — the backend's CancelDeletion
  // endpoint is public and validates the request by the token alone (see
  // backend/internal/huma/operations/deletion.go), so anyone who obtains it can cancel a
  // scheduled GDPR erasure. It's included in this centralized gate because the exposure
  // mechanism (attribute + text node) and the fix (keep it out of the DOM entirely) are
  // the same shape as the PII findings elsewhere in this file, but the underlying risk —
  // a stolen capability, not a stolen identity fact — is a different class the masking
  // policy doc was never designed to cover (see the note added there).
  const SENTINEL_TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.SENTINEL_R21.token'

  beforeEach(() => {
    cleanup()
    mockUseRequestDeletion.mockReturnValue({
      mutateAsync: mockRequestDeletionMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockRequestDeletionMutateAsync.mockResolvedValue({
      message: 'Account deletion scheduled',
      scheduled_at: '2025-11-26T00:00:00Z',
      cancellation_token: SENTINEL_TOKEN,
    })
    mockCancelDeletion.mockResolvedValue({ message: 'Account deletion canceled successfully' })
  })

  test('never puts the cancellation token into any DOM attribute or text node', async () => {
    const user = userEvent.setup()
    const { container } = render(<DeleteAccountSection />, { wrapper: createWrapper() })
    await user.click(screen.getByRole('button', { name: /delete my account/i }))
    await user.click(screen.getByRole('checkbox'))
    await user.click(screen.getByRole('button', { name: /yes, delete my account/i }))
    await waitFor(() => expect(screen.getByText(/account deletion scheduled/i)).toBeInTheDocument())

    const sentinels: PIISentinel[] = [
      { value: SENTINEL_TOKEN, fieldDescription: 'account_deletion.cancellation_token (capability, #1514 round 5)' },
    ]
    expect(scanForUnmaskedPII(container, sentinels)).toEqual([])
  })
})

// ── #1569 Task 12 fix round 1 (I3) ────────────────────────────────────────────────────────────
//
// This gate is a hand-written per-surface list, so a new surface is invisible to it until someone
// adds one. Task 12 shipped two and added neither. Worse, the existing "Savings page" block could
// not have reached DeletedGoalsPanel even by accident: the panel's query mock defaults to
// `data: undefined`, and the disclosure starts collapsed.

describe('PII coverage — #1569 T12: DeletedGoalsPanel (deleted savings goals + purge dialog)', () => {
  beforeEach(() => {
    cleanup()
    mockUsePurgeSavingsGoal.mockReturnValue({
      mutateAsync: mockPurgeSavingsGoalMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    } as unknown as ReturnType<typeof mockUsePurgeSavingsGoal>)
    mockUseGetDeletedSavingsGoals.mockReturnValue({
      data: [
        {
          id: 'goal-pii-1',
          user_id: 'user-1',
          name: SENTINEL_GOAL_NAME,
          target_amount: 44444.44,
          current_amount: 33333.33,
          currency_code: 'USD',
          account_id: 'acct-1',
          is_completed: false,
          created_at: '2025-01-01T00:00:00Z',
          updated_at: '2025-06-01T00:00:00Z',
          deleted_at: '2025-06-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve(null)),
    } as unknown as ReturnType<typeof mockUseGetDeletedSavingsGoals>)
  })

  const sentinels: PIISentinel[] = [
    { value: SENTINEL_GOAL_NAME, fieldDescription: 'deleted savings goal name (#1569 T12)' },
  ]

  test('goal name and saved/target amounts are masked in the expanded list', async () => {
    const user = userEvent.setup()
    render(<DeletedGoalsPanel />, { wrapper: createWrapper() })
    await user.click(screen.getByRole('button', { name: /deleted goals/i }))
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  // ── #1569 T7 fix round 1 (item 7) ────────────────────────────────────────────────────────
  //
  // T7 added a THIRD surface under this panel — the retained ledger — and did not add it here.
  // Masking is asserted in DeletedGoalsPanel.test.tsx, so this was a coverage-location gap rather
  // than an unguarded surface, but nothing enforces that this hand-written list is complete: the
  // next surface to be added would be invisible in exactly the same way.
  //
  // The ledger's amounts are the point. They include money the user typed in by hand, which no
  // provider can supply again, and they are read on a screen someone is looking at precisely
  // because they are wondering what happened to it.
  test('the retained contributions ledger masks every amount it renders', async () => {
    mockUseListDeletedSavingsGoalContributions.mockReturnValue({
      data: {
        contributions: [
          {
            id: 'contrib-pii-1',
            savings_goal_id: 'goal-pii-1',
            amount: 91827.36,
            occurred_on: '2025-03-14',
            source: 'manual',
            status: 'confirmed',
            confirmed_by_user: true,
            managed_by_reconciliation: false,
            created_at: '2025-03-14T00:00:00Z',
            updated_at: '2025-03-14T00:00:00Z',
          },
        ],
        total_confirmed: 91827.36,
      },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof mockUseListDeletedSavingsGoalContributions>)

    const ledgerSentinels: PIISentinel[] = [
      { value: '91,827.36', fieldDescription: 'retained contribution amount (#1569 T7)' },
      { value: SENTINEL_GOAL_NAME, fieldDescription: 'deleted savings goal name (#1569 T12)' },
    ]

    const user = userEvent.setup()
    render(<DeletedGoalsPanel />, { wrapper: createWrapper() })
    await user.click(screen.getByRole('button', { name: /deleted goals/i }))
    await user.click(screen.getByRole('button', { name: /contributions:/i }))
    await waitForSentinels(ledgerSentinels)
    expect(scanForUnmaskedPII(document.body, ledgerSentinels)).toEqual([])
  })

  // The confirmation dialog repeats the goal name, and it is the surface a user stares at while
  // deciding to destroy hand-logged money — exactly when a replay would be watched back.
  test('the purge confirmation dialog masks the goal name it repeats', async () => {
    const user = userEvent.setup()
    render(<DeletedGoalsPanel />, { wrapper: createWrapper() })
    await user.click(screen.getByRole('button', { name: /deleted goals/i }))
    await user.click(screen.getByRole('button', { name: /delete permanently/i }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /^delete forever$/i })).toBeInTheDocument()
    )
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — #1569 T12: TransactionTable reversed rows', () => {
  beforeEach(() => {
    cleanup()
  })

  const reversedRow = {
    source: 'banking' as const,
    id: 'txn-pii-reversed',
    user_id: 'user-1',
    account_id: 'acct-1',
    amount: 77777.77,
    date: '2025-03-11',
    name: SENTINEL_MERCHANT,
    merchant_name: SENTINEL_MERCHANT,
    pending: false,
    reversed: true,
    reversed_at: '2025-03-14T09:00:00Z',
    created_at: '2025-03-11T00:00:00Z',
    updated_at: '2025-03-11T00:00:00Z',
  }

  // A reversal adds a badge, a strike-through and a data-reversed marker to the amount cell. The
  // marker is a bare boolean on purpose — attributes are recorded verbatim by session replay and
  // no mask covers them — and this is what will notice if that ever stops being true.
  test('a reversed row leaks neither its amount nor its merchant', async () => {
    const sentinels: PIISentinel[] = [
      { value: SENTINEL_MERCHANT, fieldDescription: 'merchant on a reversed transaction (#1569 T12)' },
    ]
    render(
      <TransactionTable
        transactions={[reversedRow]}
        accountsMap={{}}
      />,
      { wrapper: createWrapper() }
    )
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

// ── #1569 Task 8.6 ────────────────────────────────────────────────────────────────────────────
//
// THE THREE RETAINED-HISTORY SURFACES THIS GATE COULD NOT SEE. Rounds 6–7 shipped four screens
// whose entire purpose is showing money we kept rather than destroyed. Only one of them
// (DeletedGoalsPanel, above) entered this sweep. HoldingsTable was in the sweep but only ever
// rendered as `<HoldingsTable />` — no props, i.e. CURRENT-STATE mode — so its closed-account
// branch was never mounted here; ClosedAccountBalanceHistory and BoostAutomationHistoryPanel were
// absent entirely.
//
// ⚠ STATED PRECISELY: each of these surfaces already carries thorough masking assertions in its
// OWN test file, and all three were in fact already masked when this block was written. Nothing
// below fixed a leak. What it fixes is WHERE the coverage lives — a per-component test protects
// that component only, while this file is the one place a reviewer looks to ask "is every money
// surface covered". A surface outside it is invisible to that question.
//
// ATTRIBUTES ARE THE POINT, not just text. `scanForUnmaskedPII` scans every attribute that is not
// on `pii-masking.ts`'s SAFE_ATTRS denylist and deliberately does NOT honour a masked ancestor
// when it does so, because session replay records attribute values verbatim and `data-ph-mask`
// covers text only. So an `aria-label` carrying an amount — the exact thing
// ClosedAccountBalanceHistory's chart wrapper had to be written around — fails these tests even
// though the visible text passes.
//
// ⚠ MEMBERSHIP IN THIS FILE IS HAND-MAINTAINED, AND NOTHING ENFORCES THAT IT IS COMPLETE. A
// completeness guard was considered for this task and deliberately NOT shipped. The denominator it
// would need — "every component that renders money" — is not soundly derivable from source text
// here: money reaches the DOM through at least four indirections (a `formatCurrency*` call, a
// DataTable column whose `key` names a numeric field with no formatter call in the file at all, a
// shared column array imported from another module, and a chart option builder), and the inverse
// is just as bad — `formatPercent`, `formatHistoryLabel` and plain numeric columns sit in the same
// files without being money. A grep-for-`formatCurrency` guard would therefore both miss surfaces
// and fire on legitimate ones; a guard that fires on legitimate code gets deleted by the next
// engineer, and one that cannot fail is worse than none. Both have already happened on this
// branch. So: **when you add a surface that renders retained money, add it here by hand.** That
// sentence is the mechanism. There is no other.

describe('PII coverage — #1569 T8.6: ClosedAccountHoldingsHistory (a closed account’s retained positions)', () => {
  const closedAccount = {
    id: 'acct-closed-pii',
    name: SENTINEL_ACCOUNT_NAME,
    type: 'investment',
    status: 'removed',
    removed_at: '2026-03-10T00:00:00Z',
    user_account_id: 'ua-closed-pii',
  }

  const position = {
    security_id: 'sec-pii-hist-1',
    as_of_date: '2026-03-04',
    security_name: SENTINEL_SECURITY_NAME,
    ticker_symbol: 'SNTL',
    security_type: 'stock',
    quantity: 12,
    institution_price: 7652,
    institution_value: 91827,
    cost_basis: 71234,
    gain_loss: 20593,
    gain_loss_pct: 28.9,
  }

  // security_id / as_of_date / security_type are structural: an opaque id, a date the surface is
  // REQUIRED to render (it is what stops these figures reading as current), and a fixed enum.
  const sentinels: PIISentinel[] = sentinelsFromFixture(position, {
    skipKeys: ['security_id', 'as_of_date', 'security_type'],
    label: 'holding_snapshot',
  })

  beforeEach(() => {
    cleanup()
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [closedAccount] },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve({})),
    } as unknown as ReturnType<typeof mockUseGetAccounts>)
    // An EMPTY current-state read on a terminal account is what routes HoldingsTable into its
    // history branch. Without this it renders the live table and the surface under test never
    // mounts — the vacuity this file's header warns about.
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [], count: 0 },
      isLoading: false,
      isError: false,
    })
    mockUseGetAccountHoldingsHistory.mockReturnValue({
      data: {
        account_id: closedAccount.id,
        as_of_date: '2026-03-04',
        available_dates: ['2026-03-04', '2026-03-03'],
        positions: [position],
        total_value: 91827,
      },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof mockUseGetAccountHoldingsHistory>)
  })

  // Same process-global reasoning as the balance-history block below: put the documented
  // "nothing was ever captured" default back so a later file does not inherit these positions.
  afterEach(() => {
    mockUseGetAccountHoldingsHistory.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: false,
    })
  })

  test('the retained positions table masks every security name, ticker and amount it renders', async () => {
    render(<HoldingsTable accountId={closedAccount.id} />, { wrapper: createWrapper() })
    // Proves the HISTORY branch mounted, not the live table: the live table has no such caption.
    await waitFor(() =>
      expect(document.body.textContent ?? '').toMatch(/not what the account holds today/i)
    )
    await waitForSentinels(sentinels)
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })

  test('the as-of picker and the total-recorded-value footer leak nothing', async () => {
    render(<HoldingsTable accountId={closedAccount.id} />, { wrapper: createWrapper() })
    // The picker only renders above one retained day, and the footer only when total_value is
    // present — both are seeded above. Confirm each rendered before asserting it is clean.
    await waitFor(() => expect(screen.getByRole('combobox')).toBeInTheDocument())
    await waitFor(() =>
      expect(document.body.textContent ?? '').toMatch(/Total recorded value/i)
    )
    expect(document.body.textContent ?? '').toContain('$91,827.00')
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — #1569 T8.6: ClosedAccountBalanceHistory (retained day-by-day balances)', () => {
  const points = [
    {
      date: '2026-03-01',
      current_balance: 91827.36,
      available_balance: 81726.25,
      currency_code: 'USD',
    },
    {
      date: '2026-03-02',
      current_balance: 76543.21,
      available_balance: 66432.1,
      currency_code: 'USD',
    },
  ]

  // No free-text field exists on a balance point — every leaf is a date, an enum currency code or
  // a number — so there is no fixture-derived sentinel to take. The amounts are caught by
  // scanForUnmaskedPII's currency shape rule instead, in text AND in every scanned attribute,
  // which is the whole risk on this surface: its chart is an ECharts canvas whose only accessible
  // rendering is an aria-label, and an aria-label is recorded verbatim.
  const sentinels: PIISentinel[] = []

  beforeEach(() => {
    cleanup()
    mockUseGetAccountBalanceHistory.mockReturnValue({
      data: { account_id: 'acct-closed-pii', points },
      isLoading: false,
      isError: false,
      error: null,
    })
  })

  // ⚠ THIS RESTORE IS LOAD-BEARING ACROSS FILES. These hook mocks are process-global (they are
  // preloaded once via bunfig.toml), so a mockReturnValue set here is still in effect for every
  // test file bun runs afterwards in the same worker. Leaving retained points seeded broke
  // AccountDetailPage's "a closed account has no balance" test, which reaches this hook
  // incidentally and relies on the documented resolved-and-empty default (see accounts-api.ts).
  afterEach(() => {
    mockUseGetAccountBalanceHistory.mockReturnValue({
      data: { account_id: 'acc-1', points: [] },
      isLoading: false,
      isError: false,
      error: null,
    })
  })

  test('both money columns, the chart wrapper and its aria-label carry no unmasked amount', async () => {
    render(
      <ClosedAccountBalanceHistory
        accountId="acct-closed-pii"
        accountType="investment"
        closedOn="2026-03-10T00:00:00Z"
      />,
      { wrapper: createWrapper() }
    )
    // Both rows must be on screen before the scan, or a pass means nothing.
    await waitFor(() => expect(document.body.textContent ?? '').toContain('$91,827.36'))
    expect(document.body.textContent ?? '').toContain('$66,432.10')
    // The chart is React.lazy + Suspense; wait for it to resolve so its attributes are scanned
    // rather than the fallback's.
    await waitFor(() => expect(screen.getByTestId('echarts-chart')).toBeInTheDocument())
    expect(scanForUnmaskedPII(document.body, sentinels)).toEqual([])
  })
})

describe('PII coverage — #1569 T8.6: BoostAutomationHistoryPanel (runs of deleted boosts)', () => {
  const execution = {
    id: 'exec-pii-1',
    boost_id: 'boost-deleted-pii',
    user_id: 'user-1',
    boost_name: SENTINEL_BOOST_NAME,
    status: 'failed',
    started_at: '2026-03-01T10:00:00Z',
    execution_duration_ms: 1200,
    retry_count: 0,
    max_retries: 3,
    // A failure message is rule/provider text: it can name a merchant and an amount, and it is
    // the one free-text field on this surface.
    error_message: `${SENTINEL_BOOST_ERROR}: could not move $4,812.09 to SENTINEL_MERCHANT_XYZ`,
    trigger_context: { type: 'schedule' },
  }

  // id / boost_id / user_id are opaque keys; status and type are fixed enums the surface must
  // render legibly (a masked status badge would erase the outcome the panel exists to show);
  // started_at is a timestamp the row is required to state.
  const sentinels: PIISentinel[] = sentinelsFromFixture(execution, {
    skipKeys: ['id', 'boost_id', 'user_id', 'status', 'started_at', 'type'],
    label: 'boost_execution',
  })

  beforeEach(() => {
    cleanup()
    // An EMPTY live-boost list is what makes this row a DELETED boost's retained run — the case
    // the panel was built for. It is also the harder case to mask: the cell pairs the
    // user-authored name with a "Deleted boost" marker that must stay legible.
    mockUseGetBoosts.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as unknown as ReturnType<typeof mockUseGetBoosts>)
    mockUseGetUserBoostExecutions.mockReturnValue({
      data: { executions: [execution], total: 1, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as unknown as ReturnType<typeof mockUseGetUserBoostExecutions>)
  })

  // Same process-global reasoning as the two blocks above.
  afterEach(() => {
    mockUseGetUserBoostExecutions.mockReturnValue({
      data: { executions: [], total: 0, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserBoostExecutions,
    } as unknown as ReturnType<typeof mockUseGetUserBoostExecutions>)
    mockUseGetBoosts.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as unknown as ReturnType<typeof mockUseGetBoosts>)
  })

  test('the retained boost name and failure message are masked once the panel is expanded', async () => {
    const user = userEvent.setup()
    render(<BoostAutomationHistoryPanel />, { wrapper: createWrapper() })
    const toggle = screen.getByRole('button', { name: /automation history/i })
    await user.click(toggle)
    // The marker proves this really is the deleted-boost path.
    await waitFor(() => expect(screen.getByText('Deleted boost')).toBeInTheDocument())
    await waitForSentinels(sentinels)

    // ⚠ SCOPED TO THE EXPANDED PANEL, NOT document.body — and this is the ONE deviation from this
    // file's default, so it is worth being exact about. The toggle's own label is
    // `Automation history{` (${total})`}`, a single text node reading " (1)", and
    // pii-masking.ts's CURRENCY_RE treats a parenthesised number as an accounting negative
    // ("($1,234)"). That is a row COUNT, not money, so scanning the toggle would fail this test on
    // legitimate code. (DeletedGoalsPanel writes the same count as `Deleted goals ({n})`, which
    // React splits into three text nodes — "Deleted goals (", "1", ")" — so it evades the same
    // rule by accident of node splitting, not by design.) Nothing here portals, so the panel
    // element contains everything this surface renders.
    //
    // The excluded region is not left unasserted: the line below pins the toggle's entire text to
    // "Automation history (N)", so an amount appearing there would fail on its own assertion.
    expect(toggle.textContent).toMatch(/^Automation history \(\d+\)$/)
    const panel = document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    expect(panel).not.toBeNull()
    expect(scanForUnmaskedPII(panel as HTMLElement, sentinels)).toEqual([])
  })
})

// ── #1569 T9.4 ────────────────────────────────────────────────────────────────────────────────
//
// RuleAutomationHistoryPanel — the automation AUDIT TRAIL of a rule the user has since deleted.
// The sibling block above covers the boost twin; this is the rule one, and its PII shape is
// different in a way worth stating: the boost row's only free text is an error message, while this
// row carries the rule's CONDITIONS, restored out of a frozen `definition_snapshot`. Those are
// user-typed merchant substrings and amounts — the same class of text as a merchant name on a
// transaction — reaching the DOM through an indirection (a JSON blob) that no `formatCurrency`
// grep would ever find. That is exactly the kind of surface this file exists to catch.
//
// The condition LABEL ("Merchant contains") is a fixed vocabulary and is deliberately NOT a
// sentinel: masking it would erase the shape of the rule from replay while protecting nothing.
describe('PII coverage — #1569 T9.4: RuleAutomationHistoryPanel (runs of deleted rules)', () => {
  const execution = {
    id: 'exec-pii-rule-1',
    rule_id: null,
    transaction_id: 'txn-pii-1',
    user_id: 'user-1',
    rule_name: SENTINEL_RULE_NAME,
    action_type: 'category',
    result: 'applied',
    executed_at: '2026-03-01T10:00:00Z',
    definition_snapshot: {
      name: SENTINEL_RULE_NAME,
      condition_logic: 'AND',
      conditions: [
        { type: 'merchant_contains', value: SENTINEL_MERCHANT },
        { type: 'amount_greater_than', value: 4812.09 },
      ],
      captured_by: 'rule_application_service',
    },
  }

  // id / rule_id / transaction_id / user_id are opaque keys; action_type and result are fixed
  // enums the surface must render legibly (a masked outcome would erase what the panel exists to
  // show); executed_at is a timestamp the row is required to state; `type` and `condition_logic`
  // are the fixed condition vocabulary, rendered through CONDITION_TYPE_LABELS rather than
  // verbatim; captured_by is capture metadata that is deliberately never rendered at all.
  const sentinels: PIISentinel[] = [
    ...sentinelsFromFixture(execution, {
      skipKeys: [
        'id',
        'rule_id',
        'transaction_id',
        'user_id',
        'action_type',
        'result',
        'executed_at',
        'type',
        'condition_logic',
        'captured_by',
      ],
      label: 'rule_execution',
    }),
    // Numbers are not walked by sentinelsFromFixture, and an amount inside a condition is money
    // the user typed. Named explicitly so it is covered rather than silently skipped.
    { value: '4812.09', fieldDescription: 'rule_execution.definition_snapshot condition amount' },
  ]

  beforeEach(() => {
    cleanup()
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [execution], total: 1, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mock(() => Promise.resolve()),
    } as unknown as ReturnType<typeof mockUseGetUserRuleExecutions>)
  })

  // Same process-global reasoning as the blocks above.
  afterEach(() => {
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [], total: 0, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRuleExecutions,
    } as unknown as ReturnType<typeof mockUseGetUserRuleExecutions>)
  })

  test('the retained rule name and its frozen conditions are masked once the panel is expanded', async () => {
    const user = userEvent.setup()
    render(<RuleAutomationHistoryPanel />, { wrapper: createWrapper() })
    const toggle = screen.getByRole('button', { name: /rule run history/i })
    await user.click(toggle)
    // The marker proves this really is the deleted-rule path.
    await waitFor(() => expect(screen.getByText('Deleted rule')).toBeInTheDocument())
    await waitForSentinels(sentinels)

    // ⚠ SCOPED TO THE EXPANDED PANEL for the same reason the boost block is: the toggle's own
    // label ends in " (1)", and pii-masking.ts's CURRENCY_RE reads a parenthesised number as an
    // accounting negative. That is a row COUNT, not money. The excluded region is not left
    // unasserted — the line below pins the toggle's entire text.
    expect(toggle.textContent).toMatch(/^Rule run history \(\d+\)$/)
    const panel = document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    expect(panel).not.toBeNull()
    expect(scanForUnmaskedPII(panel as HTMLElement, sentinels)).toEqual([])
  })
})
