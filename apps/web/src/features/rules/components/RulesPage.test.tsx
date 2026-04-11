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
import type { AutomationRule } from '@boostedbudget/api-client/models'
import { RulesPage } from './RulesPage'

// Import centralized mocks for configuration
import {
  mockUseGetAutomationRules,
  mockToggleAutomationRuleMutate,
  mockDeleteAutomationRuleMutate,
  mockTestAutomationRuleMutate,
  mockRefetchAutomationRules,
} from '@/test/mocks/automation-rules-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers } from '@/test/mocks/subscription-api'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

const mockRules: AutomationRule[] = [
  {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Daily Budget Check',
    description: 'Check budget usage daily',
    rule_type: 'budget_alert',
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
  {
    id: 'rule-2',
    user_id: 'user-1',
    name: 'Monthly Savings',
    rule_type: 'savings_goal',
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
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

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('RulesPage', () => {
  beforeEach(() => {
    // Clear all mock call history
    mockUseGetAutomationRules.mockClear()
    mockToggleAutomationRuleMutate.mockClear()
    mockDeleteAutomationRuleMutate.mockClear()
    mockTestAutomationRuleMutate.mockClear()
    mockRefetchAutomationRules.mockClear()
    mockUseGetUserProfile.mockClear()
    mockUseGetSubscriptionTiers.mockClear()
    localStorage.clear()

    // Configure automation rules mock
    mockUseGetAutomationRules.mockReturnValue({
      data: mockRules,
      isLoading: false,
      error: null,
      refetch: mockRefetchAutomationRules,
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
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByText(/automation rules/i)).toBeInTheDocument()
    })
  })

  it('should render rule count display', async () => {
    renderWithProviders(<RulesPage />)

    // With 2 rules loaded, should show "2 rules"
    await waitFor(() => {
      expect(screen.getByText(/2 rules/i)).toBeInTheDocument()
    })
  })

  it('should render create button', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
    })
  })

  it('should render view toggle', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
    })
  })

  it('should render rules list', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })
    expect(screen.getByText('Monthly Savings')).toBeInTheDocument()
  })

  it('should show loading state', async () => {
    mockUseGetAutomationRules.mockReturnValue({
      data: null,
      isLoading: true,
      error: null,
      refetch: mockRefetchAutomationRules,
    })
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  it('should show error state', async () => {
    mockUseGetAutomationRules.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
      refetch: mockRefetchAutomationRules,
    })
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })
  })

  it('should show retry button on error', async () => {
    mockUseGetAutomationRules.mockReturnValue({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
      refetch: mockRefetchAutomationRules,
    })
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
    })
  })

  it('should show empty state when no rules', async () => {
    mockUseGetAutomationRules.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchAutomationRules,
    })
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByText(/no automation rules/i)).toBeInTheDocument()
    })
  })

  it('should show create button in empty state', async () => {
    mockUseGetAutomationRules.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      refetch: mockRefetchAutomationRules,
    })
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      const createButtons = screen.getAllByRole('button', { name: /create/i })
      expect(createButtons.length).toBeGreaterThan(0)
    })
  })

  it('should default to cards view', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      const cardsButton = screen.getByRole('button', { name: /cards/i })
      expect(cardsButton).toHaveAttribute('aria-pressed', 'true')
    })
  })

  it('should switch to table view when clicked', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /table/i })).toBeInTheDocument()
    })

    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)
    expect(tableButton).toHaveAttribute('aria-pressed', 'true')
  })

  it('should show cards in cards view', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      const cards = screen.getAllByRole('article')
      expect(cards.length).toBe(2)
    })
  })

  it('should show table in table view', async () => {
    renderWithProviders(<RulesPage />)

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
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getAllByRole('switch').length).toBeGreaterThan(0)
    })

    const toggles = screen.getAllByRole('switch')
    fireEvent.click(toggles[0])
    expect(mockToggleAutomationRuleMutate).toHaveBeenCalled()
  })

  it('should open delete confirmation when delete is clicked', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThan(0)
    })

    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])

    await waitFor(() => {
      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })
  })

  it('should call delete mutation when confirmed', async () => {
    renderWithProviders(<RulesPage />)

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
    expect(mockDeleteAutomationRuleMutate).toHaveBeenCalled()
  })

  it('should have accessible page heading', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
    })
  })

  it('should have accessible view toggle group', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
    })
  })

  it('should call test mutation when test button is clicked in cards view', async () => {
    renderWithProviders(<RulesPage />)

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /test/i }).length).toBeGreaterThan(0)
    })

    const testButtons = screen.getAllByRole('button', { name: /test/i })
    fireEvent.click(testButtons[0])
    expect(mockTestAutomationRuleMutate).toHaveBeenCalled()
  })

  it('should call test mutation when test button is clicked in table view', async () => {
    renderWithProviders(<RulesPage />)

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
    expect(mockTestAutomationRuleMutate).toHaveBeenCalled()
  })
})
