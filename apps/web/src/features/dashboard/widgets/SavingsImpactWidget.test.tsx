/**
 * SavingsImpactWidget Tests
 * Issue #972: Savings Transfer Automation — dashboard widget
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
import { mock } from 'bun:test'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { mockPosthogCapture } from '@/test/mocks/posthog'

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

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
      const masked = document.querySelectorAll('[data-ph-mask]')
      const texts = Array.from(masked).map((el) => el.textContent ?? '')
      expect(texts.some((t) => t.includes('1,251') || t.includes('1,250'))).toBe(true)
    })

    it('renders month amount', () => {
      renderWidget()
      const masked = document.querySelectorAll('[data-ph-mask]')
      const texts = Array.from(masked).map((el) => el.textContent ?? '')
      expect(texts.some((t) => t.includes('350'))).toBe(true)
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
      const masked = document.querySelectorAll('[data-ph-mask]')
      const texts = Array.from(masked).map((el) => el.textContent ?? '')
      expect(texts.some((t) => t.includes('150'))).toBe(true)
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
})
