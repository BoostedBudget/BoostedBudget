/**
 * PortfolioSummaryCard Component Tests
 * Issue #1296: KPI summary tiles for the portfolio tab.
 */

import { describe, it, expect, afterEach } from 'bun:test'
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

// G7-1: day_change is present but pct is null (intraday data partially available)
const partialDaySummary: PortfolioSummary = {
  ...mockSummary,
  day_change: 230.15,
  day_change_pct: null,
}

describe('PortfolioSummaryCard', () => {
  it('renders Total Value stat', () => {
    render(<PortfolioSummaryCard summary={mockSummary} />)
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

  it('shows "--" when day_change is null', () => {
    render(<PortfolioSummaryCard summary={nullDaySummary} />)
    expect(screen.getAllByText('--').length).toBeGreaterThan(0)
  })

  it('does not render ChangeIndicator when day_change is null', () => {
    render(<PortfolioSummaryCard summary={nullDaySummary} />)
    expect(screen.queryByLabelText(/day change.*versus previous period/i)).toBeNull()
  })

  it('renders loading skeleton when summary is undefined', () => {
    const { container } = render(<PortfolioSummaryCard summary={undefined} />)
    const skeletons = container.querySelectorAll('[data-slot="skeleton"], [aria-label*="Loading"], .animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  // R7-1: Show dollar even when day_change_pct is null (mirrors Gain/Loss behavior)
  it('shows dollar amount when day_change is set but day_change_pct is null', () => {
    render(<PortfolioSummaryCard summary={partialDaySummary} />)
    expect(screen.getByText('$230.15')).toBeDefined()
    expect(screen.queryAllByText('--').length).toBe(0)
  })

  it('suppresses ChangeIndicator when day_change is set but day_change_pct is null', () => {
    render(<PortfolioSummaryCard summary={partialDaySummary} />)
    expect(screen.queryByLabelText(/day change.*versus previous period/i)).toBeNull()
  })

  it('shows "--" when day_change is null even if day_change_pct is non-null', () => {
    const noChangeSummary: PortfolioSummary = { ...mockSummary, day_change: null, day_change_pct: 1.2 }
    render(<PortfolioSummaryCard summary={noChangeSummary} />)
    expect(screen.getAllByText('--').length).toBeGreaterThan(0)
  })

  describe('dark mode', () => {
    afterEach(() => {
      document.documentElement.classList.remove('dark')
    })

    it('renders and applies data-ph-mask in dark mode context', () => {
      document.documentElement.classList.add('dark')
      const { container } = render(<PortfolioSummaryCard summary={mockSummary} />)
      expect(screen.getByText(/total value/i)).toBeDefined()
      const masked = container.querySelectorAll('[data-ph-mask]')
      expect(masked.length).toBeGreaterThan(0)
    })
  })
})
