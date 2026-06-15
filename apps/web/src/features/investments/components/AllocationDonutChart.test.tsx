/**
 * AllocationDonutChart Component Tests
 * Issue #1296: Donut chart showing portfolio allocation breakdown.
 * TDD: tests written before implementation.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
import { mockUseGetPortfolioAllocationBreakdown } from '@/test/mocks/analytics-api'
import { AllocationDonutChart } from './AllocationDonutChart'
import type { AllocationConfig } from '../hooks/usePortfolioAllocationConfig'

const DEFAULT_CONFIG: AllocationConfig = {
  include: [],
  group_by: 'security_type',
  basis: 'assets',
}

describe('AllocationDonutChart', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
  })

  it('renders pie slices from allocation buckets', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 50000,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 40000, percentage: 80, holding_count: 5 },
          { security_type: 'Fixed Income', asset_class: 'bond', total_value: 10000, percentage: 20, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
  })

  it('shows loading state', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('shows no-data state when buckets is empty array', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 0, buckets: [] },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByText(/no allocation data/i)).toBeDefined()
  })

  it('shows no-data state when buckets is null or undefined', () => {
    // Test both null and undefined — both falsy cases render no-data state.
    ;[null, undefined].forEach((buckets) => {
      mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
        data: buckets === undefined ? undefined : { total_value: 0, buckets: buckets as null },
        isLoading: false,
        isError: false,
      })
      const { unmount } = render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
      expect(screen.getByText(/no allocation data/i)).toBeDefined()
      unmount()
    })
  })

  it('normalizes "unknown" bucket label to "Other" in series data', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 10000,
        buckets: [
          { security_type: 'unknown', asset_class: '', total_value: 10000, percentage: 100, holding_count: 3 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    // series[0].data[0].name must be 'Other', not 'unknown'
    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ name: string }> }> }
    }
    expect(props.option?.series?.[0]?.data?.[0]?.name).toBe('Other')
  })

  it('normalizes empty bucket label to "Other" in series data', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 5000,
        buckets: [
          { security_type: '', asset_class: '', total_value: 5000, percentage: 100, holding_count: 1 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ name: string }> }> }
    }
    expect(props.option?.series?.[0]?.data?.[0]?.name).toBe('Other')
  })

  // R5-6: assert the exact {name, value, pct} shape of series[0].data so mis-mapping
  // is caught even if the chart renders (donut being "present" is not enough).
  it('series[0].data items have exact {name, value, pct} shape from bucket fields', () => {
    const buckets = [
      { security_type: 'stock', asset_class: 'stock', total_value: 8000, percentage: 80, holding_count: 3 },
      { security_type: 'cash', asset_class: 'cash', total_value: 2000, percentage: 20, holding_count: 1 },
    ]
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 10000, buckets },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ name: string; value: number; pct: number }> }> }
    }
    const data = props.option?.series?.[0]?.data
    expect(data).toBeDefined()
    expect(data?.length).toBe(2)

    const stock = data?.[0]
    expect(stock?.name).toBe('stock')
    expect(stock?.value).toBe(8000)
    expect(stock?.pct).toBe(80)

    const cash = data?.[1]
    expect(cash?.name).toBe('cash')
    expect(cash?.value).toBe(2000)
    expect(cash?.pct).toBe(20)
  })

  // R4-2: C11 — both isDark branches must be explicitly asserted (not just dark).
  it('passes isDark=true to EChartsChart in dark mode', () => {
    mockUseDarkMode.mockReturnValue(true)
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 50000,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 50000, percentage: 100, holding_count: 5 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
  })

  it('passes isDark=false to EChartsChart in light mode', () => {
    // beforeEach already sets mockUseDarkMode.mockReturnValue(false)
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 50000,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 50000, percentage: 100, holding_count: 5 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
  })

  it('renders a pie series type', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 50000,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 50000, percentage: 100, holding_count: 5 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    const chart = screen.getByTestId('echarts-chart')
    const types = JSON.parse(chart.dataset.seriesTypes ?? '[]') as string[]
    expect(types[0]).toBe('pie')
  })

  // Criterion 7: "shows % AND value" — the legend formatter must produce text that includes
  // both a currency value ($) and a percentage (%) so the legend is self-contained.
  it('legend formatter produces text with currency and percent', () => {
    const buckets = [
      { security_type: 'stock', asset_class: 'stock', total_value: 8000, percentage: 80, holding_count: 3 },
      { security_type: 'cash', asset_class: 'cash', total_value: 2000, percentage: 20, holding_count: 1 },
    ]
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 10000, buckets },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    // Retrieve the option prop passed to EChartsChart via mock call inspection.
    const calls = MockEChartsChart.mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // The last call's first arg is the props object; option is a prop.
    const props = calls[calls.length - 1][0] as { option?: { legend?: { formatter?: unknown } } }
    const formatter = props.option?.legend?.formatter

    expect(typeof formatter).toBe('function')
    if (typeof formatter === 'function') {
      const result = formatter('stock') as string
      // Must include a currency marker and a percent marker
      expect(result).toContain('$')
      expect(result).toContain('%')
    }
  })
})
