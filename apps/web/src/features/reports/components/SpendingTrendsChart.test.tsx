import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

import '@/test/mocks/shared-charts'
import { SpendingTrendsChart } from './SpendingTrendsChart'
import type { SpendingTrendsResponse } from '@boostedbudget/api-client/models'

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

  it('renders chart with data', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    expect(screen.getByTestId('plotly-chart')).toBeDefined()
  })

  it('renders the correct number of traces', () => {
    render(<SpendingTrendsChart data={mockData} loading={false} />)
    const chart = screen.getByTestId('plotly-chart')
    expect(chart.getAttribute('data-trace-count')).toBe('2')
  })
})
