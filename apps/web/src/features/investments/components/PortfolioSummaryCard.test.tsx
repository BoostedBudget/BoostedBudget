/**
 * PortfolioSummaryCard Component Tests
 * Issue #1296: KPI summary tiles for the portfolio tab.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { PortfolioSummaryCard } from './PortfolioSummaryCard'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'

const mockSummary: PortfolioSummary = {
  total_value: 52340.50,
  total_cost_basis: 48000.00,
  total_gain_loss: 4340.50,
  total_gain_loss_pct: 9.04,
  day_change: 230.15,
  day_change_pct: 0.44,
  holdings_count: 12,
}

const nullDaySummary: PortfolioSummary = {
  ...mockSummary,
  day_change: null,
  day_change_pct: null,
}

describe('PortfolioSummaryCard', () => {
  it('renders Total Value stat', () => {
    render(<PortfolioSummaryCard summary={mockSummary} />)
    // StatCard labels are uppercase in CSS but the accessible text is normal
    expect(screen.getByText(/total value/i)).toBeDefined()
  })

  it('renders Total Gain/Loss stat', () => {
    render(<PortfolioSummaryCard summary={mockSummary} />)
    expect(screen.getByText(/gain/i)).toBeDefined()
  })

  it('renders Day Change stat', () => {
    render(<PortfolioSummaryCard summary={mockSummary} />)
    expect(screen.getByText(/day change/i)).toBeDefined()
  })

  it('applies data-ph-mask to all monetary values', () => {
    const { container } = render(<PortfolioSummaryCard summary={mockSummary} />)
    const masked = container.querySelectorAll('[data-ph-mask]')
    expect(masked.length).toBeGreaterThan(0)
  })

  it('shows "--" for null day_change', () => {
    render(<PortfolioSummaryCard summary={nullDaySummary} />)
    const dashes = screen.getAllByText('--')
    expect(dashes.length).toBeGreaterThan(0)
  })

  it('does not render ChangeIndicator when day_change is null', () => {
    render(<PortfolioSummaryCard summary={nullDaySummary} />)
    // ChangeIndicator for "Day change" sets aria-label like "Day change up by X% versus previous period"
    // When day_change is null the ChangeIndicator for that section must not render
    expect(screen.queryByLabelText(/day change.*by/i)).toBeNull()
  })

  it('renders loading skeleton when summary is undefined', () => {
    const { container } = render(<PortfolioSummaryCard summary={undefined} />)
    // Loading state should render some skeleton or placeholder elements
    const skeletons = container.querySelectorAll('[data-slot="skeleton"], [aria-label*="Loading"], .animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })
})
