import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { NetWorthDisplay } from './NetWorthDisplay'
import type { NetWorthSummary } from '../types'

function todayString(): string {
  return new Date().toISOString().slice(0, 10)
}

const baseSummary: NetWorthSummary = {
  as_of_date: '2024-03-31',
  net_worth: 10000,
  total_assets: 15000,
  total_liabilities: 5000,
  by_category: {},
}

const summaryWithComparison: NetWorthSummary = {
  ...baseSummary,
  period_comparison: {
    previous_net_worth: 9000,
    previous_assets: 14000,
    previous_liabilities: 5000,
    net_worth_change: 1000,
    net_worth_change_percent: 11.11,
  },
}

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('NetWorthDisplay', () => {
  it('renders net worth value', () => {
    renderWithRouter(<NetWorthDisplay summary={baseSummary} />)
    expect(screen.getByText(/\$10,000/)).toBeDefined()
  })

  it('uses text-success for positive net worth', () => {
    const { container } = renderWithRouter(<NetWorthDisplay summary={baseSummary} />)
    const headline = container.querySelector('[data-ph-mask]')
    expect(headline?.className).toContain('text-success')
  })

  it('uses text-destructive for negative net worth', () => {
    const neg = { ...baseSummary, net_worth: -5000 }
    const { container } = renderWithRouter(<NetWorthDisplay summary={neg} />)
    const headline = container.querySelector('[data-ph-mask]')
    expect(headline?.className).toContain('text-destructive')
  })

  it('does not render a link when linkTo is not provided', () => {
    renderWithRouter(<NetWorthDisplay summary={baseSummary} />)
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('wraps headline in a link when linkTo is provided', () => {
    renderWithRouter(<NetWorthDisplay summary={baseSummary} linkTo="/net-worth" />)
    const link = screen.getByRole('link')
    expect(link.getAttribute('href')).toBe('/net-worth')
  })

  it('shows period comparison when present', () => {
    renderWithRouter(<NetWorthDisplay summary={summaryWithComparison} />)
    expect(screen.getByText(/\$1,000/)).toBeDefined()
  })

  it('does not use raw green-600 or red-600 classes', () => {
    const { container } = renderWithRouter(<NetWorthDisplay summary={baseSummary} />)
    expect(container.innerHTML).not.toContain('green-600')
    expect(container.innerHTML).not.toContain('red-600')
  })

  it('renders as-of date muted under headline', () => {
    renderWithRouter(<NetWorthDisplay summary={baseSummary} />)
    expect(screen.getByText(/as of Mar 31, 2024/i)).toBeDefined()
  })

  it('appends (may be stale) when as_of_date is older than 24 hours', () => {
    const stale = { ...baseSummary, as_of_date: '2020-01-01' }
    renderWithRouter(<NetWorthDisplay summary={stale} />)
    expect(screen.getByText(/may be stale/i)).toBeDefined()
  })

  it('does not append (may be stale) when as_of_date is recent', () => {
    const fresh = { ...baseSummary, as_of_date: todayString() }
    renderWithRouter(<NetWorthDisplay summary={fresh} />)
    expect(screen.queryByText(/may be stale/i)).toBeNull()
  })
})
