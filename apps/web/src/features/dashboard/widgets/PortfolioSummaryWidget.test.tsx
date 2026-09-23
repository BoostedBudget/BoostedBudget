/**
 * PortfolioSummaryWidget Tests
 * Issue #1297: Investment widgets — portfolio summary on the dashboard
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { getGetPortfolioSummaryQueryKey } from '@boostedbudget/api-client/endpoints/investments/investments'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'

// Use centralized mocks — no inline mock.module()
import '@/test/mocks/registry'
import { mockUseGetPortfolioSummary } from '@/test/mocks/investments-api'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { PortfolioSummaryWidget } from './PortfolioSummaryWidget'

function renderWidget() {
  return render(
    <MemoryRouter>
      <PortfolioSummaryWidget widgetId="test-portfolio-summary" onRemove={() => {}} />
    </MemoryRouter>
  )
}

describe('PortfolioSummaryWidget', () => {
  const mockSummary: PortfolioSummary = {
    total_value: 52340.50,
    total_cost_basis: 48000.00,
    total_gain_loss: 4340.50,
    total_gain_loss_pct: 9.04,
    day_change: 230.15,
    day_change_pct: 0.44,
    holdings_count: 5,
    // #1631 M8a: the API now states which currency these totals are in, and whether they are a
    // currency-blind sum. Both are required on the response type, so a fixture that omits them
    // describes a response the server cannot produce. This one is a single-currency portfolio.
    currency_code: 'USD',
    is_mixed_currency: false,
  }

  const defaultQueryState = {
    data: undefined,
    isLoading: false,
    isError: false,
    error: null,
  }

  beforeEach(() => {
    mockUseGetPortfolioSummary.mockClear()
    mockTrackEvent.mockClear()
    mockUseGetPortfolioSummary.mockReturnValue(defaultQueryState)
    mockUseDashboardContext.mockReturnValue({
      ...mockUseDashboardContext(),
      scope: 'personal',
    })
  })

  describe('Loading State', () => {
    it('shows loading spinner when isLoading is true', () => {
      mockUseGetPortfolioSummary.mockReturnValue({ ...defaultQueryState, isLoading: true })
      renderWidget()
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('shows error message when isError is true', () => {
      mockUseGetPortfolioSummary.mockReturnValue({
        ...defaultQueryState,
        isError: true,
        error: new Error('Network error'),
      })
      renderWidget()
      expect(screen.getByText('Network error')).toBeInTheDocument()
    })

    it('shows error text in destructive color', () => {
      mockUseGetPortfolioSummary.mockReturnValue({
        ...defaultQueryState,
        isError: true,
        error: new Error('API Error'),
      })
      renderWidget()
      const errorText = screen.getByText('API Error')
      expect(errorText).toHaveClass('text-destructive')
    })

    it('shows fallback text when error has no message', () => {
      mockUseGetPortfolioSummary.mockReturnValue({
        ...defaultQueryState,
        isError: true,
        error: null,
      })
      renderWidget()
      expect(screen.getByText('Failed to load portfolio')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('shows empty state when data is undefined', () => {
      mockUseGetPortfolioSummary.mockReturnValue(defaultQueryState)
      renderWidget()
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('shows empty state when holdings_count is 0', () => {
      mockUseGetPortfolioSummary.mockReturnValue({
        ...defaultQueryState,
        data: { ...mockSummary, holdings_count: 0 },
      })
      renderWidget()
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      mockUseGetPortfolioSummary.mockReturnValue({ ...defaultQueryState, data: mockSummary })
    })

    it('renders portfolio total value with data-ph-mask', () => {
      renderWidget()
      const maskedEls = Array.from(document.querySelectorAll('[data-ph-mask]'))
      const maskedText = maskedEls.map((el) => el.textContent ?? '').join(' ')
      expect(maskedText).toMatch(/52,340/)
    })

    it('renders total gain/loss with data-ph-mask', () => {
      renderWidget()
      const maskedEls = Array.from(document.querySelectorAll('[data-ph-mask]'))
      const maskedText = maskedEls.map((el) => el.textContent ?? '').join(' ')
      expect(maskedText).toMatch(/4,340/)
    })

    it('renders day change with data-ph-mask', () => {
      renderWidget()
      const maskedEls = Array.from(document.querySelectorAll('[data-ph-mask]'))
      const maskedText = maskedEls.map((el) => el.textContent ?? '').join(' ')
      expect(maskedText).toMatch(/230/)
    })

    it('applies data-ph-mask to monetary values', () => {
      renderWidget()
      const maskedElements = document.querySelectorAll('[data-ph-mask]')
      expect(maskedElements.length).toBeGreaterThan(0)
    })

    it('renders link to /insights?tab=portfolio', () => {
      renderWidget()
      const link = screen.getByRole('link', { name: /view portfolio/i })
      expect(link).toHaveAttribute('href', '/insights?tab=portfolio')
    })
  })

  describe('Analytics', () => {
    it('fires PORTFOLIO_SUMMARY_WIDGET_VIEWED when data is loaded', async () => {
      mockUseGetPortfolioSummary.mockReturnValue({ ...defaultQueryState, data: mockSummary })
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).toHaveBeenCalledWith('portfolio_summary_widget_viewed')
    })

    it('does not fire event when data is undefined', async () => {
      mockUseGetPortfolioSummary.mockReturnValue(defaultQueryState)
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).not.toHaveBeenCalledWith('portfolio_summary_widget_viewed')
    })

    it('does not fire event while loading', async () => {
      mockUseGetPortfolioSummary.mockReturnValue({ ...defaultQueryState, isLoading: true })
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).not.toHaveBeenCalledWith('portfolio_summary_widget_viewed')
    })

    it('fires event exactly once across data refetches', async () => {
      mockUseGetPortfolioSummary.mockReturnValue({ ...defaultQueryState, data: mockSummary })

      const { rerender } = render(
        <MemoryRouter>
          <PortfolioSummaryWidget widgetId="test-portfolio-summary" onRemove={() => {}} />
        </MemoryRouter>
      )
      await act(async () => {})
      expect(mockTrackEvent).toHaveBeenCalledTimes(1)

      // Simulate a refetch with updated data
      mockUseGetPortfolioSummary.mockReturnValue({
        ...defaultQueryState,
        data: { ...mockSummary, total_value: 53000 },
      })
      rerender(
        <MemoryRouter>
          <PortfolioSummaryWidget widgetId="test-portfolio-summary" onRemove={() => {}} />
        </MemoryRouter>
      )
      await act(async () => {})

      // viewedFired ref prevents re-firing
      expect(mockTrackEvent).toHaveBeenCalledTimes(1)
    })
  })

  describe('Dashboard Scope (Issue #1706)', () => {
    // The shared mock's factory takes no params and types `data` as `undefined`, so its inferred
    // return type rejects a real summary and its call tuple has no element 0. Cast once here
    // rather than widening the shared mock (which other suites depend on).
    const summaryMock = mockUseGetPortfolioSummary as unknown as {
      mockReturnValue: (value: unknown) => void
      mock: { calls: unknown[][] }
    }

    it('requests the portfolio summary with scope=household when the dashboard scope is household', () => {
      mockUseDashboardContext.mockReturnValue({
        ...mockUseDashboardContext(),
        scope: 'household',
      })
      summaryMock.mockReturnValue({ ...defaultQueryState, data: mockSummary })

      renderWidget()

      expect(mockUseGetPortfolioSummary).toHaveBeenCalled()
      const requestParams = summaryMock.mock.calls[0]?.[0] as
        | Record<string, unknown>
        | undefined
      expect(requestParams).toMatchObject({ scope: 'household' })
    })

    it('renders no day change when the household scope omits it', () => {
      mockUseDashboardContext.mockReturnValue({
        ...mockUseDashboardContext(),
        scope: 'household',
      })
      summaryMock.mockReturnValue({
        ...defaultQueryState,
        data: {
          ...mockSummary,
          day_change: null,
          day_change_pct: null,
          day_change_unavailable_reason: 'household_scope',
        },
      })

      renderWidget()

      expect(screen.queryByText(/day change/i)).toBeNull()
      expect(screen.queryAllByText('--').length).toBe(0)
    })

    it('keeps the personal and household query keys distinct so the scopes cannot share a cache entry', () => {
      expect(getGetPortfolioSummaryQueryKey({ scope: 'personal' })).not.toEqual(
        getGetPortfolioSummaryQueryKey({ scope: 'household' })
      )
    })
  })
})
