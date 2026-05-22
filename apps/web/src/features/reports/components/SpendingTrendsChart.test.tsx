import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode } from '@/test/mocks/shared-charts'
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
    expect(screen.getByLabelText('Loading trends chart')).toBeDefined()
  })

  it('renders empty state when no periods', () => {
    const emptyData: SpendingTrendsResponse = {
      start_date: '2024-01-01',
      end_date: '2024-03-31',
      top_categories: [],
      periods: [],
    }
    render(<SpendingTrendsChart data={emptyData} loading={false} />)
    expect(screen.getByText(/No trend data/)).toBeDefined()
  })

  it('renders echarts chart with data', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
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

  it('falls back to formatted display name when pfc_primary_display is absent', () => {
    const sparseData: SpendingTrendsResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_categories: ['LOAN_PAYMENTS'],
      periods: [
        {
          period_start: '2024-01-01',
          period_end: '2024-01-31',
          total: 100,
          categories: [{ category: 'LOAN_PAYMENTS', total: 100 } as NonNullable<SpendingTrendPeriod['categories']>[0]],
        },
      ],
    }
    render(<SpendingTrendsChart data={sparseData} loading={false} />)
    const names = JSON.parse(screen.getByTestId('echarts-chart').dataset.seriesNames ?? '[]') as string[]
    expect(names[0]).toBe('Loan Payments')
    expect(names[0]).not.toBe('LOAN_PAYMENTS')
  })

  it('falls back to "Custom Category" when a custom:<uuid> category has no pfc_primary_display', () => {
    const customData: SpendingTrendsResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      top_categories: ['custom:7a93a4f0-dead-beef-1234-abcdef012345'],
      periods: [
        {
          period_start: '2024-01-01',
          period_end: '2024-01-31',
          total: 150,
          categories: [
            {
              category: 'custom:7a93a4f0-dead-beef-1234-abcdef012345',
              total: 150,
            } as NonNullable<SpendingTrendPeriod['categories']>[0],
          ],
        },
      ],
    }
    render(<SpendingTrendsChart data={customData} loading={false} />)
    const names = JSON.parse(screen.getByTestId('echarts-chart').dataset.seriesNames ?? '[]') as string[]
    expect(names[0]).not.toContain('7a93a4f0-dead-beef-1234-abcdef012345')
    expect(names[0]).toBe('Custom Category')
  })

  describe('Dark Mode', () => {
    it('passes isDark=true to EChartsChart when dark mode is active', () => {
      mockUseDarkMode.mockReturnValue(true)
      render(<SpendingTrendsChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
    })

    it('passes isDark=false to EChartsChart when light mode is active', () => {
      mockUseDarkMode.mockReturnValue(false)
      render(<SpendingTrendsChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })
})
