import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'

import '@/test/mocks/shared-charts'

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
})
