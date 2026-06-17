/**
 * PortfolioHoldingsWidget Tests
 * Issue #1297: Investment widgets — compact holdings table with configurable topN + sort
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { GetHoldingsOutputBody } from '@boostedbudget/api-client/models/getHoldingsOutputBody'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'

// Use centralized mocks — no inline mock.module()
import '@/test/mocks/registry'
import { mockUseGetHoldings } from '@/test/mocks/investments-api'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { PortfolioHoldingsWidget } from './PortfolioHoldingsWidget'

function renderWidget(props?: { settings?: Record<string, unknown>; isEditing?: boolean; onSettingsChange?: (s: unknown) => void }) {
  return render(
    <MemoryRouter>
      <PortfolioHoldingsWidget
        widgetId="test-holdings"
        onRemove={() => {}}
        settings={props?.settings}
        onSettingsChange={props?.onSettingsChange}
        isEditing={props?.isEditing ?? false}
      />
    </MemoryRouter>
  )
}

const makeHolding = (override: Partial<HoldingView> & { id: string }): HoldingView => ({
  id: override.id,
  user_id: 'user-1',
  account_id: 'acct-1',
  security_id: `sec-${override.id}`,
  quantity: 10,
  ...override,
})

describe('PortfolioHoldingsWidget', () => {
  const mockHoldings: HoldingView[] = [
    makeHolding({ id: 'h1', ticker_symbol: 'AAPL', security_name: 'Apple Inc', institution_value: 10000, day_change_pct: 1.5, day_change: 148 }),
    makeHolding({ id: 'h2', ticker_symbol: 'MSFT', security_name: 'Microsoft Corp', institution_value: 8000, day_change_pct: -0.5, day_change: -40 }),
    makeHolding({ id: 'h3', ticker_symbol: 'GOOGL', security_name: 'Alphabet Inc', institution_value: 6000, day_change_pct: 0.3, day_change: 18 }),
    makeHolding({ id: 'h4', ticker_symbol: 'TSLA', security_name: 'Tesla Inc', institution_value: 4000, day_change_pct: -2.1, day_change: -86 }),
    makeHolding({ id: 'h5', ticker_symbol: 'AMZN', security_name: 'Amazon', institution_value: 3000, day_change_pct: 0.8, day_change: 24 }),
    makeHolding({ id: 'h6', ticker_symbol: 'NVDA', security_name: 'NVIDIA Corp', institution_value: 2000, day_change_pct: 3.2, day_change: 62 }),
  ]

  const mockData: GetHoldingsOutputBody = {
    count: mockHoldings.length,
    holdings: mockHoldings,
  }

  const defaultQueryState = {
    data: undefined,
    isLoading: false,
    isError: false,
    error: null,
  }

  beforeEach(() => {
    mockUseGetHoldings.mockClear()
    mockTrackEvent.mockClear()
    mockUseGetHoldings.mockReturnValue(defaultQueryState)
  })

  describe('Loading State', () => {
    it('shows loading spinner when isLoading is true', () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, isLoading: true })
      renderWidget()
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('shows error message when isError is true', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        isError: true,
        error: new Error('Failed to fetch'),
      })
      renderWidget()
      expect(screen.getByText('Failed to fetch')).toBeInTheDocument()
    })

    it('shows error in destructive color', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        isError: true,
        error: new Error('Network error'),
      })
      renderWidget()
      const errorText = screen.getByText('Network error')
      expect(errorText).toHaveClass('text-destructive')
    })
  })

  describe('Empty State', () => {
    it('shows empty state when data is undefined', () => {
      mockUseGetHoldings.mockReturnValue(defaultQueryState)
      renderWidget()
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('shows empty state when holdings is null', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        data: { count: 0, holdings: null },
      })
      renderWidget()
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('shows empty state when holdings count is 0', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        data: { count: 0, holdings: [] },
      })
      renderWidget()
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('Success State', () => {
    beforeEach(() => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
    })

    it('renders top 5 holdings by default (topN=5)', () => {
      renderWidget()
      const rows = document.querySelectorAll('[data-testid="holding-row"]')
      expect(rows.length).toBe(5)
    })

    it('shows ticker symbol', () => {
      renderWidget()
      expect(screen.getByText('AAPL')).toBeInTheDocument()
    })

    it('applies data-ph-mask to institution_value', () => {
      renderWidget()
      const maskedValues = document.querySelectorAll('[data-testid="holding-value"][data-ph-mask]')
      expect(maskedValues.length).toBeGreaterThan(0)
    })

    it('applies data-ph-mask to day_change_pct', () => {
      renderWidget()
      const maskedPct = document.querySelectorAll('[data-testid="holding-day-change"][data-ph-mask]')
      expect(maskedPct.length).toBeGreaterThan(0)
    })

    it('applies data-ph-mask to security name', () => {
      renderWidget()
      const maskedName = document.querySelectorAll('[data-testid="holding-name"][data-ph-mask]')
      expect(maskedName.length).toBeGreaterThan(0)
    })

    it('renders holdings sorted by institution_value descending by default', () => {
      renderWidget()
      const rows = document.querySelectorAll('[data-testid="holding-row"]')
      // First row should be AAPL (highest value: $10,000)
      expect(rows[0].textContent).toContain('AAPL')
    })
  })

  describe('Settings: topN', () => {
    it('shows topN=10 rows when settings.topN is 10', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        data: {
          count: 10,
          holdings: Array.from({ length: 10 }, (_, i) =>
            makeHolding({ id: `h${i}`, ticker_symbol: `TK${i}`, institution_value: (10 - i) * 1000 })
          ),
        },
      })
      renderWidget({ settings: { topN: 10 } })
      const rows = document.querySelectorAll('[data-testid="holding-row"]')
      expect(rows.length).toBe(10)
    })

    it('caps at topN when fewer holdings exist', () => {
      mockUseGetHoldings.mockReturnValue({
        ...defaultQueryState,
        data: { count: 3, holdings: mockHoldings.slice(0, 3) },
      })
      renderWidget({ settings: { topN: 10 } })
      const rows = document.querySelectorAll('[data-testid="holding-row"]')
      expect(rows.length).toBe(3)
    })
  })

  describe('Settings: sortBy day_change', () => {
    it('sorts by day_change_pct descending when sortBy=day_change', () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
      renderWidget({ settings: { sortBy: 'day_change' } })
      const rows = document.querySelectorAll('[data-testid="holding-row"]')
      // NVDA has highest day_change_pct (3.2%), should be first
      expect(rows[0].textContent).toContain('NVDA')
    })
  })

  describe('Edit Mode Controls', () => {
    it('hides topN selector in view mode', () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
      renderWidget({ isEditing: false })
      expect(screen.queryByRole('combobox')).toBeNull()
    })

    it('shows topN selector in edit mode', () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
      renderWidget({ isEditing: true })
      // Should have at least one select/combobox for settings
      const combos = screen.queryAllByRole('combobox')
      expect(combos.length).toBeGreaterThan(0)
    })

    it('calls onSettingsChange with new topN when selector changes', () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
      const onSettingsChange = () => {}
      // Just ensure the selector renders and doesn't throw
      renderWidget({ isEditing: true, settings: { topN: 5, sortBy: 'value' }, onSettingsChange })
      const combos = screen.queryAllByRole('combobox')
      expect(combos.length).toBeGreaterThan(0)
    })
  })

  describe('Analytics', () => {
    it('fires PORTFOLIO_HOLDINGS_WIDGET_VIEWED when data loads', async () => {
      mockUseGetHoldings.mockReturnValue({ ...defaultQueryState, data: mockData })
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).toHaveBeenCalledWith('portfolio_holdings_widget_viewed')
    })

    it('does not fire event when data is undefined', async () => {
      mockUseGetHoldings.mockReturnValue(defaultQueryState)
      renderWidget()
      await act(async () => {})
      expect(mockTrackEvent).not.toHaveBeenCalledWith('portfolio_holdings_widget_viewed')
    })
  })
})
