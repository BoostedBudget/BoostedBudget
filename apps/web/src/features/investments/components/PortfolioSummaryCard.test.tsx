/**
 * PortfolioSummaryCard Component Tests
 * Issue #1296: KPI summary tiles for the portfolio tab.
 */

import { describe, it, expect, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { PortfolioSummaryCard } from './PortfolioSummaryCard'
import type { PortfolioSummary } from '@boostedbudget/api-client/models/portfolioSummary'
import { PortfolioSummaryDayChangeUnavailableReason } from '@boostedbudget/api-client/models/portfolioSummaryDayChangeUnavailableReason'

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

// #1631 Gap B: the backend REFUSED to compute day_change (prior snapshot and today's total
// disagree on currency, or either is a currency-blind sum) rather than lacking the data for it.
// day_change/day_change_pct are null on this shape, same as nullDaySummary, but the reason field
// distinguishes "we do not know yet" from "we will never compute this from these two totals".
const currencyMismatchSummary: PortfolioSummary = {
  ...mockSummary,
  day_change: null,
  day_change_pct: null,
  day_change_unavailable_reason: 'currency_mismatch',
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

  // #1631 Gap B: a REFUSAL is not the same claim as missing data, and both must not collapse into
  // the same bare "--". Mirrors HoldingsTable's renderDayChange treatment of the same reason.
  describe('day_change_unavailable_reason (#1631 Gap B)', () => {
    it('explains the refusal instead of showing a bare "--"', () => {
      render(<PortfolioSummaryCard summary={currencyMismatchSummary} />)
      expect(screen.getByText(/currency/i)).toBeDefined()
      expect(screen.queryAllByText('--').length).toBe(0)
    })

    it('does not render a ChangeIndicator for a refused day change', () => {
      render(<PortfolioSummaryCard summary={currencyMismatchSummary} />)
      expect(screen.queryByLabelText(/day change.*versus previous period/i)).toBeNull()
    })

    it('still shows the plain "--" when day_change is merely missing (no refusal reason)', () => {
      render(<PortfolioSummaryCard summary={nullDaySummary} />)
      expect(screen.queryByText(/currency/i)).toBeNull()
    })
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

  // ── #1631 M8a/CX1: currency ─────────────────────────────────────────────────────────────────
  //
  // These REPLACE the characterisation tests that used to live here. Those pinned today's wrong
  // behaviour on purpose -- including `expect(gbpTotal).toBe(usdTotal)`, a tripwire designed to
  // fail the moment the formatter learned the currency -- and said in their own comment that a
  // follow-up should replace them with real assertions rather than work around them. This is that
  // follow-up.
  //
  // The defect they recorded: formatCurrencyFull defaulted to the dollar, so a GBP portfolio
  // rendered "$52,340.50" and a MIXED portfolio rendered a dollar figure that is not a quantity of
  // any currency at all -- the UI asserting a currency the backend had explicitly REFUSED to
  // assert by serving currency_code: null alongside is_mixed_currency: true.
  describe('currency (#1631 CX1)', () => {
    const totalValueText = () =>
      screen.getByText(/total value/i).closest('[data-slot="card"]')
        ?.querySelector('[data-ph-mask]')?.textContent ?? ''

    it('formats a non-dollar portfolio in ITS currency, not the dollar', () => {
      render(<PortfolioSummaryCard summary={foreignCurrencySummary} />)
      const total = totalValueText()
      expect(total).toContain('£')
      expect(total).not.toContain('$')
    })

    // The sibling that keeps the assertion above from passing on a card that simply stopped
    // rendering a symbol: a USD portfolio must still read as dollars.
    it('still formats a dollar portfolio in dollars', () => {
      render(<PortfolioSummaryCard summary={mockSummary} />)
      expect(totalValueText()).toContain('$')
    })

    it('never labels a MIXED total with a currency symbol the backend refused to name', () => {
      render(<PortfolioSummaryCard summary={mixedCurrencySummary} />)
      const total = totalValueText()
      expect(total).not.toContain('$')
      expect(total).not.toContain('£')
    })

    it('warns on the face of the card when the total mixes currencies', () => {
      render(<PortfolioSummaryCard summary={mixedCurrencySummary} />)
      expect(screen.getByText(/mixed currencies/i)).toBeDefined()
    })

    it('shows no mixed-currency warning on a single-currency portfolio', () => {
      render(<PortfolioSummaryCard summary={foreignCurrencySummary} />)
      expect(screen.queryByText(/mixed currencies/i)).toBeNull()
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

// #1631 Codex round 2: the headline went through the currency-aware formatter but the
// ChangeIndicator beneath it did not, so a GBP portfolio labelled the SAME figure as two
// different currencies -- worse than the single wrong currency CX1 set out to fix.
describe('PortfolioSummaryCard change indicators (#1631 Codex r2)', () => {
  it('formats the gain/loss and day-change indicators in the stated currency', () => {
    render(<PortfolioSummaryCard summary={foreignCurrencySummary} />)
    // Two headlines plus the two indicators beneath them. Before the fix the indicators were
    // dollars while the headlines were pounds -- one figure, two currencies.
    expect(screen.getAllByText(/\u00a3/).length).toBeGreaterThan(2)
    expect(screen.queryByText(/\$4,340/)).toBeNull()
    expect(screen.queryByText(/\$230/)).toBeNull()
  })

  it('drops the symbol from the indicators when the currency is unnameable', () => {
    render(<PortfolioSummaryCard summary={mixedCurrencySummary} />)
    expect(screen.queryByText(/\$4,340/)).toBeNull()
    expect(screen.queryByText(/\$230/)).toBeNull()
    expect(screen.queryByText(/\u00a3/)).toBeNull()
  })
})

// #1631 Gap B follow-up, mirroring HoldingsTable.test.tsx's own exhaustiveness pin. The card
// handles exactly one value of day_change_unavailable_reason today, which is correct only because
// the generated enum carries exactly one. Asserted in a TEST rather than a runtime map on purpose
// -- the investments chunk has almost no headroom (see HoldingsTable.test.tsx for the measured
// figure) -- so a second backend reason fails THIS test instead of silently falling through to a
// bare "--" that reads as missing data rather than a refusal.
describe('day-change unavailable reasons are handled exhaustively (#1631 Gap B)', () => {
  it('handles every value the generated enum can carry', () => {
    const backendReasons = Object.values(PortfolioSummaryDayChangeUnavailableReason)
    const handledByCard: PortfolioSummaryDayChangeUnavailableReason[] = ['currency_mismatch']
    expect([...backendReasons].sort()).toEqual([...handledByCard].sort())
  })
})
