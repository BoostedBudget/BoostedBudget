/**
 * RulesPage Component Tests
 * Issue #39: Rule Builder UI
 * Issue #575: Migrated to bun:test with centralized API mocks
 *
 * Tests for the main rules list page.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import type { Boost } from '@boostedbudget/api-client/models'
import { BoostsManagePage } from './BoostsManagePage'

// Import centralized mocks for configuration
import {
  mockUseGetBoosts,
  mockUseToggleBoost,
  mockToggleBoostMutate,
  mockToggleBoostMutateAsync,
  mockDeleteBoostMutate,
  mockTestBoostMutate,
  mockRefetchBoosts,
} from '@/test/mocks/boosts-api'
import { getGetBoostsQueryKey } from '@boostedbudget/api-client/endpoints/boosts/boosts'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers } from '@/test/mocks/subscription-api'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import { mockTrackEvent } from '@/test/mocks/track-event'

const mockBoosts: Boost[] = [
  {
    id: 'boost-1',
    user_id: 'user-1',
    name: 'Daily Budget Check',
    description: 'Check budget usage daily',
    boost_type: 'budget_alert',
    enabled: true,
    priority: 10,
    execution_count: 5,
    trigger_config: {
      type: 'schedule',
      cron: '0 9 * * *',
      timezone: 'America/New_York',
    },
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-15T00:00:00Z',
    last_executed_at: '2024-01-15T09:00:00Z',
  } as Boost,
  {
    id: 'boost-2',
    user_id: 'user-1',
    name: 'Monthly Savings',
    boost_type: 'savings_goal',
    enabled: false,
    priority: 5,
    execution_count: 12,
    trigger_config: {
      type: 'schedule',
      cron: '0 9 1 * *',
      timezone: 'America/New_York',
    },
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-10T00:00:00Z',
  } as Boost,
]

// Mock user data with premium tier
const mockUser = {
  id: 'user-1',
  email: 'test@example.com',
  subscription_tier: 'premium' as const,
  display_name: 'Test User',
  timezone: 'America/New_York',
  currency_code: 'USD',
  locale: 'en-US',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

function renderWithProviders(ui: React.ReactElement, { initialUrl = '/boosts' } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialUrl]}>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('RulesPage', () => {
  beforeEach(() => {
    // Clear all mock call history
    mockUseGetBoosts.mockClear()
    mockToggleBoostMutate.mockClear()
    mockDeleteBoostMutate.mockClear()
    mockTestBoostMutate.mockClear()
    mockRefetchBoosts.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    mockTrackEvent.mockClear()
    localStorage.clear()

    // Configure automation rules mock
    mockUseGetBoosts.mockReturnValue({
      data: mockBoosts,
      isLoading: false,
      error: null,
      refetch: mockRefetchBoosts,
    })

    // Configure profile mock - user with premium tier
    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })

    // Configure subscription tiers mock
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: () => Promise.resolve(),
    })

  })

  it('should render page title', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByText(/automation boosts/i)).toBeInTheDocument()
    })
  })

  it('should render rule count display', async () => {
    renderWithProviders(<BoostsManagePage />)

    // With 2 rules loaded, should show "2 rules"
    await waitFor(() => {
      expect(screen.getByText(/2 boosts/i)).toBeInTheDocument()
    })
  })

  it('should render create button', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create.*boost/i })).toBeInTheDocument()
    })
  })

  it('should render view toggle', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
    })
  })

  it('should render rules list', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })
    expect(screen.getByText('Monthly Savings')).toBeInTheDocument()
  })

  it('should show loading state', async () => {
    mockUseGetBoosts.mockReturnValue({
      data: null,
      isLoading: true,
      error: null,
      refetch: mockRefetchBoosts,
    })
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  it('should show error state', async () => {
    mockUseGetBoosts.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
      refetch: mockRefetchBoosts,
    })
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })
  })

  it('should show retry button on error', async () => {
    mockUseGetBoosts.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
      refetch: mockRefetchBoosts,
    })
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
    })
  })

  it('should show empty state when no rules', async () => {
    mockUseGetBoosts.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchBoosts,
    })
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByText(/no automation boosts/i)).toBeInTheDocument()
    })
  })

  it('should show create button in empty state', async () => {
    mockUseGetBoosts.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchBoosts,
    })
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      const createButtons = screen.getAllByRole('button', { name: /create/i })
      expect(createButtons.length).toBeGreaterThan(0)
    })
  })

  it('should default to cards view', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      const cardsButton = screen.getByRole('button', { name: /cards/i })
      expect(cardsButton).toHaveAttribute('aria-pressed', 'true')
    })
  })

  it('should switch to table view when clicked', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /table/i })).toBeInTheDocument()
    })

    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)
    expect(tableButton).toHaveAttribute('aria-pressed', 'true')
  })

  it('should show cards in cards view', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      const cards = screen.getAllByRole('article')
      expect(cards.length).toBe(2)
    })
  })

  it('should show table in table view', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /table/i })).toBeInTheDocument()
    })

    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)

    await waitFor(() => {
      expect(screen.getByRole('table')).toBeInTheDocument()
    })
  })

  it('should toggle rule when switch is clicked', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('switch').length).toBeGreaterThan(0)
    })

    const toggles = screen.getAllByRole('switch')
    fireEvent.click(toggles[0])
    expect(mockToggleBoostMutate).toHaveBeenCalled()
  })

  it('should open delete confirmation when delete is clicked', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })
  })

  it('masks rule name with data-ph-mask in delete-confirmation dialog (PII — session replay)', async () => {
    // R4-3: the rule name is interpolated into the dialog description. Session replay records
    // unmasked DOM text; the name must be wrapped in a data-ph-mask span.
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    // The rule name ("Daily Budget Check") must appear inside a data-ph-mask element
    const dialog = screen.getByRole('alertdialog')
    const maskedEls = dialog.querySelectorAll('[data-ph-mask]')
    const nameIsMasked = Array.from(maskedEls).some((el) =>
      el.textContent?.includes('Daily Budget Check')
    )
    expect(nameIsMasked).toBe(true)
  })

  it('should call delete mutation when confirmed', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    const confirmButton = within(screen.getByRole('alertdialog')).getByRole('button', { name: /delete/i })
    fireEvent.click(confirmButton)
    await waitFor(() => {
      expect(mockDeleteBoostMutate).toHaveBeenCalled()
    })
  })

  it('fires RULE_DELETED analytics after delete is confirmed', async () => {
    mockDeleteBoostMutate.mockImplementationOnce(
      (_args: unknown, opts?: { onSuccess?: () => void }) => { opts?.onSuccess?.() }
    )

    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    const confirmButton = within(screen.getByRole('alertdialog')).getByRole('button', { name: /delete/i })
    fireEvent.click(confirmButton)

    await waitFor(() => {
      expect(mockTrackEvent).toHaveBeenCalledWith('boost_deleted')
    })
  })

  it('should have accessible page heading', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
    })
  })

  it('should have accessible view toggle group', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
    })
  })

  describe('Sorting', () => {
    // Input order: Bravo, Alpha, Charlie — distinctly different from any single sort direction
    const sortableRules: Boost[] = [
      {
        id: 'bravo',
        user_id: 'user-1',
        name: 'Bravo Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 2,
        execution_count: 0,
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
      {
        id: 'alpha',
        user_id: 'user-1',
        name: 'Alpha Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 1,
        execution_count: 0,
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
      {
        id: 'charlie',
        user_id: 'user-1',
        name: 'Charlie Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 3,
        execution_count: 0,
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
    ]

    const lastRunRules: Boost[] = [
      {
        id: 'no-run',
        user_id: 'user-1',
        name: 'Never Run Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 1,
        execution_count: 0,
        last_executed_at: undefined,
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
      {
        id: 'early-run',
        user_id: 'user-1',
        name: 'Early Run Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 2,
        execution_count: 5,
        last_executed_at: '2026-01-01T00:00:00Z',
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
      {
        id: 'late-run',
        user_id: 'user-1',
        name: 'Late Run Rule',
        enabled: true,
        boost_type: 'tag',
        priority: 3,
        execution_count: 10,
        last_executed_at: '2026-03-01T00:00:00Z',
        trigger_config: { type: 'event' } as never,
        conditions: [],
        condition_logic: 'AND',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: {} as any,
        created_at: '',
        updated_at: '',
      } as never,
    ]

    it('renders rows in alphabetical order when ?sort=name:asc is in the URL', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: sortableRules,
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })
      renderWithProviders(<BoostsManagePage />, { initialUrl: '/boosts?sort=name:asc' })

      fireEvent.click(screen.getByTitle('Table view'))

      await waitFor(() => {
        const rows = screen.getAllByRole('row').slice(1)
        expect(rows[0]).toHaveTextContent('Alpha Rule')
        expect(rows[2]).toHaveTextContent('Charlie Rule')
      })
    })

    it('clicking the Name header cycles asc → desc → unsorted', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: sortableRules,
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })
      renderWithProviders(<BoostsManagePage />)

      fireEvent.click(screen.getByTitle('Table view'))

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument()
      })

      const nameButton = () => screen.getByText('Name').closest('button')!

      // 1st click → asc: Alpha first
      fireEvent.click(nameButton())
      await waitFor(() => {
        const rows = screen.getAllByRole('row').slice(1)
        expect(rows[0]).toHaveTextContent('Alpha Rule')
      })

      // 2nd click → desc: Charlie first
      fireEvent.click(nameButton())
      await waitFor(() => {
        const rows = screen.getAllByRole('row').slice(1)
        expect(rows[0]).toHaveTextContent('Charlie Rule')
      })

      // 3rd click → unsorted: back to input order (Bravo first)
      fireEvent.click(nameButton())
      await waitFor(() => {
        const rows = screen.getAllByRole('row').slice(1)
        expect(rows[0]).toHaveTextContent('Bravo Rule')
      })
    })

    it('places lastRun=null rules last when sorting lastRun desc (regression #1134)', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: lastRunRules,
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })
      renderWithProviders(<BoostsManagePage />, { initialUrl: '/boosts?sort=lastRun:desc' })

      fireEvent.click(screen.getByTitle('Table view'))

      await waitFor(() => {
        const rows = screen.getAllByRole('row').slice(1)
        // Late Run first (most recent), Early Run second, Never Run last
        expect(rows[0]).toHaveTextContent('Late Run Rule')
        expect(rows[2]).toHaveTextContent('Never')
      })
    })
  })

  it('should call test mutation when test button is clicked in cards view', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /test/i }).length).toBeGreaterThan(0)
    })

    const testButtons = screen.getAllByRole('button', { name: /test/i })
    fireEvent.click(testButtons[0])
    expect(mockTestBoostMutate).toHaveBeenCalled()
  })

  it('should call test mutation when test button is clicked in table view', async () => {
    renderWithProviders(<BoostsManagePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /table/i })).toBeInTheDocument()
    })

    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /test/i }).length).toBeGreaterThan(0)
    })

    const testButtons = screen.getAllByRole('button', { name: /test/i })
    fireEvent.click(testButtons[0])
    expect(mockTestBoostMutate).toHaveBeenCalled()
  })

  // R8-1c: Rule type segment for Boosts Rules tab (#1267)
  describe('Rule type segment (R8-1c)', () => {
    it('renders segment control when showTypeSegment is true', async () => {
      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => {
        expect(screen.getByRole('group', { name: /boost type filter/i })).toBeInTheDocument()
      })
      expect(screen.getByRole('button', { name: /transfer & savings/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument()
    })

    it('does not render segment when showTypeSegment is omitted (default)', async () => {
      renderWithProviders(<BoostsManagePage />)

      await waitFor(() => {
        expect(screen.getByText(/automation boosts/i)).toBeInTheDocument()
      })
      expect(screen.queryByRole('group', { name: /boost type filter/i })).not.toBeInTheDocument()
    })

    it('calls useGetBoosts with rule_type when default is Transfer & Savings', async () => {
      mockUseGetBoosts.mockClear()
      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => {
        expect(screen.getByRole('group', { name: /boost type filter/i })).toBeInTheDocument()
      })
      const lastCallParams = mockUseGetBoosts.mock.calls.at(-1)?.[0]
      expect(lastCallParams).toEqual({ rule_type: ['transfer_suggestion', 'savings_goal'] })
    })

    it('calls useGetBoosts without rule_type when All rules is selected', async () => {
      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument()
      })
      mockUseGetBoosts.mockClear()
      fireEvent.click(screen.getByRole('button', { name: /all boosts/i }))

      await waitFor(() => {
        const lastCallParams = mockUseGetBoosts.mock.calls.at(-1)?.[0]
        expect(lastCallParams).toBeUndefined()
      })
    })

    it('fires boosts_rules_filter_changed when segment is changed', async () => {
      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument()
      })
      mockTrackEvent.mockClear()
      fireEvent.click(screen.getByRole('button', { name: /all boosts/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'boosts_rules_filter_changed',
          expect.objectContaining({ filter: 'all' })
        )
      })
    })

    it('fires boosts_rules_filter_changed with filter:transfer_savings when switching back', async () => {
      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      // First switch to "All rules"
      await waitFor(() => expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument())
      fireEvent.click(screen.getByRole('button', { name: /all boosts/i }))

      mockTrackEvent.mockClear()
      // Then switch back to "Transfer & Savings"
      fireEvent.click(screen.getByRole('button', { name: /transfer & savings/i }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'boosts_rules_filter_changed',
          expect.objectContaining({ filter: 'transfer_savings' })
        )
      })
    })

    // R10-a (#1267): segment must remain reachable in the filtered-empty state.
    // Previously, the rules.length===0 early-return fired BEFORE the segment JSX
    // was rendered, so users with only budget_alert/category_rollover rules could
    // not click "All rules" to reach them from the Boosts tab.
    it('renders segment when showTypeSegment is true and the filtered query returns no rules', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })

      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => {
        expect(screen.getByRole('group', { name: /boost type filter/i })).toBeInTheDocument()
      })
      expect(screen.getByRole('button', { name: /transfer & savings/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument()
    })

    it('clicking All rules in filtered-empty state fires boosts_rules_filter_changed and clears rule_type', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })

      renderWithProviders(<BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />)

      await waitFor(() => expect(screen.getByRole('button', { name: /all boosts/i })).toBeInTheDocument())
      mockUseGetBoosts.mockClear()
      mockTrackEvent.mockClear()
      fireEvent.click(screen.getByRole('button', { name: /all boosts/i }))

      await waitFor(() => {
        const lastCallParams = mockUseGetBoosts.mock.calls.at(-1)?.[0]
        expect(lastCallParams).toBeUndefined()
        expect(mockTrackEvent).toHaveBeenCalledWith(
          'boosts_rules_filter_changed',
          expect.objectContaining({ filter: 'all' })
        )
      })
    })

    it('does NOT render segment in filtered-empty state when showTypeSegment is false', async () => {
      mockUseGetBoosts.mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })

      renderWithProviders(<BoostsManagePage />)

      await waitFor(() => {
        // The genuine empty state (no rules at all, no segment) renders
        expect(screen.getByText(/no automation rules yet/i)).toBeInTheDocument()
      })
      expect(screen.queryByRole('group', { name: /boost type filter/i })).not.toBeInTheDocument()
    })
  })

  // R9-c (#1267): Optimistic toggle cache-key regression test.
  // Verifies that onMutate writes to the FILTERED cache key when the segment is active,
  // and that onError rolls back exactly that same key. Prevents silent regressions where
  // a future edit might use the base (unfiltered) key instead.
  describe('Optimistic toggle cache-key (R9-c)', () => {
    it('onMutate writes to the filtered cache key and onError rolls it back', async () => {
      type OnMutateCallback = (vars: { id: string; data: { enabled: boolean } }) => Promise<{ previousRules: unknown }>
      type OnErrorCallback = (err: unknown, vars: unknown, ctx: { previousRules: unknown } | undefined) => void

      let capturedOnMutate: OnMutateCallback | undefined
      let capturedOnError: OnErrorCallback | undefined

      mockUseToggleBoost.mockImplementationOnce(
        (opts: { mutation?: { onMutate?: unknown; onError?: unknown } } | undefined) => {
          capturedOnMutate = opts?.mutation?.onMutate as OnMutateCallback
          capturedOnError = opts?.mutation?.onError as OnErrorCallback
          return {
            mutate: mockToggleBoostMutate,
            mutateAsync: mockToggleBoostMutateAsync,
            isPending: false,
            isError: false,
            isSuccess: false,
            error: null,
            data: null,
            reset: () => {},
          }
        }
      )

      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
      })

      const seedRule: Boost = {
        ...mockBoosts[0],
        id: 'seed-rule',
        boost_type: 'transfer_suggestion',
        enabled: false,
      }

      // Pre-seed the FILTERED cache key — this is the key the segment view reads.
      const filteredParams = { rule_type: ['transfer_suggestion', 'savings_goal'] }
      queryClient.setQueryData(getGetBoostsQueryKey(filteredParams), [seedRule])

      mockUseGetBoosts.mockReturnValueOnce({
        data: [seedRule],
        isLoading: false,
        error: null,
        refetch: mockRefetchBoosts,
      })

      render(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <BoostsManagePage showTypeSegment ruleTypeFilter={['transfer_suggestion', 'savings_goal']} />
          </MemoryRouter>
        </QueryClientProvider>
      )

      await waitFor(() => {
        expect(capturedOnMutate).toBeDefined()
        expect(capturedOnError).toBeDefined()
      })

      // (1) onMutate should optimistically flip `enabled` on the FILTERED key.
      const rollbackContext = await capturedOnMutate!({ id: 'seed-rule', data: { enabled: true } })

      const filteredAfterOptimistic = queryClient.getQueryData(
        getGetBoostsQueryKey(filteredParams)
      ) as Boost[]
      expect(filteredAfterOptimistic[0]?.enabled).toBe(true)

      // The unfiltered (base) key is untouched — onMutate must NOT write to the wrong key.
      expect(queryClient.getQueryData(getGetBoostsQueryKey())).toBeUndefined()

      // (2) onError should roll back the filtered key to its previous value.
      capturedOnError!(
        new Error('network error'),
        { id: 'seed-rule', data: { enabled: true } },
        rollbackContext
      )

      const filteredAfterRollback = queryClient.getQueryData(
        getGetBoostsQueryKey(filteredParams)
      ) as Boost[]
      expect(filteredAfterRollback[0]?.enabled).toBe(false)
    })
  })
})
