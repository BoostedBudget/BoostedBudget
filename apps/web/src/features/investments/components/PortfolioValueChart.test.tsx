/**
 * PortfolioValueChart Component Tests
 * Issue #1296: Line chart showing portfolio value over time.
 * TDD: tests written before implementation.
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
import { mockUseGetPortfolioHistory } from '@/test/mocks/investments-api'
import { PortfolioValueChart } from './PortfolioValueChart'

const DATE_RANGE = { start_date: '2024-01-01', end_date: '2024-03-31' }

describe('PortfolioValueChart', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
    MockEChartsChart.mockClear()
  })

  it('renders a line chart with history data', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
  })

  it('shows not-enough-data message when fewer than 2 points', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history: [{ date: '2024-01-01', total_value: 1000 }] },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByText(/not enough data/i)).toBeDefined()
  })

  it('shows not-enough-data message when history is null', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history: null },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByText(/not enough data/i)).toBeDefined()
  })

  it('shows not-enough-data message when data is undefined', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByText(/not enough data/i)).toBeDefined()
  })

  it('shows loading state', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('does not add data-ph-mask directly on the container div', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    const { container } = render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    // EChartsChart wrapper handles data-ph-mask internally.
    // The outer container div must not duplicate it.
    const outerDiv = container.firstElementChild as HTMLElement
    expect(outerDiv?.hasAttribute('data-ph-mask')).toBe(false)
  })

  it('renders a line series type', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    const chart = screen.getByTestId('echarts-chart')
    const types = JSON.parse(chart.dataset.seriesTypes ?? '[]') as string[]
    expect(types[0]).toBe('line')
  })

  // R4-2: C11 — both isDark branches must be explicitly asserted (not just dark).
  it('passes isDark=true to EChartsChart in dark mode', () => {
    mockUseDarkMode.mockReturnValue(true)
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
  })

  it('passes isDark=false to EChartsChart in light mode', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2024-01-01', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
  })

  // R4-3: C5 — prove the dateRange prop is forwarded to useGetPortfolioHistory.
  // Without this test, a mismatch between dateRange prop and API params would be silent.
  it('forwards dateRange start_date and end_date to useGetPortfolioHistory', () => {
    const customRange = { start_date: '2023-06-01', end_date: '2023-09-30' }
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '2023-06-01', total_value: 5000 },
          { date: '2023-09-30', total_value: 5500 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    mockUseGetPortfolioHistory.mockClear()
    render(<PortfolioValueChart dateRange={customRange} />)
    // The mock captures the args passed to useGetPortfolioHistory on render
    const callArg = mockUseGetPortfolioHistory.mock.calls[0]?.[0] as Record<string, unknown>
    expect(callArg).toBeDefined()
    expect(callArg.start_date).toBe('2023-06-01')
    expect(callArg.end_date).toBe('2023-09-30')
  })

  it('tooltip formatter escapes malicious names (XSS guard)', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: {
        history: [
          { date: '<img src=x>', total_value: 1000 },
          { date: '2024-01-08', total_value: 1100 },
        ],
      },
      isLoading: false,
      isError: false,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    // Anchor: exactly one render call — prevents reading a stale call from a prior test.
    expect(MockEChartsChart).toHaveBeenCalledTimes(1)
    const lastCall = MockEChartsChart.mock.calls.at(-1)
    const option = lastCall?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    // Fail-stop: if the formatter is removed/renamed the guard can never fire silently.
    const formatter = option?.tooltip?.formatter
    expect(formatter).toBeDefined()
    expect(typeof formatter).toBe('function')
    const formatted = formatter!([{ name: '<script>alert(1)</script>', value: 1000 }])
    expect(formatted).not.toMatch(/<script/i)
    expect(formatted).toContain('&lt;script')
  })
})

// Issue #1569 T9.5 — a failed read is not "not enough data". The latter tells the user their
// portfolio has no history in this range, which is a claim about their money the client cannot
// make when the request never returned.
describe('PortfolioValueChart failed read', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
  })

  // The API mock is process-global; leave it as other files expect to find it.
  afterEach(() => {
    mockUseGetPortfolioHistory.mockReturnValue({ data: undefined, isLoading: false, isError: false })
  })

  it('reports the failed read and does not claim there is not enough data', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    })
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    expect(screen.getByRole('alert')).toBeDefined()
    expect(screen.getByText(/couldn't load/i)).toBeDefined()
    expect(screen.queryByText(/not enough data/i)).toBeNull()
    expect(screen.queryByTestId('echarts-chart')).toBeNull()
  })
})

// #1631 CX1: the tooltip formatted every point as dollars. The series carries a RANGE-level
// currency verdict -- a line chart has one y-axis with one unit, so the verdict cannot be
// per-point -- and this component threw it away.
describe('PortfolioValueChart currency (#1631 CX1)', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
    MockEChartsChart.mockClear()
  })

  const tooltipText = (value: number) => {
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatter = option?.tooltip?.formatter
    expect(formatter).toBeDefined()
    return formatter!([{ name: '2024-01-08', value }])
  }

  const history = [
    { date: '2024-01-01', total_value: 1000 },
    { date: '2024-01-08', total_value: 1100 },
  ]

  it('formats tooltip values in the currency the series states', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history, currency_code: 'GBP', is_mixed_currency: false },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)

    const out = tooltipText(1100)
    expect(out).toContain('£')
    expect(out).not.toContain('$')
  })

  // The sibling: an unlabelled series keeps today's dollar rendering, because unknown is not a
  // conflict and that is the state of every snapshot written before migration 000202.
  it('keeps the dollar rendering for an unlabelled series', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history, is_mixed_currency: false },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)

    expect(tooltipText(1100)).toContain('$')
  })

  // A currency-blind series must not be labelled with any currency, and the reader has to be told
  // why the symbol went away.
  it('declares a mixed-currency series instead of labelling it', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history, is_mixed_currency: true },
      isLoading: false,
      isError: false,
      // The shared mock factory infers `data: undefined`, so every seeded payload in this file
      // is a type error against it. Cast rather than widen the ceiling (#1631 CX1).
    } as never)
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)

    expect(screen.getByText(/mixed currencies/i)).toBeDefined()
    const out = tooltipText(1100)
    expect(out).not.toContain('$')
    expect(out).not.toContain('£')
  })
})

// #1631 Codex round 2: the tooltip was made currency-aware but the Y-AXIS was not. This component
// spreads getCurrencyOption(isDark) and overrides only `tooltip` and `xAxis`, so the inherited
// yAxis.axisLabel.formatter (shared/charts/options/currency.ts) kept formatting every tick as
// dollars. A GBP portfolio therefore rendered pound tooltips against a dollar axis -- two
// currencies asserted about one series, which is worse than the single wrong currency it replaced.
describe('PortfolioValueChart y-axis currency (#1631 Codex r2)', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
    MockEChartsChart.mockClear()
  })

  const axisTick = (value: number) => {
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      yAxis?: { axisLabel?: { formatter?: (v: unknown) => string } }
    }
    const formatter = option?.yAxis?.axisLabel?.formatter
    expect(formatter).toBeDefined()
    return formatter!(value)
  }

  const history = [
    { date: '2024-01-01', total_value: 1000 },
    { date: '2024-01-08', total_value: 1100 },
  ]

  it('formats axis ticks in the currency the series states', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history, currency_code: 'GBP', is_mixed_currency: false },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof mockUseGetPortfolioHistory>)
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    const tick = axisTick(1100)
    expect(tick).toContain('\u00a3')
    expect(tick).not.toContain('$')
  })

  it('drops the symbol on the axis when the currency is unnameable', () => {
    mockUseGetPortfolioHistory.mockReturnValue({
      data: { history, currency_code: null, is_mixed_currency: true },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof mockUseGetPortfolioHistory>)
    render(<PortfolioValueChart dateRange={DATE_RANGE} />)
    const tick = axisTick(1100)
    expect(tick).not.toContain('$')
    expect(tick).not.toContain('\u00a3')
  })
})
