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

  // R6-3: Aggregate same-normalized-name buckets — duplicate labels must not collide.
  it('merges two buckets normalizing to the same label into one slice with summed value+pct', () => {
    // Two buckets both normalize to "Other" (unknown + empty security_type).
    // Without aggregation the second would overwrite the first in sliceByName,
    // causing the legend to show the wrong value for "Other".
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 10000,
        buckets: [
          { security_type: 'unknown', asset_class: '', total_value: 3000, percentage: 30, holding_count: 1 },
          { security_type: '', asset_class: '', total_value: 2000, percentage: 20, holding_count: 2 },
          { security_type: 'stock', asset_class: 'stock', total_value: 5000, percentage: 50, holding_count: 3 },
        ],
      },
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
    // The two "Other" buckets must be merged into one — only 2 slices total.
    expect(data?.length).toBe(2)

    const otherSlice = data?.find((d) => d.name === 'Other')
    expect(otherSlice).toBeDefined()
    expect(otherSlice?.value).toBe(5000) // 3000 + 2000
    expect(otherSlice?.pct).toBeCloseTo(50, 1) // 30 + 20

    const stockSlice = data?.find((d) => d.name === 'stock')
    expect(stockSlice).toBeDefined()
    expect(stockSlice?.value).toBe(5000)
  })

  // R6-3: Legend formatter must use the merged "Other" slice value, not the overwritten one.
  it('legend formatter shows the merged (summed) value for a merged "Other" slice', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 5000,
        buckets: [
          { security_type: 'unknown', asset_class: '', total_value: 2000, percentage: 40, holding_count: 1 },
          { security_type: '', asset_class: '', total_value: 3000, percentage: 60, holding_count: 1 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { legend?: { formatter?: unknown } }
    }
    const formatter = props.option?.legend?.formatter
    expect(typeof formatter).toBe('function')
    if (typeof formatter === 'function') {
      const result = formatter('Other') as string
      // Merged value is 5000 (2000+3000) — must NOT show 3000 (last-write-wins)
      expect(result).toContain('$5,000')
    }
  })

  // Codex R6 issue 2: zero-value buckets (e.g. fully-overdrawn cash floored to $0 by GREATEST)
  // must not render a 0.1%-fabricated slice — show "No allocation data" instead.
  it('shows no-data state when all buckets have zero total_value', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 0,
        buckets: [
          { security_type: 'cash', asset_class: 'cash', total_value: 0, percentage: 0, holding_count: 0 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByText(/no allocation data/i)).toBeDefined()
  })

  // Codex R6 issue 3: reconcilePercentages must handle negative deficit (API pcts sum > 100).
  it('displayed percentages sum to exactly 100.0 when API pcts slightly overshoot 100 (negative deficit)', () => {
    // Two slices with pct=50.11: floors = [501, 501], sum=1002 > TARGET=1000 → deficit=-2.
    // Without the fix: no adjustments → displayed 50.1+50.1=100.2. With fix: both reduced to 50.0.
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 20000,
        buckets: [
          { security_type: 'A', asset_class: 'A', total_value: 10000, percentage: 50.11, holding_count: 1 },
          { security_type: 'B', asset_class: 'B', total_value: 10000, percentage: 50.11, holding_count: 1 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ pct: number }> }> }
    }
    const data = props.option?.series?.[0]?.data
    expect(data).toBeDefined()
    if (data) {
      const displayedSum = data.reduce((sum, d) => sum + parseFloat(d.pct.toFixed(1)), 0)
      expect(Math.abs(displayedSum - 100.0)).toBeLessThanOrEqual(0.05)
    }
  })

  // R6-5: Largest-remainder rounding — sum of displayed percentages must be exactly 100.0.
  it('displayed percentages (after .toFixed(1)) sum to exactly 100.0', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 30000,
        buckets: [
          { security_type: 'A', asset_class: 'A', total_value: 10000, percentage: 33.333333, holding_count: 1 },
          { security_type: 'B', asset_class: 'B', total_value: 10000, percentage: 33.333333, holding_count: 1 },
          { security_type: 'C', asset_class: 'C', total_value: 10000, percentage: 33.333333, holding_count: 1 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ pct: number }> }> }
    }
    const data = props.option?.series?.[0]?.data
    expect(data).toBeDefined()
    if (data) {
      const displayedSum = data.reduce((sum, d) => sum + parseFloat(d.pct.toFixed(1)), 0)
      expect(Math.abs(displayedSum - 100.0)).toBeLessThanOrEqual(0.05)
    }
  })

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
