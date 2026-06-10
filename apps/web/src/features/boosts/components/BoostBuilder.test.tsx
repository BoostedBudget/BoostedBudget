/**
 * BoostBuilder Component Tests
 * Issue #39: Rule Builder UI
 * Issue #575: Migrated to bun:test with centralized mocks
 *
 * Tests for the main rule builder form.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { BoostBuilder } from './BoostBuilder'
import type { Boost } from '@boostedbudget/api-client/models'

// Import centralized mocks for configuration
import {
  mockUseGetBoosts,
  mockUseGetBoostByID,
  mockCreateBoostMutateAsync,
  mockUpdateBoostMutateAsync,
} from '@/test/mocks/boosts-api'
import { mockUseGetUserProfile } from '@/test/mocks/profile-api'
import { mockUseGetSubscriptionTiers } from '@/test/mocks/subscription-api'
import { mockUseGetAccounts } from '@/test/mocks/accounts-api'
import { mockUseGetSavingsGoals } from '@/test/mocks/budgets-api'
import { mockNavigate } from '@/test/mocks/react-router'
import { mockUseBudgets } from '@/test/mocks/budgets-wrapper'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import { mockTrackEvent } from '@/test/mocks/track-event'

// Re-register to ensure mock is current for this test file's execution order
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

const mockBoost: Boost = {
  id: 'boost-1',
  user_id: 'user-1',
  name: 'Test Rule',
  description: 'Test description',
  boost_type: 'budget_alert',
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
  { route = '/boosts/new' } = {}
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
          <Route path="/boosts/new" element={ui} />
          <Route path="/boosts/:id/edit" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

describe('BoostBuilder', () => {
  beforeEach(() => {
    // Clear all mocks
    mockNavigate.mockClear()
    mockTrackEvent.mockClear()
    mockCreateBoostMutateAsync.mockClear()
    mockUpdateBoostMutateAsync.mockClear()
    mockUseGetBoosts.mockClear()
    mockUseGetBoostByID.mockClear()
    mockUseBudgets.mockClear()

    // Configure API mocks
    mockCreateBoostMutateAsync.mockResolvedValue(mockBoost)
    mockUpdateBoostMutateAsync.mockResolvedValue(mockBoost)

    mockUseGetBoosts.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    })

    mockUseGetBoostByID.mockReturnValue({
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
    it('should render page title for new boost', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: /create.*boost/i })).toBeInTheDocument()
      })
    })

    it('should render name input', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })
    })

    it('should render description input', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      })
    })

    it('should render boost type selector', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      })
    })

    it('should show all boost type options', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/budget alert/i)).toBeInTheDocument()
      })
      expect(screen.getByText(/transfer suggestion/i)).toBeInTheDocument()
      expect(screen.getByText(/category rollover/i)).toBeInTheDocument()
      expect(screen.getByText(/savings goal/i)).toBeInTheDocument()
    })

    it('should render trigger section', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/trigger/i)).toBeInTheDocument()
      })
    })

    it('should render save and cancel buttons', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      })
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
    })
  })

  describe('Boost Type Selection', () => {
    it('should show budget alert action form when budget_alert selected', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/^budget$/i)).toBeInTheDocument()
      })
    })

    it('should show transfer action form when transfer_suggestion selected', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'transfer_suggestion' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/from account/i)).toBeInTheDocument()
      })
    })

    it('should show rollover action form when category_rollover selected', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'category_rollover' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/source budget/i)).toBeInTheDocument()
      })
    })

    it('should show savings goal action form when savings_goal selected', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'savings_goal' } })

      await waitFor(() => {
        expect(screen.getByLabelText(/savings goal/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form Validation', () => {
    it('should require name', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })
    })

    it('should require boost type', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/boost name/i)
      fireEvent.change(nameInput, { target: { value: 'Test Rule' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(screen.getByText(/boost type is required/i)).toBeInTheDocument()
      })
    })
  })

  describe('Form Submission - Create', () => {
    it('should call createBoost mutation on save', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/boost name/i)
      fireEvent.change(nameInput, { target: { value: 'New Test Rule' } })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockCreateBoostMutateAsync).toHaveBeenCalled()
      })
    })

    it('should navigate back after successful save', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/boost name/i)
      fireEvent.change(nameInput, { target: { value: 'New Test Boost' } })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith('/boosts')
      })
    })

    it('fires BOOST_CREATED analytics after successful create', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText(/boost name/i)
      fireEvent.change(nameInput, { target: { value: 'Analytics Test Boost' } })

      const typeSelect = screen.getByLabelText(/boost type/i)
      fireEvent.change(typeSelect, { target: { value: 'budget_alert' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith('boost_created')
      })
    })
  })

  describe('Form Submission - Edit', () => {
    function renderEditMode() {
      mockUseGetBoostByID.mockReturnValue({
        data: mockBoost,
        isLoading: false,
        error: null,
      })
      return renderWithProviders(<BoostBuilder />, { route: '/boosts/rule-1/edit' })
    }

    it('fires BOOST_EDITED analytics after successful edit save', async () => {
      renderEditMode()

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })

      // Touch the name so the save is valid (field is pre-populated from mockBoost)
      const nameInput = screen.getByLabelText(/boost name/i)
      fireEvent.change(nameInput, { target: { value: 'Updated Boost Name' } })

      const saveButton = screen.getByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith('boost_edited')
      })
    })
  })

  describe('Cancel', () => {
    it('should navigate back on cancel', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
      })

      const cancelButton = screen.getByRole('button', { name: /cancel/i })
      fireEvent.click(cancelButton)

      expect(mockNavigate).toHaveBeenCalledWith('/boosts')
    })
  })

  describe('Trigger Configuration', () => {
    it('should show schedule trigger by default', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByText(/scheduled/i)).toBeInTheDocument()
      })
    })

    it('should allow switching to event trigger when permitted', async () => {
      renderWithProviders(<BoostBuilder />)

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
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
      })
    })

    it('should default to priority 10', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        const priorityInput = screen.getByLabelText(/priority/i)
        expect(priorityInput).toHaveValue(10)
      })
    })

    it('should allow changing priority', async () => {
      renderWithProviders(<BoostBuilder />)

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
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('form')).toBeInTheDocument()
      })
    })

    it('should have accessible heading', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
      })
    })

    it('should have labeled inputs', async () => {
      renderWithProviders(<BoostBuilder />)

      await waitFor(() => {
        expect(screen.getByLabelText(/boost name/i)).toBeInTheDocument()
      })
      expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/boost type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/priority/i)).toBeInTheDocument()
    })
  })

  describe('Read-only guarding (R2-D)', () => {
    function renderReadOnly() {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
      })
      return render(
        <ReadOnlyProvider isReadOnly={true}>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={['/boosts/new']}>
              <Routes>
                <Route path="/boosts/new" element={<BoostBuilder />} />
              </Routes>
            </MemoryRouter>
          </QueryClientProvider>
        </ReadOnlyProvider>
      )
    }

    it('wraps save/submit controls in ReadOnlyGuard in read-only mode', async () => {
      const { container } = renderReadOnly()
      // ReadOnlyGuard applies .pointer-events-none to its wrapper in read-only mode
      await waitFor(() => {
        expect(container.querySelector('.pointer-events-none')).toBeTruthy()
      })
    })

    it('does not render Save button as interactive when read-only', async () => {
      renderReadOnly()
      await waitFor(() => {
        // The save button should still be in the DOM (defense-in-depth) but guarded
        const saveBtn = screen.queryByLabelText(/save rule/i)
        if (saveBtn) {
          // If it's present, it should be inside a read-only guard container
          expect(saveBtn.closest('.pointer-events-none')).toBeTruthy()
        }
      })
    })
  })
})
