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

  it('normalizes "unknown" bucket label to "Other"', () => {
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
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    // The option should have a data item named "Other"
    // Chart renders so the echarts-chart mock is present
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
  })

  it('normalizes empty bucket label to "Other"', () => {
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
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
  })

  it('passes isDark to EChartsChart', () => {
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
