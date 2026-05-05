import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { SavingsGoal } from './types'

import {
  mockUseGetSavingsGoals,
  mockUseGetActiveSavingsGoals,
  mockSavingsGoalsRefetch,
} from '@/test/mocks/savings-goals-api'

// ── Mock: analytics ──────────────────────────────────────────────────────────
const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

// ── Mock: accounts API (used by CreateSavingsGoalDialog) ─────────────────────
mock.module('@boostedbudget/api-client/endpoints/accounts/accounts', () => ({
  useGetAccounts: () => ({ data: { accounts: [] }, isLoading: false, error: null }),
}))

// ── Mock: ReadOnlyContext ─────────────────────────────────────────────────────
mock.module('@/auth/ReadOnlyContext', () => ({
  useReadOnly: () => ({ isReadOnly: false }),
}))

// ── Import component AFTER mocks ──────────────────────────────────────────────
const { SavingsPage } = await import('./SavingsPage')

// ── Helpers ───────────────────────────────────────────────────────────────────
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

// ── Tests ─────────────────────────────────────────────────────────────────────
describe('SavingsPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockReset()
    mockSavingsGoalsRefetch.mockClear()
    // Default: loaded, no goals
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
    // Default beforeEach sets data: []
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
})
