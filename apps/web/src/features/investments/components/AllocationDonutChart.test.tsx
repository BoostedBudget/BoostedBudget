/**
 * AllocationDonutChart Component Tests
 * Issue #1296: Donut chart showing portfolio allocation breakdown.
 * TDD: tests written before implementation.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart, MOCK_CATEGORY_COLORS } from '@/test/mocks/shared-charts'
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

  // R5-6: Verify exact {name, value, pct} shape to catch field mis-mapping
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
    expect(stock?.name).toBe('Stock')
    expect(stock?.value).toBe(8000)
    expect(stock?.pct).toBe(80)

    const cash = data?.[1]
    expect(cash?.name).toBe('Cash')
    expect(cash?.value).toBe(2000)
    expect(cash?.pct).toBe(20)
  })

  // R8-1: humanizeBucketLabel — underscored / acronym / mixed-case security types
  it('humanizes underscored security types to title-case display labels', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 30000,
        buckets: [
          { security_type: 'mutual_fund', asset_class: 'mutual_fund', total_value: 10000, percentage: 33.3, holding_count: 2 },
          { security_type: 'fixed_income', asset_class: 'fixed_income', total_value: 10000, percentage: 33.3, holding_count: 1 },
          { security_type: 'real_estate', asset_class: 'real_estate', total_value: 10000, percentage: 33.4, holding_count: 1 },
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
    const names = props.option?.series?.[0]?.data?.map((d) => d.name)
    expect(names).toContain('Mutual Fund')
    expect(names).toContain('Fixed Income')
    expect(names).toContain('Real Estate')
  })

  it('humanizes acronym security types to uppercase display labels', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 20000,
        buckets: [
          { security_type: 'etf', asset_class: 'etf', total_value: 12000, percentage: 60, holding_count: 3 },
          { security_type: 'reit', asset_class: 'reit', total_value: 8000, percentage: 40, holding_count: 2 },
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
    const names = props.option?.series?.[0]?.data?.map((d) => d.name)
    expect(names).toContain('ETF')
    expect(names).toContain('REIT')
  })

  it('passes account names verbatim in group_by=account mode (no title-casing)', () => {
    const accountConfig: AllocationConfig = { include: [], group_by: 'account', basis: 'assets' }
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 30000,
        buckets: [
          // Intentionally lowercase so title-case would produce a different string —
          // this gates the groupBy==='account' verbatim passthrough branch.
          { security_type: 'schwab brokerage', asset_class: 'investments', total_value: 20000, percentage: 66.7, holding_count: 5 },
          { security_type: 'fidelity ira', asset_class: 'investments', total_value: 10000, percentage: 33.3, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={accountConfig} />)
    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ name: string }> }> }
    }
    const names = props.option?.series?.[0]?.data?.map((d) => d.name)
    // Account names must NOT be title-cased — lowercase fixture would become 'Schwab Brokerage'
    // / 'Fidelity Ira' if title-case ran; asserting the original proves the verbatim path.
    expect(names).toContain('schwab brokerage')
    expect(names).toContain('fidelity ira')
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

  // R6-3: Aggregate same-label buckets (e.g., unknown + empty → "Other")
  it('merges two buckets normalizing to the same label into one slice with summed value+pct', () => {
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

    const stockSlice = data?.find((d) => d.name === 'Stock')
    expect(stockSlice).toBeDefined()
    expect(stockSlice?.value).toBe(5000)
  })

  it('legend formatter shows the merged (summed) value for "Other" slice', () => {
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

  // Zero-value buckets (overdrawn cash, etc.) must not render fabricated slices
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

  // Handle negative deficit (API pcts > 100) in percentage reconciliation
  it('displayed percentages sum to exactly 100.0 when API pcts slightly overshoot 100 (negative deficit)', () => {
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

  // R7-2: Handle large deficits (|deficit| > sliceCount) in percentage reconciliation
  it('displayed percentages sum to exactly 100.0 when |deficit| > sliceCount', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 20000,
        buckets: [
          { security_type: 'A', asset_class: 'A', total_value: 10000, percentage: 50.31, holding_count: 1 },
          { security_type: 'B', asset_class: 'B', total_value: 10000, percentage: 50.31, holding_count: 1 },
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
      // base=3, extra=0 → each slice: floor(503) - 3 = 500 tenths = 50.0%
      expect(data[0].pct).toBe(50.0)
      expect(data[1].pct).toBe(50.0)
      const displayedSum = data.reduce((sum, d) => sum + parseFloat(d.pct.toFixed(1)), 0)
      expect(Math.abs(displayedSum - 100.0)).toBeLessThanOrEqual(0.05)
    }
  })

  // R7-4: Enforce colorblind-safe palette slice-by-slice
  it('applies CATEGORY_COLORS palette to each slice (colorblind-safe)', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 30000,
        buckets: [
          { security_type: 'stock', asset_class: 'stock', total_value: 20000, percentage: 66.7, holding_count: 2 },
          { security_type: 'bond', asset_class: 'bond', total_value: 10000, percentage: 33.3, holding_count: 1 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    MockEChartsChart.mockClear()
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const props = MockEChartsChart.mock.calls[0]?.[0] as {
      option?: { series?: Array<{ data?: Array<{ name: string; itemStyle?: { color: string } }> }> }
    }
    const data = props.option?.series?.[0]?.data
    expect(data).toBeDefined()
    expect(data?.length).toBeGreaterThanOrEqual(2)
    data?.forEach((slice, i) => {
      expect(slice.itemStyle?.color).toBe(MOCK_CATEGORY_COLORS[i % MOCK_CATEGORY_COLORS.length])
    })
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
      // ECharts calls the legend formatter with the slice's display name (humanized).
      const result = formatter('Stock') as string
      // Must include a currency marker and a percent marker
      expect(result).toContain('$')
      expect(result).toContain('%')
    }
  })
})

// Issue #1569 T9.5 — "No allocation data" on a failed read tells the user they hold nothing.
describe('AllocationDonutChart failed read', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
  })

  // The API mock is process-global; leave it as other files expect to find it.
  afterEach(() => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({ data: undefined, isLoading: false, isError: false })
  })

  it('reports the failed read and does not claim there is no allocation data', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    })
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)
    expect(screen.getByRole('alert')).toBeDefined()
    expect(screen.getByText(/couldn't load/i)).toBeDefined()
    expect(screen.queryByText(/no allocation data/i)).toBeNull()
    expect(screen.queryByTestId('echarts-chart')).toBeNull()
  })
})

// #1631 CX1: tooltip and legend formatted every slice as dollars. The breakdown carries its own
// currency verdict and it was thrown away, so a GBP portfolio's donut read in dollars and a mixed
// one claimed a currency the backend had explicitly refused to name.
describe('AllocationDonutChart currency (#1631 CX1)', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
    MockEChartsChart.mockClear()
  })

  const buckets = [
    { security_type: 'Equity', asset_class: 'stock', total_value: 1000, percentage: 100, holding_count: 1 },
  ]

  const legendText = () => {
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      legend?: { formatter?: (n: string) => string }
    }
    const formatter = option?.legend?.formatter
    expect(formatter).toBeDefined()
    // The legend key is the aggregated slice label, which securityTypeLabel derives from
    // security_type -- read it back off the series rather than guessing it.
    const series = (MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      series?: Array<{ data?: Array<{ name: string }> }>
    })?.series
    const sliceName = series?.[0]?.data?.[0]?.name ?? ''
    return formatter!(sliceName)
  }

  it('formats slices in the currency the breakdown states', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 1000, buckets, currency_code: 'GBP', is_mixed_currency: false },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    const out = legendText()
    expect(out).toContain('£')
    expect(out).not.toContain('$')
  })

  // The sibling: an unlabelled breakdown keeps today's dollar rendering. Unknown is not a conflict.
  it('keeps the dollar rendering for an unlabelled breakdown', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 1000, buckets, is_mixed_currency: false },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    expect(legendText()).toContain('$')
  })

  it('declares a mixed-currency breakdown instead of labelling it', () => {
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: { total_value: 1000, buckets, is_mixed_currency: true },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<AllocationDonutChart config={DEFAULT_CONFIG} />)

    expect(screen.getByText(/mixed currencies/i)).toBeDefined()
    const out = legendText()
    expect(out).not.toContain('$')
    expect(out).not.toContain('£')
  })
})
