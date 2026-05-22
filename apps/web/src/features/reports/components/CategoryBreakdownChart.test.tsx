import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/shared-charts'
import { mockUseDarkMode, MockEChartsChart } from '@/test/mocks/shared-charts'
import { CategoryBreakdownChart } from './CategoryBreakdownChart'
import type { SpendingByCategoryResponse } from '@boostedbudget/api-client/models'

const mockData: SpendingByCategoryResponse = {
  start_date: '2024-01-01',
  end_date: '2024-01-31',
  total_spent: 500,
  categories: [
    { category: 'FOOD_AND_DRINK', pfc_primary_display: 'Food And Drink', total: 300, percentage: 60, transaction_count: 20 },
    { category: 'TRANSPORTATION', pfc_primary_display: 'Transportation', total: 200, percentage: 40, transaction_count: 8 },
  ],
}

describe('CategoryBreakdownChart', () => {
  it('renders loading state', () => {
    render(<CategoryBreakdownChart data={null} loading={true} />)
    expect(screen.getByLabelText('Loading chart')).toBeInTheDocument()
  })

  it('renders empty state when no categories', () => {
    const emptyData: SpendingByCategoryResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_spent: 0,
      categories: [],
    }
    render(<CategoryBreakdownChart data={emptyData} loading={false} />)
    expect(screen.getByText(/No spending data/)).toBeInTheDocument()
  })

  it('renders echarts chart with data', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    expect(screen.getByTestId('echarts-chart')).toBeInTheDocument()
  })

  it('chart has series type pie', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    const chart = screen.getByTestId('echarts-chart')
    const types = JSON.parse(chart.dataset.seriesTypes ?? '[]') as string[]
    expect(types[0]).toBe('pie')
  })

  it('renders total spent amount', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    expect(screen.getByText('Total:')).toBeInTheDocument()
  })

  it('calls onCategoryClick with raw category field (not display label) when slice clicked', () => {
    const onCategoryClick = mock(() => {})
    render(
      <CategoryBreakdownChart
        data={mockData}
        loading={false}
        onCategoryClick={onCategoryClick}
      />
    )
    const trigger = screen.getByTestId('echarts-point-0')
    fireEvent.click(trigger)
    expect(onCategoryClick).toHaveBeenCalledTimes(1)
    const [category, displayLabel] = onCategoryClick.mock.calls[0] as [string, string]
    // category should be the raw category ID, not the display label
    expect(category).toBe('FOOD_AND_DRINK')
    expect(displayLabel).toBe('Food And Drink')
  })

  it('passes custom-category id correctly (not display label) to onCategoryClick', () => {
    const onCategoryClick = mock(() => {})
    const customData: SpendingByCategoryResponse = {
      start_date: '2024-01-01',
      end_date: '2024-01-31',
      total_spent: 150,
      categories: [
        {
          category: 'custom:7a93a4f0-dead-beef-1234-abcdef012345',
          pfc_primary_display: 'My Coffee Budget',
          total: 150,
          percentage: 100,
          transaction_count: 5,
        },
      ],
    }
    render(<CategoryBreakdownChart data={customData} loading={false} onCategoryClick={onCategoryClick} />)
    fireEvent.click(screen.getByTestId('echarts-point-0'))
    const [category, displayLabel] = onCategoryClick.mock.calls[0] as [string, string]
    expect(category).toBe('custom:7a93a4f0-dead-beef-1234-abcdef012345')
    expect(displayLabel).toBe('My Coffee Budget')
  })


  it('tooltip formatter escapes malicious labels in fallback path (XSS regression — #1169)', () => {
    render(<CategoryBreakdownChart data={mockData} loading={false} />)
    const lastCall = MockEChartsChart.mock.calls.at(-1)
    const option = lastCall?.[0]?.option as { tooltip?: { formatter?: (p: unknown) => string } }
    // Fallback path: value/percent not numbers (e.g. zero-total category)
    const formatted = option.tooltip!.formatter!({ name: '<img src=x onerror=alert(1)>', value: undefined, percent: undefined })
    expect(formatted).toContain('&lt;img')
    expect(formatted).not.toContain('<img src')
  })

  describe('Dark Mode', () => {
    it('reflects dark mode state in isDark prop', () => {
      mockUseDarkMode.mockReturnValue(true)
      const { unmount } = render(<CategoryBreakdownChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('true')
      unmount()

      mockUseDarkMode.mockReturnValue(false)
      render(<CategoryBreakdownChart data={mockData} loading={false} />)
      expect(screen.getByTestId('echarts-chart').dataset.isDark).toBe('false')
    })
  })
})
