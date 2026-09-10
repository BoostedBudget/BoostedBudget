/**
 * AutoCategorizationPage Tests
 * Issue #346: Custom Transaction Categorization Rules
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * NOTE: Uses API-level mocks to test the page with real useAutoCategorizationRules hook.
 * This prevents mock pollution that would break useAutoCategorizationRules.test.tsx.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { CategorizationRule } from '../types'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'
import {
  mockUseGetUserRules,
  mockRefetchUserRules,
  mockCreateRuleMutateAsync,
  mockUseCreateRule,
  mockUpdateRuleMutateAsync,
  mockUseUpdateRule,
  mockDeleteRuleMutateAsync,
  mockUseDeleteRule,
  mockToggleRuleMutateAsync,
  mockUseToggleRule,
  mockUseGetUserRuleExecutions,
  mockRefetchUserRuleExecutions,
} from '@/test/mocks/auto-categorization-api'
import { mockUseGetAccounts, mockRefetchAccounts } from '@/test/mocks/accounts-api'

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{ui}</BrowserRouter>
    </QueryClientProvider>
  )
}

describe('AutoCategorizationPage', () => {
  const mockRule1: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    condition_logic: 'AND',
    target_kind: 'pfc',
    target_pfc_primary: 'FOOD_AND_DRINK',
    target_pfc_detailed: 'FOOD_AND_DRINK_COFFEE',
    target_tags: ['morning'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  const mockRule2: CategorizationRule = {
    ...mockRule1,
    id: 'rule-2',
    name: 'Groceries',
    priority: 5,
  }

  const mockCategories = [
    ['Food & Drink'],
    ['Food & Drink', 'Coffee'],
    ['Shopping'],
  ]

  function configureQuery(config: {
    data?: CategorizationRule[] | null
    isLoading?: boolean
    error?: Error | null
  }) {
    mockUseGetUserRules.mockReturnValue({
      data: config.data ?? undefined,
      isLoading: config.isLoading ?? false,
      error: config.error ?? null,
      refetch: mockRefetchUserRules,
    })
  }

  function configureMutations() {
    mockUseCreateRule.mockReturnValue({
      mutateAsync: mockCreateRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseUpdateRule.mockReturnValue({
      mutateAsync: mockUpdateRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseDeleteRule.mockReturnValue({
      mutateAsync: mockDeleteRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
    mockUseToggleRule.mockReturnValue({
      mutateAsync: mockToggleRuleMutateAsync,
      isPending: false,
      isError: false,
      error: null,
    })
  }

  beforeEach(() => {
    mockUseGetUserRules.mockClear()
    mockRefetchUserRules.mockClear()
    mockCreateRuleMutateAsync.mockClear()
    mockUseCreateRule.mockClear()
    mockUpdateRuleMutateAsync.mockClear()
    mockUseUpdateRule.mockClear()
    mockDeleteRuleMutateAsync.mockClear()
    mockUseDeleteRule.mockClear()
    mockToggleRuleMutateAsync.mockClear()
    mockUseToggleRule.mockClear()

    // #1626: restore the preloaded "accounts still loading" default so an accounts fixture set by
    // one test cannot leak into the next.
    mockUseGetAccounts.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
      refetch: mockRefetchAccounts,
    } as unknown as ReturnType<typeof mockUseGetAccounts>)

    // Default configuration - not loading, no error, empty rules
    configureQuery({ data: [] })
    configureMutations()
    mockCreateRuleMutateAsync.mockResolvedValue(mockRule1)
    mockUpdateRuleMutateAsync.mockResolvedValue(mockRule1)
    mockDeleteRuleMutateAsync.mockResolvedValue({})
    mockToggleRuleMutateAsync.mockResolvedValue({ ...mockRule1, enabled: false })
  })

  afterEach(() => {
    window.history.replaceState({}, '', '/')
    localStorage.clear()
  })

  describe('Loading state', () => {
    it('should show loading spinner when loading', () => {
      configureQuery({ isLoading: true })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/loading rules/i)).toBeInTheDocument()
    })
  })

  describe('Error state', () => {
    it('should show error message when error occurs', () => {
      configureQuery({ error: new Error('Failed to fetch rules') })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/failed to fetch rules/i)).toBeInTheDocument()
    })

    it('should show retry button on error', async () => {
      configureQuery({ error: new Error('Network error') })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      await waitFor(() => {
        expect(mockRefetchUserRules).toHaveBeenCalled()
      })
    })
  })

  describe('Empty state', () => {
    it('should show empty state when no rules', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/no rules yet/i)).toBeInTheDocument()
    })

    it('should show create button in empty state', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
    })
  })

  describe('Rules list', () => {
    it('should render list of rules', () => {
      configureQuery({ data: [mockRule1, mockRule2] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText('Coffee Shops')).toBeInTheDocument()
      expect(screen.getByText('Groceries')).toBeInTheDocument()
    })

    it('should show rule count', () => {
      configureQuery({ data: [mockRule1, mockRule2] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      expect(screen.getByText(/2 rules/i)).toBeInTheDocument()
    })
  })

  describe('Create rule', () => {
    it('should open editor when Create Rule is clicked', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })

    it('should close editor on cancel', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('heading', { name: /create rule/i })).not.toBeInTheDocument()
    })

    it('should show editor form when create button clicked', () => {
      configureQuery({ data: [] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /create.*rule/i }))
      expect(screen.getByRole('heading', { name: /create rule/i })).toBeInTheDocument()
    })
  })

  describe('Edit rule', () => {
    it('should open editor with rule data when Edit is clicked', () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      expect(screen.getByRole('heading', { name: /edit rule/i })).toBeInTheDocument()
      expect(screen.getByDisplayValue('Coffee Shops')).toBeInTheDocument()
    })
  })

  describe('Delete rule', () => {
    it('should call deleteRule when delete is confirmed (cards view)', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      const deleteButton = screen.getByRole('button', { name: 'Delete' })
      fireEvent.click(deleteButton)
      const dialog = await screen.findByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
      await waitFor(() => {
        expect(mockDeleteRuleMutateAsync).toHaveBeenCalled()
      })
    })

    it('should show confirmation dialog before deleting in table view', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)

      // Switch to table view
      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      // Click the delete button in the table row
      const deleteButton = await screen.findByRole('button', { name: /delete rule/i })
      fireEvent.click(deleteButton)

      // Confirmation dialog should open without calling deleteRule
      await waitFor(() => {
        expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      })
      expect(mockDeleteRuleMutateAsync).not.toHaveBeenCalled()
    })

    it('should call deleteRule when table view delete is confirmed', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)

      // Switch to table view
      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      const deleteButton = await screen.findByRole('button', { name: /delete rule/i })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))

      await waitFor(() => {
        expect(mockDeleteRuleMutateAsync).toHaveBeenCalled()
      })
    })

    it('should not call deleteRule when table view delete is cancelled', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      const deleteButton = await screen.findByRole('button', { name: /delete rule/i })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      fireEvent.click(within(dialog).getByRole('button', { name: /cancel/i }))

      await waitFor(() => {
        expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      })
      expect(mockDeleteRuleMutateAsync).not.toHaveBeenCalled()
    })

    // Issue #1514 (fix round 2): the delete-confirmation description interpolated the
    // user-authored rule name directly into DialogDescription TEXT with no data-ph-mask —
    // `Are you sure you want to delete "${tableDeleteTarget.name}"?`. Assert with the
    // scanner (sentinel-based), not just visual inspection, so a future edit that drops
    // the mask wrapper fails loudly.
    it('masks the rule name in the table view delete confirmation dialog', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      const deleteButton = await screen.findByRole('button', { name: /delete rule/i })
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('alertdialog')
      expect(within(dialog).getByText(new RegExp(mockRule1.name))).toBeInTheDocument()

      const sentinels: PIISentinel[] = [
        { value: mockRule1.name, fieldDescription: 'auto_cat_rule.name in delete confirmation dialog' },
      ]
      expect(scanForUnmaskedPII(dialog, sentinels)).toEqual([])
    })
  })

  describe('Toggle rule', () => {
    it('should call toggleRule when toggle is clicked', async () => {
      configureQuery({ data: [mockRule1] })
      renderWithProviders(<AutoCategorizationPage categories={mockCategories} />)
      fireEvent.click(screen.getByRole('button', { name: /disable/i }))
      await waitFor(() => {
        expect(mockToggleRuleMutateAsync).toHaveBeenCalled()
      })
    })
  })

})

/**
 * Issue #1569 T9.4 — where the retained rule-run history is REACHABLE from.
 *
 * ⚠ THE ZERO-RULES BRANCH IS THE LOAD-BEARING ONE. `AutoCategorizationPage` early-returns an
 * "No rules yet" empty state when the user has no live rules, and that is EXACTLY the state a user
 * who has deleted all their rules is in — the state in which the retained history matters most and
 * had, before this, no reachable surface at all. Mounting the panel only in the happy path would
 * have shipped a seat that vanishes at the moment it is needed.
 *
 * The error branch is included for the same reason the boost page includes it: the history is a
 * SEPARATE query with its own loading, error and empty handling, so a blip on the live rules list
 * is no reason to also erase every record of what the user's automations already did.
 */
describe('AutoCategorizationPage — retained rule run history (#1569 T9.4)', () => {
  const mockPageRule: CategorizationRule = {
    id: 'rule-page-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    enabled: true,
    priority: 10,
    conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    condition_logic: 'AND',
    target_kind: 'pfc',
    target_pfc_primary: 'FOOD_AND_DRINK',
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-15T10:00:00Z',
  }

  const execution = {
    id: 'exec-page-1',
    rule_id: null,
    transaction_id: 'txn-1',
    user_id: 'user-1',
    rule_name: 'Old rainy-day rule',
    action_type: 'category',
    result: 'applied',
    executed_at: '2026-02-01T10:00:00Z',
    definition_snapshot: {
      condition_logic: 'AND',
      conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
    },
  }

  beforeEach(() => {
    mockUseGetUserRules.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRules,
    } as unknown as ReturnType<typeof mockUseGetUserRules>)
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [execution], total: 1, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRuleExecutions,
    } as ReturnType<typeof mockUseGetUserRuleExecutions>)
  })

  afterEach(() => {
    // mock.module is process-global; restore the preloaded empty-page default.
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [], total: 0, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRuleExecutions,
    } as ReturnType<typeof mockUseGetUserRuleExecutions>)
  })

  it('is reachable when the user has deleted every rule (the "No rules yet" state)', async () => {
    renderWithProviders(<AutoCategorizationPage categories={[]} />)
    expect(screen.getByText(/no rules yet/i)).toBeInTheDocument()

    const toggle = await screen.findByRole('button', { name: /rule run history/i })
    fireEvent.click(toggle)
    expect(await screen.findByText('Old rainy-day rule')).toBeInTheDocument()
    expect(screen.getByText('Deleted rule')).toBeInTheDocument()
  })

  it('is reachable alongside the user’s live rules', async () => {
    mockUseGetUserRules.mockReturnValue({
      data: [mockPageRule],
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRules,
    } as unknown as ReturnType<typeof mockUseGetUserRules>)
    renderWithProviders(<AutoCategorizationPage categories={[]} />)
    expect(await screen.findByRole('button', { name: /rule run history/i })).toBeInTheDocument()
  })

  it('survives a failure of the LIVE rules query', async () => {
    mockUseGetUserRules.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('rules unavailable'),
      refetch: mockRefetchUserRules,
    } as unknown as ReturnType<typeof mockUseGetUserRules>)
    renderWithProviders(<AutoCategorizationPage categories={[]} />)
    expect(await screen.findByRole('button', { name: /rule run history/i })).toBeInTheDocument()
  })

  it('renders no history seat at all when nothing has ever run', () => {
    mockUseGetUserRuleExecutions.mockReturnValue({
      data: { executions: [], total: 0, limit: 20, offset: 0 },
      isLoading: false,
      error: null,
      refetch: mockRefetchUserRuleExecutions,
    } as ReturnType<typeof mockUseGetUserRuleExecutions>)
    renderWithProviders(<AutoCategorizationPage categories={[]} />)
    expect(screen.queryByRole('button', { name: /rule run history/i })).not.toBeInTheDocument()
  })

  /**
   * Issue #1626 (Task K). The card resolves account names from a PROP, so the one thing that can
   * silently regress is the page forgetting to supply it — the card would quietly fall back to
   * "Unknown account" and nothing else would notice. This test is that wiring's only guard.
   */
  describe('account conditions on the rules list (#1626)', () => {
    const ACCOUNT_UUID = '32a0e8da-8aa6-44ac-9be7-3a321e1402c7'

    afterEach(() => {
      // mock.module is process-global; restore the preloaded "still loading" accounts default.
      mockUseGetAccounts.mockReturnValue({
        data: undefined,
        isLoading: true,
        isError: false,
        error: null,
        refetch: mockRefetchAccounts,
      } as unknown as ReturnType<typeof mockUseGetAccounts>)
    })

    it('renders the account name for an account_id condition', async () => {
      mockUseGetAccounts.mockReturnValue({
        data: {
          accounts: [
            { id: ACCOUNT_UUID, name: 'Plaid Checking', nickname: 'Everyday Spending', type: 'depository', status: 'active' },
          ],
        },
        isLoading: false,
        isError: false,
        error: null,
        refetch: mockRefetchAccounts,
      } as unknown as ReturnType<typeof mockUseGetAccounts>)
      mockUseGetUserRules.mockReturnValue({
        data: [{ ...mockPageRule, conditions: [{ type: 'account_id' as const, value: ACCOUNT_UUID }] }],
        isLoading: false,
        error: null,
        refetch: mockRefetchUserRules,
      } as unknown as ReturnType<typeof mockUseGetUserRules>)

      renderWithProviders(<AutoCategorizationPage categories={[]} />)

      expect(await screen.findByText('Everyday Spending')).toBeInTheDocument()
      expect(screen.queryByText(ACCOUNT_UUID)).toBeNull()
      expect(screen.queryByText('Unknown account')).toBeNull()
    })
  })
})
