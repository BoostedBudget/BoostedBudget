import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { SavingsGoal } from './types'
import type { SavingsGoalWithProgress } from './hooks/useSavingsGoals'

// ── Mock: analytics ──────────────────────────────────────────────────────────
const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

// ── Mock: useSavingsGoals ────────────────────────────────────────────────────
const mockRefetch = mock(() => Promise.resolve())
const mockDeleteGoal = mock(() => Promise.resolve())
const mockUseSavingsGoals = mock(() => ({
  goals: [] as SavingsGoal[],
  goalsWithProgress: [] as SavingsGoalWithProgress[],
  loading: false,
  error: null as Error | null,
  totalTarget: 0,
  totalSaved: 0,
  refetch: mockRefetch,
  createGoal: mock(() => Promise.resolve()),
  updateGoal: mock(() => Promise.resolve()),
  deleteGoal: mockDeleteGoal,
}))

mock.module('@/features/savings/hooks/useSavingsGoals', () => ({
  useSavingsGoals: () => mockUseSavingsGoals(),
}))

// ── Mock: accounts API (used by CreateSavingsGoalDialog) ─────────────────────
mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  useGetAccounts: () => ({ data: { accounts: [] }, isLoading: false, error: null }),
}))

// ── Mock: savings-goals API (used by Create/UpdateSavingsGoalDialog) ─────────
mock.module('@boostedbudget/api-client/endpoints/savings-goals/savings-goals', () => ({
  useCreateSavingsGoal: () => ({ mutateAsync: mock(() => Promise.resolve({})), isPending: false }),
  useUpdateSavingsGoal: () => ({ mutateAsync: mock(() => Promise.resolve({})), isPending: false }),
  useDeleteSavingsGoal: () => ({ mutateAsync: mock(() => Promise.resolve()), isPending: false }),
  useGetSavingsGoals: () => ({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) }),
  useGetActiveSavingsGoals: () => ({ data: undefined, isLoading: false, error: null, refetch: mock(() => Promise.resolve()) }),
  getGetSavingsGoalsQueryKey: () => ['savings-goals'],
  getGetActiveSavingsGoalsQueryKey: () => ['savings-goals', 'active'],
}))

// ── Mock: ReadOnlyContext ─────────────────────────────────────────────────────
mock.module('@/auth/ReadOnlyContext', () => ({
  useReadOnly: () => ({ isReadOnly: false }),
}))

// ── Import component AFTER mocks ──────────────────────────────────────────────
const { SavingsPage } = await import('./SavingsPage')

// ── Helpers ───────────────────────────────────────────────────────────────────
function renderPage() {
  return render(
    <MemoryRouter>
      <SavingsPage />
    </MemoryRouter>,
  )
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

const baseGoalWithProgress: SavingsGoalWithProgress = { ...baseGoal, progress: 50 }

// ── Tests ─────────────────────────────────────────────────────────────────────
describe('SavingsPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockReset()
    mockRefetch.mockReset()
    mockDeleteGoal.mockReset()
    // Default: loaded, no goals
    mockUseSavingsGoals.mockReturnValue({
      goals: [],
      goalsWithProgress: [],
      loading: false,
      error: null,
      totalTarget: 0,
      totalSaved: 0,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
    })
  })

  it('renders page heading', () => {
    mockUseSavingsGoals.mockReturnValue({
      goals: [baseGoal],
      goalsWithProgress: [baseGoalWithProgress],
      loading: false,
      error: null,
      totalTarget: 5000,
      totalSaved: 2500,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
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
    mockUseSavingsGoals.mockReturnValue({
      goals: [],
      goalsWithProgress: [],
      loading: true,
      error: null,
      totalTarget: 0,
      totalSaved: 0,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
    })
    renderPage()
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('renders error state with retry button', () => {
    mockUseSavingsGoals.mockReturnValue({
      goals: [],
      goalsWithProgress: [],
      loading: false,
      error: new Error('Network failure'),
      totalTarget: 0,
      totalSaved: 0,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
    })
    renderPage()
    expect(screen.getByText(/network failure/i)).toBeDefined()
    expect(screen.getByRole('button', { name: /retry/i })).toBeDefined()
  })

  it('renders empty state when no goals exist', () => {
    // Default beforeEach sets goals: []
    renderPage()
    expect(screen.getByText(/no savings goals/i)).toBeDefined()
  })

  it('renders goal card when goals are present', () => {
    mockUseSavingsGoals.mockReturnValue({
      goals: [baseGoal],
      goalsWithProgress: [baseGoalWithProgress],
      loading: false,
      error: null,
      totalTarget: 5000,
      totalSaved: 2500,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
    })
    renderPage()
    expect(screen.getByText('Vacation Fund')).toBeDefined()
  })

  it('opens create dialog when New goal button is clicked', async () => {
    mockUseSavingsGoals.mockReturnValue({
      goals: [baseGoal],
      goalsWithProgress: [baseGoalWithProgress],
      loading: false,
      error: null,
      totalTarget: 5000,
      totalSaved: 2500,
      refetch: mockRefetch,
      createGoal: mock(() => Promise.resolve()),
      updateGoal: mock(() => Promise.resolve()),
      deleteGoal: mockDeleteGoal,
    })
    renderPage()
    const newGoalButton = screen.getByRole('button', { name: /new goal/i })
    await act(async () => {
      newGoalButton.click()
    })
    expect(screen.getByText(/create savings goal/i)).toBeDefined()
  })
})
