import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
import { SpendingTrendsChart } from './SpendingTrendsChart'
import type { SpendingTrendsResponse, SpendingTrendPeriod } from '@boostedbudget/api-client/models'

const mockData: SpendingTrendsResponse = {
  start_date: '2024-01-01',
  end_date: '2024-03-31',
  top_categories: ['FOOD_AND_DRINK', 'TRANSPORTATION'],
  periods: [
    {
      period_start: '2024-01-01',
      period_end: '2024-01-31',
      total: 280,
      categories: [
        { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food And Drink', total: 200 },
        { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 80 },
      ],
    },
    {
      period_start: '2024-02-01',
      period_end: '2024-02-29',
      total: 320,
      categories: [
        { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food And Drink', total: 230 },
        { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 90 },
      ],
    },
  ],
}

describe('SpendingTrendsChart', () => {
  it('renders loading state', () => {
    render(<SpendingTrendsChart data={null} loading={true} />)
    expect(screen.getByLabelText('Loading trends chart')).toBeInTheDocument()
  })

  it('renders empty state when no periods', () => {
    const emptyData: SpendingTrendsResponse = {
      start_date: '2024-01-01',
      end_date: '2024-03-31',
      top_categories: [],
      periods: [],
    }
    render(<SpendingTrendsChart data={emptyData} loading={false} />)
    expect(screen.getByText(/No trend data/)).toBeInTheDocument()
  })

  it('renders echarts chart with data', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
  })

  it('renders the correct number of series', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    const chart = screen.getByTestId('echarts-chart')
    expect(chart.dataset.seriesCount).toBe('2')
  })

  it('all series have type bar (stacked)', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    const types = JSON.parse(screen.getByTestId('echarts-chart').dataset.seriesTypes ?? '[]') as string[]
    expect(types.every((t) => t === 'bar')).toBe(true)
  })

  it.each([
    {
      name: 'falls back to formatted display name when pfc_primary_display is absent',
      category: 'LOAN_PAYMENTS',
      expected: 'Loan Payments',
    },
    {
      name: 'falls back to "Custom Category" when custom:<uuid> has no pfc_primary_display',
      category: 'custom:7a93a4f0-dead-beef-1234-abcdef012345',
      expected: 'Custom Category',
    },
  ])('$name', ({ category, expected }) => {
    type CategoryType = NonNullable<SpendingTrendPeriod['categories']>[0]
    const testData: SpendingTrendsResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_categories: [category],
      periods: [
        {
          period_start: '2024-01-01',
          period_end: '2024-01-31',
          total: 150,
          categories: [{ category, total: 150 } as CategoryType],
        },
      ],
    }
    render(<SpendingTrendsChart data={testData} loading={false} />)
    const names = JSON.parse(screen.getByTestId('echarts-chart').dataset.seriesNames ?? '[]') as string[]
    expect(names[0]).toBe(expected)
  })

  describe('Dark Mode', () => {
    it('reflects dark mode state in isDark prop', () => {
      mockUseDarkMode.mockReturnValue(true)
      const { unmount } = render(<SpendingTrendsChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
      unmount()

      mockUseDarkMode.mockReturnValue(false)
      render(<SpendingTrendsChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })

  it('tooltip formatter escapes malicious series names (XSS regression — #1169)', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    const option = MockEChartsChart.mock.calls.at(-1)?.[0]?.option as {
      tooltip?: { formatter?: (p: unknown) => string }
    }
    const formatted = option.tooltip!.formatter!([{ seriesName: '<img src=x onerror=alert(1)>', value: 200 }])
    expect(formatted).not.toMatch(/<(img|script|svg)/i)
    expect(formatted).toContain('&lt;img')
  })
})
