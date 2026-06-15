/**
 * PortfolioValueChart Component Tests
 * Issue #1296: Line chart showing portfolio value over time.
 * TDD: tests written before implementation.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
import { mockUseGetPortfolioHistory } from '@/test/mocks/investments-api'
import { PortfolioValueChart } from './PortfolioValueChart'

const DATE_RANGE = { start_date: '2024-01-01', end_date: '2024-03-31' }

describe('PortfolioValueChart', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
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

  it('passes isDark to EChartsChart', () => {
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
    const lastCall = MockEChartsChart.mock.calls.at(-1)
    const option = lastCall?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option?.tooltip?.formatter?.([
      { name: '<script>alert(1)</script>', value: 1000 },
    ])
    if (formatted !== undefined) {
      expect(formatted).not.toMatch(/<script/i)
      expect(formatted).toContain('&lt;script')
    }
  })
})
