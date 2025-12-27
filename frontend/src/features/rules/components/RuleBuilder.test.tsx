/**
 * RuleBuilder Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the main rule builder form.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { RuleBuilder } from './RuleBuilder'
import type { AutomationRule } from '@/api/generated/models'

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
  },
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

// Mocks
const mockCreateRuleAsync = vi.fn()
const mockUpdateRuleAsync = vi.fn()
const mockNavigate = vi.fn()

vi.mock('@/api/generated/endpoints/automation-rules/automation-rules', () => ({
  useCreateAutomationRule: () => ({
    mutate: vi.fn(),
    mutateAsync: mockCreateRuleAsync.mockResolvedValue(mockRule),
    isPending: false,
  }),
  useUpdateAutomationRule: () => ({
    mutate: vi.fn(),
    mutateAsync: mockUpdateRuleAsync.mockResolvedValue(mockRule),
    isPending: false,
  }),
  useGetAutomationRuleByID: () => ({
    data: null,
    isLoading: false,
    error: null,
  }),
  useGetAutomationRules: () => ({
    data: [],
    isLoading: false,
    error: null,
  }),
}))

// Mock profile hook
vi.mock('@/features/profile/hooks/useProfile', () => ({
  useProfile: () => ({
    user: { subscription_tier: 'premium' },
    loading: false,
    error: null,
  }),
}))

// Mock subscription tiers hook
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

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('../hooks/useRuleTierLimits', () => ({
  useRuleTierLimits: () => ({
    ruleLimit: 5,
    isUnlimited: false,
    currentRuleCount: 1,
    isAtLimit: false,
    canUseEventTriggers: true,
    shouldShowUpgradeBanner: false,
    rulesCountDisplay: '1/5 rules used',
  }),
}))

// Mock budgets hook
vi.mock('@/features/budgets/hooks/useBudgets', () => ({
  useBudgets: () => ({
    budgets: [
      { id: 'budget-1', name: 'Monthly Budget', categories: [] },
    ],
    loading: false,
    error: null,
  }),
}))

// Mock accounts hook
vi.mock('@/api/generated/endpoints/accounts/accounts', () => ({
  useGetAccounts: () => ({
    data: [
      { id: 'acc-1', name: 'Checking', type: 'depository' },
    ],
    isLoading: false,
    error: null,
  }),
}))

// Mock savings goals hook
vi.mock('@/api/generated/endpoints/budgets/budgets', () => ({
  useGetSavingsGoals: () => ({
    data: [
      { id: 'goal-1', name: 'Emergency Fund', target_amount: 10000, current_amount: 2500 },
    ],
    isLoading: false,
    error: null,
  }),
}))

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(
  ui: React.ReactElement,
  { route = '/rules/new' } = {}
) {
  const queryClient = createQueryClient()
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
    vi.clearAllMocks()
  })

  describe('Rendering - Create Mode', () => {
    it('should render page title for new rule', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByRole('heading', { name: /create.*rule/i })).toBeInTheDocument()
    })

    it('should render name input', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
    })

    it('should render description input', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
    })

    it('should render rule type selector', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
    })

    it('should show all rule type options', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByText(/budget alert/i)).toBeInTheDocument()
      expect(screen.getByText(/transfer suggestion/i)).toBeInTheDocument()
      expect(screen.getByText(/category rollover/i)).toBeInTheDocument()
      expect(screen.getByText(/savings goal/i)).toBeInTheDocument()
    })

    it('should render trigger section', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByText(/trigger/i)).toBeInTheDocument()
    })

    it('should render save and cancel buttons', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
    })
  })

  describe('Rule Type Selection', () => {
    it('should show budget alert action form when budget_alert selected', async () => {
      renderWithProviders(<RuleBuilder />)

      // Select budget alert
      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/^budget$/i)).toBeInTheDocument()
      })
    })

    it('should show transfer action form when transfer_suggestion selected', async () => {
      renderWithProviders(<RuleBuilder />)

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'transfer_suggestion' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
      })
    })

    it('should show rollover action form when category_rollover selected', async () => {
      renderWithProviders(<RuleBuilder />)

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'category_rollover' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/source budget/i)).toBeInTheDocument()
      })
    })

    it('should show savings goal action form when savings_goal selected', async () => {
      renderWithProviders(<RuleBuilder />)

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

      // Try to save without name
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })
    })

    it('should require rule type', async () => {
      renderWithProviders(<RuleBuilder />)

      // Fill name but not type
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

      // Fill form
      const nameInput = screen.getByLabelText(/rule name/i)
      fireEvent.change(nameInput, { target: { value: 'New Test Rule' } })

      const typeSelect = screen.getByLabelText(/rule type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      // Save
      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockCreateRuleAsync).toHaveBeenCalled()
      })
    })

    it('should navigate back after successful save', async () => {
      renderWithProviders(<RuleBuilder />)

      // Fill minimum required fields
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
    it('should navigate back on cancel', () => {
      renderWithProviders(<RuleBuilder />)

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(mockNavigate).toHaveBeenCalledWith('/rules')
    })
  })

  describe('Trigger Configuration', () => {
    it('should show schedule trigger by default', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByText(/scheduled/i)).toBeInTheDocument()
    })

    it('should allow switching to event trigger when permitted', async () => {
      renderWithProviders(<RuleBuilder />)

      const eventRadio = screen.getByLabelText(/event/i)
      fireEvent.click(eventRadio)

      await waitFor(() => {
        expect(eventRadio).toBeChecked()
      })
    })
  })

  describe('Priority', () => {
    it('should render priority input', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
    })

    it('should default to priority 10', () => {
      renderWithProviders(<RuleBuilder />)

      const priorityInput = screen.getByLabelText(/priority/i)
      expect(priorityInput).toHaveValue(10)
    })

    it('should allow changing priority', () => {
      renderWithProviders(<RuleBuilder />)

      const priorityInput = screen.getByLabelText(/priority/i)
      fireEvent.change(priorityInput, { target: { value: '5' } })

      expect(priorityInput).toHaveValue(5)
    })
  })

  describe('Accessibility', () => {
    it('should have accessible form structure', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByRole('form')).toBeInTheDocument()
    })

    it('should have accessible heading', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
    })

    it('should have labeled inputs', () => {
      renderWithProviders(<RuleBuilder />)

      expect(screen.getByLabelText(/rule name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/rule type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
    })
  })
})
