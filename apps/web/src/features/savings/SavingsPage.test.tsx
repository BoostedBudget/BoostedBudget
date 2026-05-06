// Uses preloaded API mocks from bunfig.toml — do NOT add mock.module() for
// savings-goals or accounts here; those are centralized in src/test/mocks/.

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { SavingsGoal } from './types'

import {
  mockUseGetSavingsGoals,
  mockUseGetActiveSavingsGoals,
  mockSavingsGoalsRefetch,
  mockDeleteSavingsGoalMutateAsync,
} from '@/test/mocks/savings-goals-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'

const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

mock.module('@/auth/ReadOnlyContext', () => ({
  useReadOnly: () => ({ isReadOnly: false }),
}))

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
    mockTrackEvent.mockReset()
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
  })
})
