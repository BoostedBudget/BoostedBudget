import { describe, it, expect, beforeEach } from 'bun:test'
import { render as rtlRender, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import React from 'react'

import '@/test/mocks/shared-charts'
import { MockEChartsChart, mockUseDarkMode } from '@/test/mocks/shared-charts'
import { createLocationHarness } from '@/test/utils/locationProbe'
import { CashFlowSankeyChart } from './CashFlowSankeyChart'
import type { IncomeSourceBreakdownResponse, SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

// CashFlowSankeyChart calls useNavigate() unconditionally, so every render
// needs a real Router ancestor. A location probe rendered alongside the
// component lets tests assert on real navigation instead of a mocked fn.
const { LocationProbe, currentUrl, navigationCount, reset: resetLocationProbe } = createLocationHarness()

function render(ui: React.ReactElement) {
  return rtlRender(
    <MemoryRouter>
      <LocationProbe />
      {ui}
    </MemoryRouter>
  )
}

const mockIncomeData: IncomeSourceBreakdownResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  total_income: 5000,
  sources: [
    { category: 'INCOME_WAGES', pfc_primary_display: 'Wages', total: 5000, percentage: 100, transaction_count: 2 },
  ],
}

const mockCategoryData: SpendingByCategoryResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  total_spent: 2000,
  categories: [
    { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food & Drink', total: 1200, percentage: 60, transaction_count: 20 },
    { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 800, percentage: 40, transaction_count: 8 },
  ],
}

const defaultProps = {
  incomeData: mockIncomeData,
  categoryData: mockCategoryData,
  loading: false,
  privacyMode: false,
  startDate: '2024-01-01',
  endDate: '2024-01-31',
}

describe('CashFlowSankeyChart', () => {
  beforeEach(() => {
    resetLocationProbe()
    mockUseDarkMode.mockReturnValue(false)
  })

  describe('loading state', () => {
    it('renders loading skeleton with aria-busy when loading', () => {
      render(<CashFlowSankeyChart {...defaultProps} loading={true} />)
      const container = screen.getByTestId('sankey-chart')
      expect(container.getAttribute('aria-busy')).toBe('true')
    })

    it('does not render echarts chart when loading', () => {
      render(<CashFlowSankeyChart {...defaultProps} loading={true} />)
      expect(screen.queryByTestId('echarts-chart')).toBeNull()
    })
  })

  describe('empty state', () => {
    it('renders empty message when both data sources are null', () => {
      render(
        <CashFlowSankeyChart
          {...defaultProps}
          incomeData={null}
          categoryData={null}
        />,
      )
      expect(screen.getByText('No cash flow data for this period.')).toBeDefined()
    })

    it('renders empty message when income has no sources and categories empty', () => {
      render(
        <CashFlowSankeyChart
          {...defaultProps}
          incomeData={{ ...mockIncomeData, total_income: 0, sources: [] }}
          categoryData={{ ...mockCategoryData, total_spent: 0, categories: [] }}
        />,
      )
      expect(screen.getByText('No cash flow data for this period.')).toBeDefined()
    })
  })

  describe('chart rendering', () => {
    it('renders echarts chart when data is present', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      expect(screen.getByTestId('echarts-chart')).toBeDefined()
    })

    it('renders sankey series type', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const chart = screen.getByTestId('echarts-chart')
      const types = JSON.parse(chart.dataset.seriesTypes ?? '[]') as string[]
      expect(types[0]).toBe('sankey')
    })

    it('passes isDark=true when dark mode active', () => {
      mockUseDarkMode.mockReturnValue(true)
      render(<CashFlowSankeyChart {...defaultProps} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
    })

    it('passes isDark=false when light mode', () => {
      mockUseDarkMode.mockReturnValue(false)
      render(<CashFlowSankeyChart {...defaultProps} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })

  describe('accessibility table', () => {
    it('renders a sr-only table for keyboard navigation', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const table = screen.getByRole('table', { hidden: true })
      expect(table).toBeDefined()
      expect(table.getAttribute('aria-label')).toBe('Cash flow data table')
    })

    it('table rows correspond to links in the Sankey (hub→expenses + hub→savings)', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      // income→hub, hub→expenses×2, hub→savings = 4 links total
      const table = screen.getByRole('table', { hidden: true })
      const rows = table.querySelectorAll('tbody tr')
      expect(rows.length).toBe(4)
    })
  })

  describe('tooltip formatter', () => {
    it('formats tooltip with amount and percentage when privacyMode=false', () => {
      render(<CashFlowSankeyChart {...defaultProps} privacyMode={false} />)
      const lastCall = MockEChartsChart.mock.calls.at(-1)
      const option = lastCall?.[0]?.option as { tooltip?: { formatter?: (p: unknown) => string } }
      const formatted = option.tooltip!.formatter!({ value: 1200, name: 'Food & Drink' })
      expect(formatted).toContain('Food &amp; Drink')
      expect(formatted).toContain('$1,200')
    })

    it('shows only percentage in tooltip when privacyMode=true', () => {
      render(<CashFlowSankeyChart {...defaultProps} privacyMode={true} />)
      const lastCall = MockEChartsChart.mock.calls.at(-1)
      const option = lastCall?.[0]?.option as { tooltip?: { formatter?: (p: unknown) => string } }
      const formatted = option.tooltip!.formatter!({ value: 1200, name: 'Food & Drink' })
      expect(formatted).not.toContain('$1,200')
      expect(formatted).toContain('%')
    })

    it('returns empty string for non-value params', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const lastCall = MockEChartsChart.mock.calls.at(-1)
      const option = lastCall?.[0]?.option as { tooltip?: { formatter?: (p: unknown) => string } }
      expect(option.tooltip!.formatter!({ name: 'link', value: undefined })).toBe('')
    })
  })

  describe('click navigation', () => {
    it('navigates to expense transactions when an expense node is clicked', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      // nodes order: hub(0), in:INCOME_WAGES(1), out:FOOD_AND_DRINK(2), out:TRANSPORTATION(3), savings(4)
      const foodBtn = screen.getByTestId('echarts-point-2')
      fireEvent.click(foodBtn)
      expect(navigationCount()).toBe(1)
      const url = currentUrl()
      expect(url).toContain('/transactions')
      expect(url).toContain('pfc_primaries=FOOD_AND_DRINK')
      expect(url).toContain('amount_type=expense')
      expect(url).toContain('start_date=2024-01-01')
    })

    it('navigates to income transactions when an income node is clicked', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const wagesBtn = screen.getByTestId('echarts-point-1')
      fireEvent.click(wagesBtn)
      expect(navigationCount()).toBe(1)
      const url = currentUrl()
      expect(url).toContain('pfc_primaries=INCOME_WAGES')
      expect(url).toContain('amount_type=income')
    })

    it('income navigation appends start_date and end_date (G14)', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      fireEvent.click(screen.getByTestId('echarts-point-1')) // Wages income node
      const url = currentUrl()
      expect(url).toContain('start_date=2024-01-01')
      expect(url).toContain('end_date=2024-01-31')
    })

    it('omits start_date and end_date from URL when they are empty (G14)', () => {
      render(
        <CashFlowSankeyChart {...defaultProps} startDate="" endDate="" />,
      )
      fireEvent.click(screen.getByTestId('echarts-point-2')) // Food expense node
      const url = currentUrl()
      expect(url).not.toContain('start_date')
      expect(url).not.toContain('end_date')
    })

    it('does NOT navigate when the hub node is clicked', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const initialUrl = currentUrl()
      fireEvent.click(screen.getByTestId('echarts-point-0'))
      expect(navigationCount()).toBe(0)
      expect(currentUrl()).toBe(initialUrl)
    })

    it('does NOT navigate when the savings node is clicked', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const initialUrl = currentUrl()
      // savings is the last node in surplus scenario
      const lastTrigger = screen.getAllByTestId(/^echarts-point-/).at(-1)!
      fireEvent.click(lastTrigger)
      // savings is non-navigable
      expect(navigationCount()).toBe(0)
      expect(currentUrl()).toBe(initialUrl)
    })

    it('uses custom_category_ids for custom category expense nodes', () => {
      const customData: SpendingByCategoryResponse = {
        ...mockCategoryData,
        total_spent: 500,
        categories: [
          { category: 'custom:uuid-abc', pfc_primary_display: 'My Budget', total: 500, percentage: 100, transaction_count: 5 },
        ],
      }
      render(
        <CashFlowSankeyChart
          {...defaultProps}
          categoryData={customData}
          incomeData={{ ...mockIncomeData, total_income: 1000 }}
        />,
      )
      // nodes: hub(0), in:INCOME_WAGES(1), out:custom:uuid-abc(2), savings(3)
      fireEvent.click(screen.getByTestId('echarts-point-2'))
      const url = currentUrl()
      expect(url).toContain('custom_category_ids=uuid-abc')
      expect(url).not.toContain('pfc_primaries')
    })
  })

  describe('accessibility table keyboard navigation', () => {
    it('clicking a navigable link in the table navigates to transactions', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      // The table has buttons for navigable links
      const table = screen.getByRole('table', { hidden: true })
      const buttons = table.querySelectorAll('button')
      expect(buttons.length).toBeGreaterThan(0)
      // Issue #1437: a raw DOM .click() isn't wrapped in act(), so the router's
      // real state update (from navigate()) may not have flushed before the
      // assertion runs. fireEvent.click() wraps the dispatch in act() for us.
      fireEvent.click(buttons[0] as HTMLElement)
      expect(navigationCount()).toBeGreaterThan(0)
    })
  })

  describe('lineStyle gradient (G1)', () => {
    it('sets lineStyle.color to "gradient" regardless of theme', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      type OptionType = { series?: Array<{ lineStyle?: { color?: string; opacity?: number } }> }
      const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as OptionType
      expect(option.series?.[0]?.lineStyle?.color).toBe('gradient')
      expect(option.series?.[0]?.lineStyle?.opacity).toBe(0.5)
    })
  })

  describe('edge-click guard (G4)', () => {
    it('does NOT navigate when ECharts fires a link/edge click (dataType set but not "node")', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const initialUrl = currentUrl()
      // The mock trigger buttons fire { dataIndex } with no dataType — they exercise the
      // test-mock branch. To test the real-ECharts edge-click guard we grab the handler
      // directly and call it with a synthetic edge event.
      type EventsType = { click?: (p: unknown) => void }
      const clickHandler = MockEChartsChart.mock.calls.at(-1)?.[0]?.onEvents as EventsType | undefined
      clickHandler?.click?.({ dataType: 'edge', dataIndex: 2 })
      expect(navigationCount()).toBe(0)
      expect(currentUrl()).toBe(initialUrl)
    })
  })

  describe('PII — sr-only table data-ph-mask (G6)', () => {
    it('the sr-only table has data-ph-mask so currency amounts are masked', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      const table = screen.getByRole('table', { hidden: true })
      // Attribute presence is what posthog-pii-masking.ts checks (hasMaskedAncestor uses
      // hasAttribute, not the value). JSX boolean `data-ph-mask` renders as "true" in DOM.
      expect(table.hasAttribute('data-ph-mask')).toBe(true)
    })
  })

  describe('label formatter — name not id (G11)', () => {
    it('label formatter returns the display name, not the internal out: id', () => {
      render(<CashFlowSankeyChart {...defaultProps} />)
      type OptionType = { series?: Array<{ label?: { formatter?: (p: unknown) => string } }> }
      const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as OptionType
      const formatter = option.series?.[0]?.label?.formatter
      expect(formatter).toBeDefined()
      // Should return the escaped display name — not 'out:FOOD_AND_DRINK'
      const result = formatter!({ data: { id: 'out:FOOD_AND_DRINK', name: 'Food & Drink' } })
      expect(result).toBe('Food &amp; Drink')
      expect(result).not.toContain('out:')
    })
  })

  describe('percentage base — uses filtered array sums, not diverged API totals (G10/Task F)', () => {
    it('tooltip percentage is based on filtered expense sum, not API total_spent when they diverge', () => {
      // Scenario: expenses total_spent=1950 (API total includes a filtered refund row)
      // Valid expense sum = 1200+800 = 2000 (the refund has total:-50, filtered out)
      // income=1500 < expenses=2000 → deficit scenario
      // hubTotal (correct, from array sums) = max(1500, 2000) = 2000
      // hubTotal (OLD bug, from API totals) = max(1500, 1950) = 1950
      const divergedCategoryData: SpendingByCategoryResponse = {
        start_date: '2024-01-01',
        end_date: '2024-01-31',
        total_spent: 1950, // API total diverges from valid sum
        categories: [
          { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food & Drink', total: 1200, percentage: 60, transaction_count: 20 },
          { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 800, percentage: 40, transaction_count: 8 },
          { category: 'REFUNDS', pfc_primary_display: 'Refunds', total: -50, percentage: 0, transaction_count: 1 },
        ],
      }
      render(
        <CashFlowSankeyChart
          {...defaultProps}
          incomeData={{ ...mockIncomeData, total_income: 1500, sources: [{ category: 'INCOME_WAGES', pfc_primary_display: 'Wages', total: 1500, percentage: 100, transaction_count: 2 }] }}
          categoryData={divergedCategoryData}
        />
      )
      type OptionType = { tooltip?: { formatter?: (p: unknown) => string } }
      const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as OptionType
      const formatter = option.tooltip!.formatter!
      // Wages→hub link value = 1500; correct hubTotal = 2000
      // pct = (1500/2000)*100 = 75.0 → "75.00%"
      // With the old bug hubTotal was 1950 → pct ≈ 76.9 → "76.92%"
      const result = formatter({ value: 1500, name: 'Wages' })
      expect(result).toContain('75.00%')
    })
  })
})
