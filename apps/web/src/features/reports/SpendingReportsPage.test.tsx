import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'

import '@/test/mocks/shared-charts'

// Mock CategoryBreakdownChart to expose onCategoryClick in a testable way
mock.module('./components/CategoryBreakdownChart', () => ({
  CategoryBreakdownChart: ({ onCategoryClick }: { onCategoryClick?: (cat: string, label: string) => void }) =>
    React.createElement('div', {
      'data-testid': 'category-chart',
      onClick: () => onCategoryClick?.('FOOD_AND_DRINK', 'Food And Drink'),
    }),
}))

// Mock toggle-group for DateRangePicker
mock.module('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({ children }: { children: React.ReactNode }) =>
    React.createElement('div', { role: 'group' }, children),
  ToggleGroupItem: ({ children, value }: { children: React.ReactNode; value: string }) =>
    React.createElement('button', { 'data-value': value }, children),
}))

// Mock analytics
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: mock(() => mock(() => {})),
}))

import { SpendingReportsPage } from './SpendingReportsPage'

const renderPage = () =>
  render(
    <MemoryRouter>
      <SpendingReportsPage />
    </MemoryRouter>
  )

describe('SpendingReportsPage', () => {
  it('renders page heading', () => {
    renderPage()
    expect(screen.getByText('Spending Reports')).toBeDefined()
  })

  it('renders date range picker', () => {
    renderPage()
    expect(screen.getByRole('group')).toBeDefined()
  })

  it('renders category breakdown section', () => {
    renderPage()
    expect(screen.getByText('Spending by Category')).toBeDefined()
  })

  it('renders spending trends section', () => {
    renderPage()
    expect(screen.getByText('Spending Trends')).toBeDefined()
  })

  it('renders top merchants section', () => {
    renderPage()
    expect(screen.getByText('Top Merchants')).toBeDefined()
  })

  it('renders all three chart/table components', () => {
    renderPage()
    expect(screen.getByTestId('category-chart')).toBeDefined()
    expect(screen.getByTestId('trends-chart')).toBeDefined()
    expect(screen.getByTestId('merchants-table')).toBeDefined()
  })

  it('shows humanized display label in drilldown breadcrumb after category click', () => {
    const { container } = renderPage()
    const chart = screen.getByTestId('category-chart')
    fireEvent.click(chart)
    const heading = container.querySelector('#category-heading')
    expect(heading?.textContent).toContain('Food And Drink')
    expect(heading?.textContent).not.toContain('FOOD_AND_DRINK')
  })
})
