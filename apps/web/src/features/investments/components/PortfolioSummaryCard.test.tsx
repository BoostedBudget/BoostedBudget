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
  // #1631 M8a: a single-currency portfolio, stated rather than assumed.
  currency_code: 'USD',
  is_mixed_currency: false,
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

// #1631 M8a. The server can now tell us that these totals are a CURRENCY-BLIND SUM: holdings
// denominated in two or more different currencies, added together, because there is no FX rate
// source in this system to convert them with. currency_code is null in that case precisely so
// nothing labels the sum with a currency it is not in.
const mixedCurrencySummary: PortfolioSummary = {
  ...mockSummary,
  currency_code: undefined,
  is_mixed_currency: true,
}

// A single-currency portfolio in a currency that is NOT the dollar. Distinct from the mixed case:
// this total is exactly right, it is simply not in USD.
const foreignCurrencySummary: PortfolioSummary = {
  ...mockSummary,
  currency_code: 'GBP',
  is_mixed_currency: false,
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

  // G15-1: StatCard renders `value` inside <p data-slot="stat-value"> (phrasing-only context).
  // PortfolioSummaryCard must not pass block-level <div> wrappers as value — that produces
  // invalid <p><div> nesting, React validateDOMNesting warnings, and browser auto-close.
  it('renders no invalid <div> block markup inside the StatCard value <p> (HTML validity)', () => {
    const { container } = render(<PortfolioSummaryCard summary={mockSummary} />)
    const valueEls = container.querySelectorAll('p[data-slot="stat-value"]')
    expect(valueEls.length).toBe(3)
    valueEls.forEach((p) => expect(p.querySelector('div')).toBeNull())
  })

  // ── #1631 M8a: currency ─────────────────────────────────────────────────────────────────────
  //
  // ⚠ THESE ARE CHARACTERISATION TESTS. They pin what this card does TODAY and record that it is
  // wrong, in the same spirit as the close_price_as_of NULL companion test on the backend: they
  // pass both before and after the eventual fix, and that is the point. The server-side work in
  // #1631 M8a made the currency situation KNOWABLE (currency_code + is_mixed_currency on the
  // response); surfacing it in the UI is a deliberate follow-up, because the generated API client
  // did not carry these fields until this change landed.
  //
  // What is still wrong, stated plainly so the follow-up has a specification:
  //   1. formatCurrencyFull hard-codes the dollar, so a GBP portfolio renders "$52,340.50" and a
  //      mixed one renders a dollar figure that is not a quantity of any currency at all.
  //   2. is_mixed_currency is not surfaced, so the user is not told the total mixes currencies.
  //
  // The assertions below are deliberately NARROW — they check only that the total still renders
  // and that the card does not crash on either shape. A follow-up that adds a badge or switches
  // the formatter will NOT break them; it should instead REPLACE them with real assertions.
  describe('currency (#1631 M8a — flag served, not yet surfaced)', () => {
    it('renders a mixed-currency portfolio without crashing', () => {
      render(<PortfolioSummaryCard summary={mixedCurrencySummary} />)
      expect(screen.getByText(/total value/i)).toBeDefined()
    })

    it('renders a portfolio priced in a non-dollar currency without crashing', () => {
      render(<PortfolioSummaryCard summary={foreignCurrencySummary} />)
      expect(screen.getByText(/total value/i)).toBeDefined()
    })

    it('formats the total identically whatever the currency says — the known gap', () => {
      const usd = render(<PortfolioSummaryCard summary={mockSummary} />)
      const usdTotal = usd.container.querySelector('[data-ph-mask]')?.textContent
      usd.unmount()

      const gbp = render(<PortfolioSummaryCard summary={foreignCurrencySummary} />)
      const gbpTotal = gbp.container.querySelector('[data-ph-mask]')?.textContent
      gbp.unmount()

      const mixed = render(<PortfolioSummaryCard summary={mixedCurrencySummary} />)
      const mixedTotal = mixed.container.querySelector('[data-ph-mask]')?.textContent

      // Identical today, and that is the defect: the same string is served for a dollar total, a
      // pound total, and a total that is in no single currency. Replace this with a real
      // assertion when the formatter learns the currency.
      expect(gbpTotal).toBe(usdTotal)
      expect(mixedTotal).toBe(usdTotal)
    })
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
