/**
 * RuleBuilder Component Tests
 * Issue #39: Rule Builder UI
 * Issue #575: Migrated to bun:test with centralized mocks
 *
 * Tests for the main rule builder form.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { RuleBuilder } from './RuleBuilder'
import type { AutomationRule } from '@boostedbudget/api-client/models'

// Import centralized mocks for configuration
import {
  mockUseGetAutomationRules,
  mockUseGetAutomationRuleByID,
  mockCreateAutomationRuleMutateAsync,
  mockUpdateAutomationRuleMutateAsync,
} from '@/test/mocks/automation-rules-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers } from '@/test/mocks/subscription-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockUseGetSavingsGoals } from '@/test/mocks/budgets-api'
import { mockNavigate } from '@/test/mocks/react-router'
import { mockUseBudgets } from '@/test/mocks/budgets-wrapper'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'

const mockRule: AutomationRule = {
  id: 'rule-1',
  user_id: 'user-1',
  name: 'Test Rule',
  description: 'Test description',
  rule_type: 'budget_alert',
  enabled: true,
  priority: 10,
  execution_count: 0,
  trigger_config: {
    type: 'schedule',
    cron: '0 9 * * *',
    timezone: 'America/New_York',
  },
  conditions: [],
  condition_logic: 'AND',
  actions: {
    notification_channels: ['in_app'],
    threshold_type: 'percentage',
    threshold_value: 80,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
} as any

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

function renderWithProviders(
  ui: React.ReactElement,
  { route = '/rules/new' } = {}
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <Routes>
          <Route path="/rules/new" element={ui} />
          <Route path="/rules/:id/edit" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('RuleBuilder', () => {
  beforeEach(() => {
    // Clear all mocks
    mockNavigate.mockClear()
    mockCreateAutomationRuleMutateAsync.mockClear()
    mockUpdateAutomationRuleMutateAsync.mockClear()
    mockUseGetAutomationRules.mockClear()
    mockUseGetAutomationRuleByID.mockClear()
    mockUseBudgets.mockClear()

    // Configure API mocks
    mockCreateAutomationRuleMutateAsync.mockResolvedValue(mockRule)
    mockUpdateAutomationRuleMutateAsync.mockResolvedValue(mockRule)

    mockUseGetAutomationRules.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    })

    mockUseGetAutomationRuleByID.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
    })

    mockUseGetUserProfile.mockReturnValue({
      data: mockUser,
      isLoading: false,
      error: null,
    })

    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
    })

    mockUseBudgets.mockReturnValue({
      budgets: [{ id: 'budget-1', name: 'Monthly Budget', categories: [] }],
      loading: false,
      error: null,
    })

    mockUseGetAccounts.mockReturnValue({
      data: [{ id: 'acc-1', name: 'Checking', type: 'depository' }],
      isLoading: false,
      error: null,
    })

    mockUseGetSavingsGoals.mockReturnValue({
      data: [{ id: 'goal-1', name: 'Emergency Fund', target_amount: 10000, current_amount: 2500 }],
      isLoading: false,
      error: null,
    })
  })

  describe('Rendering - Create Mode', () => {
    it('should render page title for new rule', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /create.*rule/i })).toBeInTheDocument()
      })
    })

    it('should render name input', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      })
    })

    it('should render description input', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      })
    })

    it('should render rule type selector', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      })
    })

    it('should show all rule type options', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/budget alert/i)).toBeInTheDocument()
      })
      expect(screen.getByText(/transfer suggestion/i)).toBeInTheDocument()
      expect(screen.getByText(/category rollover/i)).toBeInTheDocument()
      expect(screen.getByText(/savings goal/i)).toBeInTheDocument()
    })

    it('should render trigger section', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/trigger/i)).toBeInTheDocument()
      })
    })

    it('should render save and cancel buttons', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      })
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
    })
  })

  describe('Rule Type Selection', () => {
    it('should show budget alert action form when budget_alert selected', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/^budget$/i)).toBeInTheDocument()
      })
    })

    it('should show transfer action form when transfer_suggestion selected', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'transfer_suggestion' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
      })
    })

    it('should show rollover action form when category_rollover selected', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'category_rollover' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/source budget/i)).toBeInTheDocument()
      })
    })

    it('should show savings goal action form when savings_goal selected', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'savings_goal' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/savings goal/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form Validation', () => {
    it('should require name', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })
    })

    it('should require rule type', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/rule name/i)
      fireEvent.change(nameInput, { target: { value: 'Test Rule' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/rule type is required/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form Submission - Create', () => {
    it('should call createRule mutation on save', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/rule name/i)
      fireEvent.change(nameInput, { target: { value: 'New Test Rule' } })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockCreateAutomationRuleMutateAsync).toHaveBeenCalled()
      })
    })

    it('should navigate back after successful save', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/rule name/i)
      fireEvent.change(nameInput, { target: { value: 'New Test Rule' } })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith('/rules')
      })
    })
  })

  describe('Cancel', () => {
    it('should navigate back on cancel', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
      })

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(mockNavigate).toHaveBeenCalledWith('/rules')
    })
  })

  describe('Trigger Configuration', () => {
    it('should show schedule trigger by default', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/scheduled/i)).toBeInTheDocument()
      })
    })

    it('should allow switching to event trigger when permitted', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/event/i)).toBeInTheDocument()
      })

      const eventRadio = screen.getByLabelText(/event/i)
      fireEvent.click(eventRadio)

      await waitFor(() => {
        expect(eventRadio).toBeChecked()
      })
    })
  })

  describe('Priority', () => {
    it('should render priority input', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
      })
    })

    it('should default to priority 10', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        const priorityInput = screen.getByLabelText(/priority/i)
        expect(priorityInput).toHaveValue(10)
      })
    })

    it('should allow changing priority', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
      })

      const priorityInput = screen.getByLabelText(/priority/i)
      fireEvent.change(priorityInput, { target: { value: '5' } })

      expect(priorityInput).toHaveValue(5)
    })
  })

  describe('Accessibility', () => {
    it('should have accessible form structure', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('form')).toBeInTheDocument()
      })
    })

    it('should have accessible heading', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
      })
    })

    it('should have labeled inputs', async () => {
      renderWithProviders(<RuleBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      })
      expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
    })
  })
})
