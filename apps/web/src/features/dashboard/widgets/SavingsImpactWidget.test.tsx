/**
 * SavingsImpactWidget Tests
 * Issue #972: Savings Transfer Automation — dashboard widget
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import type { UserSavingsImpact } from '@boostedbudget/api-client/models'

// Use centralized mocks — no inline mock.module()
import '@/test/mocks/registry'
import {
  mockUseDashboardContext,
  mockSetFilters,
  mockSetTimeframe,
  mockSetIsEditing,
} from '@/test/mocks/dashboard-context'
import {
  mockUseSavingsImpactData,
} from '@/test/mocks/savings-impact-hooks'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockPosthogCapture } from '@/test/mocks/posthog'

const { SavingsImpactWidget } = await import('./SavingsImpactWidget')

function renderWidget() {
  return render(
    <MemoryRouter>
      <SavingsImpactWidget widgetId="test-savings-impact" onRemove={() => {}} />
    </MemoryRouter>
  )
}

describe('SavingsImpactWidget', () => {
  const mockData: UserSavingsImpact = {
    total_amount: 1250.75,
    completed_count: 5,
    month_amount: 350.00,
    prev_month_amount: 200.00,
    recent_items: [],
  }

  const defaultContextState = {
    filters: {
      startDate: '2024-01-01',
      endDate: '2024-01-31',
      accountTypes: undefined,
    },
    setFilters: mockSetFilters,
    timeframe: 'this_month' as const,
    setTimeframe: mockSetTimeframe,
    isEditing: false,
    setIsEditing: mockSetIsEditing,
    hasAccounts: true,
    accountsLoading: false,
  }

  const defaultDataState = {
    data: null,
    loading: false,
    error: null,
  }

  beforeEach(() => {
    mockUseDashboardContext.mockClear()
    mockUseSavingsImpactData.mockClear()
    mockSetFilters.mockClear()
    mockSetTimeframe.mockClear()
    mockSetIsEditing.mockClear()
    mockTrackEvent.mockClear()
    mockPosthogCapture.mockClear()

    mockUseDashboardContext.mockReturnValue(defaultContextState)
    mockUseSavingsImpactData.mockReturnValue(defaultDataState)
  })

  describe('Loading State', () => {
    it('shows loading spinner when loading', () => {
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, loading: true })
      renderWidget()
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('shows error message when an error occurs', () => {
      mockUseSavingsImpactData.mockReturnValue({
        ...defaultDataState,
        error: new Error('Network error'),
      })
      renderWidget()
      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('shows error text in destructive color', () => {
      mockUseSavingsImpactData.mockReturnValue({
        ...defaultDataState,
        error: new Error('API Error'),
      })
      renderWidget()
      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-destructive')
    })
  })

  describe('Empty State', () => {
    it('shows empty state when data is null', () => {
      mockUseSavingsImpactData.mockReturnValue(defaultDataState)
      renderWidget()
      // WidgetEmptyState renders with "No data available" when no emptyState config found
      // (registry is mocked, so getWidget returns undefined)
      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: mockData })
    })

    it('renders total amount moved to savings', () => {
      renderWidget()
      // $1,251 formatted as currency — check by content within data-ph-mask containers
      const maskedTexts = Array.from(document.querySelectorAll('[data-ph-mask]')).map(
        (el) => el.textContent ?? ''
      )
      expect(maskedTexts.some((t) => t.includes('1,251') || t.includes('1,250'))).toBe(true)
    })

    it('renders month amount', () => {
      renderWidget()
      const maskedTexts = Array.from(document.querySelectorAll('[data-ph-mask]')).map(
        (el) => el.textContent ?? ''
      )
      expect(maskedTexts.some((t) => t.includes('350'))).toBe(true)
    })

    it('renders completed count', () => {
      renderWidget()
      expect(screen.getByText(/5 transfer/i)).toBeInTheDocument()
    })

    it('applies data-ph-mask to dollar amounts', () => {
      renderWidget()
      const maskedElements = document.querySelectorAll('[data-ph-mask]')
      expect(maskedElements.length).toBeGreaterThan(0)
    })

    it('shows month-over-month change', () => {
      renderWidget()
      // 350 vs 200 = +$150 shown in a data-ph-mask span
      const maskedTexts = Array.from(document.querySelectorAll('[data-ph-mask]')).map(
        (el) => el.textContent ?? ''
      )
      expect(maskedTexts.some((t) => t.includes('150'))).toBe(true)
    })
  })

  describe('Analytics', () => {
    it('fires SAVINGS_IMPACT_VIEWED when data is non-null', async () => {
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: mockData })
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).toHaveBeenCalledWith('savings_impact_viewed')
    })

    it('does not fire SAVINGS_IMPACT_VIEWED when data is null', async () => {
      mockUseSavingsImpactData.mockReturnValue(defaultDataState) // data: null
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).not.toHaveBeenCalledWith('savings_impact_viewed')
    })

    it('does not fire SAVINGS_IMPACT_VIEWED while loading', async () => {
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, loading: true })
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).not.toHaveBeenCalledWith('savings_impact_viewed')
    })

    // C11: The viewedFired ref (SavingsImpactWidget.tsx:21-29) prevents the analytics
    // event from firing more than once per component mount, regardless of how many times
    // the data changes (simulating React Query refetches). Without the ref guard, every
    // data change would fire the event again.
    it('fires SAVINGS_IMPACT_VIEWED exactly once across refetches (viewedFired ref dedup)', async () => {
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: mockData })

      const { rerender } = render(
        <MemoryRouter>
          <SavingsImpactWidget widgetId="test-savings-impact" onRemove={() => {}} />
        </MemoryRouter>
      )
      await act(async () => {})

      // First render: event fires once.
      expect(mockTrackEvent).toHaveBeenCalledTimes(1)
      expect(mockTrackEvent).toHaveBeenCalledWith('savings_impact_viewed')

      // Simulate a refetch by updating the data object reference (same shape, new object).
      const refreshedData: UserSavingsImpact = { ...mockData, month_amount: 400.00 }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: refreshedData })

      // Rerender the SAME component instance (refs persist across rerenders).
      rerender(
        <MemoryRouter>
          <SavingsImpactWidget widgetId="test-savings-impact" onRemove={() => {}} />
        </MemoryRouter>
      )
      await act(async () => {})

      // The viewedFired ref is true after the first fire, so the event must NOT fire again.
      expect(mockTrackEvent).toHaveBeenCalledTimes(1)
    })
  })

  // GAP AZ: AC#1 requires the widget show "a list of recent completed suggestions
  // (amount, destination account, date)". The widget previously rendered only
  // total/count/this-month/MoM and never mapped recent_items.
  describe('Recent Items (GAP AZ — AC#1)', () => {
    it('renders a row for each recent item with data-ph-mask on amount', () => {
      const dataWithItems: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-1',
            amount: 300.00,
            actual_amount: 250.75,
            completed_at: '2024-03-15T10:00:00Z',
            completion_source: 'exact_match',
          },
          {
            id: 'sugg-2',
            amount: 100.00,
            actual_amount: 98.00,
            completed_at: '2024-02-20T08:30:00Z',
            completion_source: 'user_confirmed',
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataWithItems })
      renderWidget()

      // Both rows should render
      const recentRows = document.querySelectorAll('[data-testid="savings-recent-item"]')
      expect(recentRows.length).toBe(2)

      // Amounts must be wrapped with data-ph-mask
      const maskedAmounts = document.querySelectorAll('[data-testid="savings-recent-amount"][data-ph-mask]')
      expect(maskedAmounts.length).toBe(2)

      // GAP CB: recent-item amounts must show exact cents (250.75, not $251).
      const maskedTexts = Array.from(maskedAmounts).map((el) => el.textContent ?? '')
      expect(maskedTexts.some((t) => t.includes('250.75'))).toBe(true)
      expect(maskedTexts.some((t) => t.includes('98.00'))).toBe(true)
    })

    it('renders a date for each recent item', () => {
      const dataWithItems: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-date-1',
            amount: 200.00,
            actual_amount: 200.00,
            completed_at: '2024-03-15T10:00:00Z',
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataWithItems })
      renderWidget()

      // A formatted date should appear in the row
      const dateEl = document.querySelector('[data-testid="savings-recent-date"]')
      expect(dateEl).toBeInTheDocument()
      expect(dateEl?.textContent).toBeTruthy()
    })

    // GAP CA: when to_account_name is present, the real name is rendered.
    it('renders the real account name when to_account_name is provided', () => {
      const dataWithItems: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-dest-real',
            amount: 150.00,
            actual_amount: 150.00,
            completed_at: '2024-04-01T12:00:00Z',
            to_account_name: 'Chase Savings',
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataWithItems })
      renderWidget()

      const destEl = document.querySelector('[data-testid="savings-recent-destination"]')
      expect(destEl).toBeInTheDocument()
      expect(destEl?.textContent).toBe('Chase Savings')
    })

    // GAP CA: when to_account_name is absent, a neutral fallback is rendered (NOT "Savings account").
    it('renders neutral fallback when to_account_name is absent', () => {
      const dataWithItems: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-dest-fallback',
            amount: 150.00,
            actual_amount: 150.00,
            completed_at: '2024-04-01T12:00:00Z',
            to_user_account_id: 'acct-savings-123',
            // to_account_name intentionally absent
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataWithItems })
      renderWidget()

      const destEl = document.querySelector('[data-testid="savings-recent-destination"]')
      expect(destEl).toBeInTheDocument()
      expect(destEl?.textContent).toBe('Transfer destination')
      // Must NOT render the old misleading literal
      expect(destEl?.textContent).not.toBe('Savings account')
    })

    it('shows an empty-state message when recent_items is an empty array', () => {
      const dataEmpty: UserSavingsImpact = { ...mockData, recent_items: [] }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataEmpty })
      renderWidget()

      const noItems = document.querySelector('[data-testid="savings-recent-empty"]')
      expect(noItems).toBeInTheDocument()
    })

    it('shows an empty-state message when recent_items is null', () => {
      const dataNull: UserSavingsImpact = { ...mockData, recent_items: null }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataNull })
      renderWidget()

      const noItems = document.querySelector('[data-testid="savings-recent-empty"]')
      expect(noItems).toBeInTheDocument()
    })

    // GAP CC: CA replaced the non-PII literal "Savings account" with a real user-controlled
    // account name. That name must carry data-ph-mask for PostHog session-replay PII policy.
    // (posthog-pii-masking.md:45-49 classifies account names/nicknames as DOM-replay PII.)
    it('applies data-ph-mask to destination account name (GAP CC)', () => {
      const dataWithName: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-cc',
            amount: 150.00,
            actual_amount: 150.00,
            completed_at: '2024-04-01T12:00:00Z',
            to_account_name: 'Chase Savings',
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataWithName })
      renderWidget()

      // The destination span must carry data-ph-mask regardless of whether to_account_name
      // is present — masking the neutral fallback "Transfer destination" is harmless.
      const destEl = document.querySelector('[data-testid="savings-recent-destination"][data-ph-mask]')
      expect(destEl).toBeInTheDocument()
    })

    // Codex MED: completed_at is optional in the API type — the date cell is skipped
    // when absent. The row must still render with amount and destination.
    it('renders row without date when completed_at is absent', () => {
      const dataNoDate: UserSavingsImpact = {
        ...mockData,
        recent_items: [
          {
            id: 'sugg-no-date',
            amount: 180.00,
            actual_amount: 180.00,
            // completed_at intentionally omitted
          },
        ],
      }
      mockUseSavingsImpactData.mockReturnValue({ ...defaultDataState, data: dataNoDate })
      renderWidget()

      // Row and amount must still appear
      const row = document.querySelector('[data-testid="savings-recent-item"]')
      expect(row).toBeInTheDocument()
      const amountEl = document.querySelector('[data-testid="savings-recent-amount"][data-ph-mask]')
      expect(amountEl).toBeInTheDocument()

      // Date cell is omitted when completed_at is absent (conditional render)
      const dateEl = document.querySelector('[data-testid="savings-recent-date"]')
      expect(dateEl).toBeNull()
    })
  })
})
