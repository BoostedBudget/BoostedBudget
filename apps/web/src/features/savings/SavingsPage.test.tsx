// Uses preloaded API mocks from bunfig.toml — do NOT add mock.module() for
// savings-goals or accounts here; those are centralized in src/test/mocks/.

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { SavingsGoal } from './types'

import {
  mockUseGetSavingsGoals,
  mockUseGetActiveSavingsGoals,
  mockSavingsGoalsRefetch,
  mockDeleteSavingsGoalMutateAsync,
  mockUseListSavingsGoalContributions,
  mockConfirmSavingsGoalContributionMutateAsync,
  mockUseGetSavingsProgressSummary,
  mockUseGetDeletedSavingsGoals,
  mockDeletedSavingsGoalsRefetch,
} from '@/test/mocks/savings-goals-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockTrackEvent } from '@/test/mocks/track-event'

// IMPORTANT: Do NOT add mock.module('@/auth/ReadOnlyContext', ...) here.
// It is process-global in bun and corrupts ReadOnlyGuard for other test files
// that run in the same worker (see CLAUDE.md feedback_bun_test_mock_module_global).
// The real ReadOnlyContext default (isReadOnly: false) is correct for SavingsPage tests.
const { SavingsPage } = await import('./SavingsPage')
function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>{children}</MemoryRouter>
      </QueryClientProvider>
    )
  }
}

function renderPage() {
  return render(<SavingsPage />, { wrapper: createWrapper() })
}

const baseGoal: SavingsGoal = {
  id: 'goal-1',
  user_id: 'user-1',
  name: 'Vacation Fund',
  description: 'Summer trip',
  target_amount: 5000,
  current_amount: 2500,
  currency_code: 'USD',
  account_id: 'acct-1',
  is_completed: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

const completedGoal: SavingsGoal = {
  ...baseGoal,
  id: 'goal-2',
  name: 'Emergency Fund',
  is_completed: true,
  current_amount: 5000,
}

describe('SavingsPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
    mockSavingsGoalsRefetch.mockClear()
    mockDeleteSavingsGoalMutateAsync.mockClear()
    mockUseGetAccounts.mockReturnValue({
      data: { accounts: [] },
      isLoading: false,
      error: null,
    })
    mockUseGetSavingsGoals.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    mockUseGetActiveSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    mockUseGetSavingsProgressSummary.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
    })
    mockUseGetDeletedSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: null,
      refetch: mockDeletedSavingsGoalsRefetch,
    })
  })

  it('renders page heading', () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: [baseGoal],
      isLoading: false,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    renderPage()
    expect(screen.getByRole('heading', { level: 1 })).toBeDefined()
    expect(screen.getByText('Savings')).toBeDefined()
  })

  it('fires savings_page_viewed on mount', async () => {
    renderPage()
    await act(async () => {})
    expect(mockTrackEvent).toHaveBeenCalledWith('savings_page_viewed')
  })

  it('renders loading state', () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    renderPage()
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('renders error state with retry button', () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network failure'),
      refetch: mockSavingsGoalsRefetch,
    })
    renderPage()
    expect(screen.getByText(/network failure/i)).toBeDefined()
    expect(screen.getByRole('button', { name: /retry/i })).toBeDefined()
  })

  // #1569 R8 layer 2. The live-goals error branch returned before <DeletedGoalsPanel>, so a
  // transient failure on an unrelated query hid the deleted-goal ledger AND its purge verb —
  // the same shape as the Critical this surface was built to fix (an empty state rendering
  // before the panel that made its own promise true). The live-list error must stay visible.
  it('keeps the deleted-goals panel reachable when the live goals query fails', () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('Network failure'),
      refetch: mockSavingsGoalsRefetch,
    } as unknown as ReturnType<typeof mockUseGetSavingsGoals>)
    mockUseGetDeletedSavingsGoals.mockReturnValue({
      data: [
        {
          ...baseGoal,
          id: 'goal-deleted',
          name: 'Retired Fund',
          deleted_at: '2024-02-01T00:00:00Z',
          progress_basis: 'contributions',
        },
      ],
      isLoading: false,
      error: null,
      refetch: mockDeletedSavingsGoalsRefetch,
    } as unknown as ReturnType<typeof mockUseGetDeletedSavingsGoals>)
    renderPage()

    // The live-list failure is NOT swallowed.
    expect(screen.getByText(/network failure/i)).toBeDefined()
    // ...and the retained history survives it.
    fireEvent.click(screen.getByRole('button', { name: /deleted goals/i }))
    expect(screen.getByText('Retired Fund')).toBeDefined()
  })

  it('renders empty state when no goals exist', () => {
    renderPage()
    expect(screen.getByText(/no savings goals/i)).toBeDefined()
  })

  it('renders goal card when goals are present', () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: [baseGoal],
      isLoading: false,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    renderPage()
    expect(screen.getByText('Vacation Fund')).toBeDefined()
  })

  it('opens create dialog when New goal button is clicked', async () => {
    mockUseGetSavingsGoals.mockReturnValue({
      data: [baseGoal],
      isLoading: false,
      error: null,
      refetch: mockSavingsGoalsRefetch,
    })
    renderPage()
    const newGoalButton = screen.getByRole('button', { name: /new goal/i })
    await act(async () => {
      newGoalButton.click()
    })
    expect(screen.getByText(/create savings goal/i)).toBeDefined()
  })

  describe('SavingsPage — flows', () => {
    it('empty-state CTA button opens the create dialog', async () => {
      const user = userEvent.setup()
      renderPage()
      const ctaButton = screen.getByRole('button', { name: /create goal/i })
      await user.click(ctaButton)
      expect(screen.getByText('Create Savings Goal')).toBeDefined()
    })

    it('create dialog Cancel closes without calling refetch', async () => {
      const user = userEvent.setup()
      renderPage()
      const ctaButton = screen.getByRole('button', { name: /create goal/i })
      await user.click(ctaButton)
      expect(screen.getByText('Create Savings Goal')).toBeDefined()

      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      await user.click(cancelButton)

      expect(screen.queryByText('Create Savings Goal')).toBeNull()
      expect(mockSavingsGoalsRefetch).not.toHaveBeenCalled()
    })

    it('renders active and completed goals (both visible)', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal, completedGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      // switch to All tab to see both
      const allTab = screen.getByRole('tab', { name: /all/i })
      await user.click(allTab)
      expect(screen.getByText('Vacation Fund')).toBeDefined()
      expect(screen.getByText('Emergency Fund')).toBeDefined()
    })

    it('hides "New goal" header button when no goals exist (empty state only shows CTA)', () => {
      renderPage()
      // Header "New goal" only renders in the non-empty branch
      expect(screen.queryByRole('button', { name: /new goal/i })).toBeNull()
      // Empty-state CTA should be present
      expect(screen.getByRole('button', { name: /create goal/i })).toBeDefined()
    })

    it('shows "New goal" header button when only completed goals exist', () => {
      mockUseGetSavingsGoals.mockReturnValue({
        data: [completedGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      expect(screen.getByRole('button', { name: /new goal/i })).toBeDefined()
    })

    // #1569 T8.5 folded the copy assertions in here rather than adding an `it` of their own: every
    // test in this file re-stubs mockUseGetSavingsGoals, and each such stub costs one entry in the
    // typecheck-tests ratchet, which must not move for a copy fix.
    //
    // The POSITIVE half alone would be worthless: the previous, FALSE copy ("Nothing is thrown
    // away — every contribution you logged is kept") also contained "kept" and "Deleted goals", so
    // a reviewer could restore it with a positive-only test still green. The NEGATIVE assertions
    // are the load-bearing half. They are false because an explicitly deleted contribution is HARD
    // deleted (savings_contribution_repository.DeleteContribution).
    it('opens delete confirmation dialog when Delete is clicked on a goal card', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const deleteButton = screen.getByRole('button', { name: /delete goal/i })
      await user.click(deleteButton)
      expect(screen.getByRole('alertdialog')).toBeDefined()
      expect(screen.getByText(/delete savings goal/i)).toBeDefined()

      const text = screen.getByRole('alertdialog').textContent ?? ''
      expect(text).toMatch(/on its own/i)
      expect(text).not.toMatch(/nothing is thrown away/i)
      expect(text).not.toMatch(/every contribution you logged is kept/i)
    })

    it('cancel on delete dialog closes it without calling deleteGoal', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const deleteButton = screen.getByRole('button', { name: /delete goal/i })
      await user.click(deleteButton)

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      await user.click(cancelButton)

      expect(screen.queryByRole('alertdialog')).toBeNull()
      expect(mockDeleteSavingsGoalMutateAsync).not.toHaveBeenCalled()
    })

    it('confirm delete calls deleteGoal with goal id', async () => {
      const user = userEvent.setup()
      mockDeleteSavingsGoalMutateAsync.mockResolvedValueOnce(undefined)
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const deleteButton = screen.getByRole('button', { name: /delete goal/i })
      await user.click(deleteButton)

      const confirmButton = screen.getByRole('button', { name: /^delete$/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockDeleteSavingsGoalMutateAsync).toHaveBeenCalledWith({ id: 'goal-1' })
      })
    })

    it('fires SAVINGS_GOAL_DELETED analytics after confirm delete', async () => {
      const user = userEvent.setup()
      mockDeleteSavingsGoalMutateAsync.mockResolvedValueOnce(undefined)
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const deleteButton = screen.getByRole('button', { name: /delete goal/i })
      await user.click(deleteButton)

      const confirmButton = screen.getByRole('button', { name: /^delete$/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith('savings_goal_deleted')
      })
    })

    it('can delete a completed goal', async () => {
      const user = userEvent.setup()
      mockDeleteSavingsGoalMutateAsync.mockResolvedValueOnce(undefined)
      mockUseGetSavingsGoals.mockReturnValue({
        data: [completedGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const completedTab = screen.getByRole('tab', { name: /completed/i })
      await user.click(completedTab)

      const deleteButton = screen.getByRole('button', { name: /delete goal/i })
      await user.click(deleteButton)

      const confirmButton = screen.getByRole('button', { name: /^delete$/i })
      await user.click(confirmButton)

      await waitFor(() => {
        expect(mockDeleteSavingsGoalMutateAsync).toHaveBeenCalledWith({ id: 'goal-2' })
      })
    })

    it('fires SAVINGS_FILTER_CHANGED when tab changes', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const completedTab = screen.getByRole('tab', { name: /completed/i })
      await user.click(completedTab)

      expect(mockTrackEvent).toHaveBeenCalledWith('savings_filter_changed', { tab: 'completed' })
    })

    it('fires SAVINGS_SORT_CHANGED when sort changes', () => {
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const sortSelect = screen.getByRole('combobox', { name: /sort goals/i })
      fireEvent.change(sortSelect, { target: { value: 'deadline_asc' } })

      expect(mockTrackEvent).toHaveBeenCalledWith('savings_sort_changed', { sort: 'deadline_asc' })
    })

    it('clicking Retry in error state calls refetch', async () => {
      mockUseGetSavingsGoals.mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Network failure'),
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const retryButton = screen.getByRole('button', { name: /retry/i })
      await act(async () => {
        retryButton.click()
      })
      expect(mockSavingsGoalsRefetch).toHaveBeenCalledTimes(1)
    })

    it('auto-opens create dialog when navigated with location.state.openCreateDialog', async () => {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      render(<SavingsPage />, {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={[{ pathname: '/savings', state: { openCreateDialog: true } }]}>
              {children}
            </MemoryRouter>
          </QueryClientProvider>
        ),
      })
      await act(async () => {})
      expect(screen.getByText('Create Savings Goal')).toBeDefined()
    })

    it('clicking edit on a goal card opens the edit dialog', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const editButton = screen.getByRole('button', { name: /edit goal/i })
      await user.click(editButton)
      expect(screen.getByText('Edit Savings Goal')).toBeDefined()
    })

    it('cancel on edit dialog closes it without calling refetch', async () => {
      const user = userEvent.setup()
      mockUseGetSavingsGoals.mockReturnValue({
        data: [baseGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      renderPage()
      const editButton = screen.getByRole('button', { name: /edit goal/i })
      await user.click(editButton)
      expect(screen.getByText('Edit Savings Goal')).toBeDefined()

      const cancelButton = screen.getByRole('button', { name: /^cancel$/i })
      await user.click(cancelButton)

      expect(screen.queryByText('Edit Savings Goal')).toBeNull()
      expect(mockSavingsGoalsRefetch).not.toHaveBeenCalled()
    })

    // R4-T3: Prove the R3-5 wiring at the page level.
    // SavingsPage passes onContributionChanged={() => void refetch()} → SavingsGoalsGrid →
    // SavingsGoalCard → GoalContributionsSheet → handleChanged → onContributionChanged.
    // After any contribution action, the page's goal-list refetch must fire.
    it('R4-T3: contribution action in sheet triggers page goal-list refetch (R3-5 wiring)', async () => {
      const user = userEvent.setup()

      // contributions-basis goal — the "Contributions" button is only visible for this basis.
      const contribGoal: SavingsGoal = {
        ...baseGoal,
        id: 'goal-contrib',
        progress_basis: 'contributions',
      }
      mockUseGetSavingsGoals.mockReturnValue({
        data: [contribGoal],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })

      // Sheet lists one proposed contribution → Confirm button is rendered.
      mockUseListSavingsGoalContributions.mockReturnValue({
        data: [
          {
            id: 'c-proposed-page-test',
            savings_goal_id: 'goal-contrib',
            amount: 100,
            source: 'sync',
            status: 'proposed',
            confirmed_by_user: false,
            managed_by_reconciliation: true,
            occurred_on: '2026-06-10',
            created_at: '2026-06-10T00:00:00Z',
            updated_at: '2026-06-10T00:00:00Z',
          },
        ],
        isLoading: false,
        error: null,
        refetch: mock(() => Promise.resolve({})),
      })

      // Confirm mutation resolves successfully so the success path runs.
      mockConfirmSavingsGoalContributionMutateAsync.mockResolvedValueOnce({
        id: 'c-proposed-page-test',
        savings_goal_id: 'goal-contrib',
        amount: 100,
        source: 'sync',
        status: 'confirmed',
        confirmed_by_user: true,
        managed_by_reconciliation: true,
        occurred_on: '2026-06-10',
        created_at: '2026-06-10T00:00:00Z',
        updated_at: '2026-06-10T00:00:00Z',
      })

      renderPage()

      // Open the contributions sheet.
      const viewContribButton = screen.getByRole('button', { name: /view contributions/i })
      await user.click(viewContribButton)

      // Confirm the proposed contribution.
      const confirmButton = await screen.findByRole('button', { name: /confirm/i })
      await user.click(confirmButton)

      // Page-level refetch must fire: handleChanged → onContributionChanged →
      // SavingsPage's `() => void refetch()` → mockSavingsGoalsRefetch.
      await waitFor(() => {
        expect(mockSavingsGoalsRefetch).toHaveBeenCalled()
      })
    })
  })

  // Issue #1569 (Task 13, E14). Before this, `freshness` had no frontend consumer at all, so a
  // goal whose linked account was gone showed a frozen number the UI could not explain.
  describe('frozen-amount explanation from the progress summary', () => {
    function renderWithSummary(goals: SavingsGoal[], summaryGoals: unknown[] | undefined) {
      mockUseGetSavingsGoals.mockReturnValue({
        data: goals,
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      mockUseGetSavingsProgressSummary.mockReturnValue({
        data: summaryGoals === undefined ? undefined : { goals: summaryGoals },
        isLoading: false,
        error: null,
      })
      return renderPage()
    }

    it('explains the frozen amount on the goal whose basis account is disconnected', () => {
      renderWithSummary([baseGoal], [
        { goal_id: baseGoal.id, freshness: 'basis_account_disconnected' },
      ])
      expect(screen.getByText('Account disconnected')).toBeInTheDocument()
    })

    it('leaves a healthy goal unannotated', () => {
      renderWithSummary([baseGoal], [{ goal_id: baseGoal.id, freshness: 'fresh' }])
      expect(screen.queryByText('Account disconnected')).not.toBeInTheDocument()
    })

    it('annotates only the affected goal when several are shown', () => {
      const otherGoal: SavingsGoal = { ...baseGoal, id: 'goal-9', name: 'House Deposit' }
      renderWithSummary([baseGoal, otherGoal], [
        { goal_id: baseGoal.id, freshness: 'basis_account_disconnected' },
        { goal_id: otherGoal.id, freshness: 'fresh' },
      ])
      expect(screen.getAllByText('Account disconnected')).toHaveLength(1)
    })

    it('renders normally when the summary is unavailable', () => {
      // The summary is a secondary signal — if it fails or has not loaded, the goals list must
      // still render rather than blocking on it.
      renderWithSummary([baseGoal], undefined)
      expect(screen.getByText('Vacation Fund')).toBeInTheDocument()
      expect(screen.queryByText('Account disconnected')).not.toBeInTheDocument()
    })
  })

  // Issue #1569 whole-branch review, C2. The delete confirmation promises the contributions are
  // kept and that the goal can be erased later "from Deleted goals". A user deleting their LAST
  // goal landed on the zero-goals empty state, which returned before <DeletedGoalsPanel> was
  // ever reached — so both halves of that promise were false exactly for the user who had just
  // been told them, and POST /savings-goals/{id}/purge had no reachable caller again.
  describe('deleted-goals disclosure with no live goals remaining', () => {
    const deletedGoal = {
      ...baseGoal,
      id: 'goal-deleted-1',
      name: 'Old Laptop Fund',
      deleted_at: '2024-03-04T00:00:00Z',
    }

    function withDeleted(goals: unknown[], deleted: unknown[]) {
      mockUseGetSavingsGoals.mockReturnValue({
        data: goals as SavingsGoal[],
        isLoading: false,
        error: null,
        refetch: mockSavingsGoalsRefetch,
      })
      mockUseGetDeletedSavingsGoals.mockReturnValue({
        data: deleted as never,
        isLoading: false,
        error: null,
        refetch: mockDeletedSavingsGoalsRefetch,
      })
      return renderPage()
    }

    it('renders the deleted-goals panel alongside the zero-goals empty state', () => {
      withDeleted([], [deletedGoal])
      // Both, together: the empty state is still correct (there are no live goals), and the
      // panel must not be traded away for it.
      expect(screen.getByText(/no savings goals yet/i)).toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: /deleted goals \(1\)/i }),
      ).toBeInTheDocument()
    })

    it('exposes the purge verb from the zero-live-goals page', async () => {
      const user = userEvent.setup()
      withDeleted([], [deletedGoal])
      await user.click(screen.getByRole('button', { name: /deleted goals \(1\)/i }))
      // Named by goal so it is distinguishable in a screen reader's element list.
      expect(
        screen.getByRole('button', { name: /delete permanently: old laptop fund/i }),
      ).toBeInTheDocument()
    })

    it('still shows nothing when there are neither live nor deleted goals', () => {
      // The panel self-hides on an empty listing, so hoisting it must not introduce an empty
      // "Deleted goals (0)" header onto the first-run page.
      withDeleted([], [])
      expect(screen.getByText(/no savings goals yet/i)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /deleted goals/i })).toBeNull()
    })
  })
})
