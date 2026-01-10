/**
 * RulesPage Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the main rules list page.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { AutomationRule } from '@/api/generated/models'

const mockUseGetAutomationRules = vi.fn(() => ({
  data: [] as AutomationRule[],
  isLoading: false,
  error: null,
}))
const mockToggleRule = vi.fn()
const mockDeleteRule = vi.fn()
const mockTestRule = vi.fn()

vi.mock('@/api/generated/endpoints/automation-rules/automation-rules', () => ({
  useGetAutomationRules: () => mockUseGetAutomationRules(),
  useToggleAutomationRule: () => ({
    mutate: mockToggleRule,
    isPending: false,
  }),
  useDeleteAutomationRule: () => ({
    mutate: mockDeleteRule,
    isPending: false,
  }),
  useTestAutomationRule: () => ({
    mutate: mockTestRule,
    mutateAsync: mockTestRule,
    isPending: false,
    data: null,
    reset: vi.fn(),
  }),
}))

vi.mock('../hooks/useRuleTierLimits', () => ({
  useRuleTierLimits: () => ({
    ruleLimit: 5,
    isUnlimited: false,
    currentRuleCount: 2,
    isAtLimit: false,
    canUseEventTriggers: true,
    shouldShowUpgradeBanner: false,
    rulesCountDisplay: '2/5 rules used',
  }),
}))

vi.mock('@/features/profile/hooks/useProfile', () => ({
  useProfile: () => ({
    user: { subscription_tier: 'premium' },
    loading: false,
    error: null,
  }),
}))

vi.mock('@/features/subscription/hooks/useSubscriptionTiers', () => ({
  useSubscriptionTiers: () => ({
    tiers: {
      tiers: {
        free: { features: { max_rules: 1 } },
        premium_lite: { features: { max_rules: 5 } },
        premium: { features: { max_rules: 15 } },
        premium_plus: { features: { max_rules: -1 } },
      },
    },
    loading: false,
    error: null,
  }),
}))

import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { RulesPage } from './RulesPage'

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
    actions: {},
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-15T00:00:00Z',
    last_executed_at: '2024-01-15T09:00:00Z',
  },
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
    actions: {},
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-10T00:00:00Z',
  },
]

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = createQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('RulesPage', () => {
  beforeEach(() => {
    mockUseGetAutomationRules.mockClear()
    mockToggleRule.mockClear()
    mockDeleteRule.mockClear()
    mockTestRule.mockClear()
    localStorage.clear()
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: mockRules,
      isLoading: false,
      error: null,
    }))
  })

  it('should render page title', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByText(/automation rules/i)).toBeInTheDocument()
  })

  it('should render tier limit display', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByText(/2\/5 rules used/i)).toBeInTheDocument()
  })

  it('should render create button', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByRole('button', { name: /create.*rule/i })).toBeInTheDocument()
  })

  it('should render view toggle', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
  })

  it('should render rules list', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    expect(screen.getByText('Monthly Savings')).toBeInTheDocument()
  })

  it('should show loading state', () => {
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: null,
      isLoading: true,
      error: null,
    }))
    renderWithProviders(<RulesPage />)
    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('should show error state', () => {
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
    }))
    renderWithProviders(<RulesPage />)
    expect(screen.getByText(/error/i)).toBeInTheDocument()
  })

  it('should show retry button on error', () => {
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: null,
      isLoading: false,
      error: new Error('Failed to load'),
      refetch: vi.fn(),
    }))
    renderWithProviders(<RulesPage />)
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
  })

  it('should show empty state when no rules', () => {
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: [],
      isLoading: false,
      error: null,
    }))
    renderWithProviders(<RulesPage />)
    expect(screen.getByText(/no automation rules/i)).toBeInTheDocument()
  })

  it('should show create button in empty state', () => {
    mockUseGetAutomationRules.mockImplementation(() => ({
      data: [],
      isLoading: false,
      error: null,
    }))
    renderWithProviders(<RulesPage />)
    const createButtons = screen.getAllByRole('button', { name: /create/i })
    expect(createButtons.length).toBeGreaterThan(0)
  })

  it('should default to cards view', () => {
    renderWithProviders(<RulesPage />)
    const cardsButton = screen.getByRole('button', { name: /cards/i })
    expect(cardsButton).toHaveAttribute('aria-pressed', 'true')
  })

  it('should switch to table view when clicked', () => {
    renderWithProviders(<RulesPage />)
    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)
    expect(tableButton).toHaveAttribute('aria-pressed', 'true')
  })

  it('should show cards in cards view', () => {
    renderWithProviders(<RulesPage />)
    const cards = screen.getAllByRole('article')
    expect(cards.length).toBe(2)
  })

  it('should show table in table view', () => {
    renderWithProviders(<RulesPage />)
    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  it('should toggle rule when switch is clicked', async () => {
    renderWithProviders(<RulesPage />)
    const toggles = screen.getAllByRole('switch')
    fireEvent.click(toggles[0])
    expect(mockToggleRule).toHaveBeenCalled()
  })

  it('should open delete confirmation when delete is clicked', () => {
    renderWithProviders(<RulesPage />)
    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])
    expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
  })

  it('should call delete mutation when confirmed', async () => {
    renderWithProviders(<RulesPage />)
    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteButtons[0])
    const confirmButton = screen.getByRole('button', { name: /confirm/i })
    fireEvent.click(confirmButton)
    expect(mockDeleteRule).toHaveBeenCalled()
  })

  it('should have accessible page heading', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
  })

  it('should have accessible view toggle group', () => {
    renderWithProviders(<RulesPage />)
    expect(screen.getByRole('group', { name: /view toggle/i })).toBeInTheDocument()
  })

  it('should call test mutation when test button is clicked in cards view', () => {
    renderWithProviders(<RulesPage />)
    const testButtons = screen.getAllByRole('button', { name: /test/i })
    fireEvent.click(testButtons[0])
    expect(mockTestRule).toHaveBeenCalled()
  })

  it('should call test mutation when test button is clicked in table view', () => {
    renderWithProviders(<RulesPage />)
    const tableButton = screen.getByRole('button', { name: /table/i })
    fireEvent.click(tableButton)
    const testButtons = screen.getAllByRole('button', { name: /test/i })
    fireEvent.click(testButtons[0])
    expect(mockTestRule).toHaveBeenCalled()
  })
})
