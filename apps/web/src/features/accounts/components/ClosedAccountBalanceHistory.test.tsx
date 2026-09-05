/**
 * ClosedAccountBalanceHistory tests — Issue #1569.
 *
 * Privacy policy §8.2a tells the user a removed linked account's "transaction, balance, and
 * holdings history is retained ... so you can still view it". The BALANCE third was false for the
 * whole life of the product: the rows sat in balance_snapshots and no per-account reader existed.
 * These tests are what stop that promise going stale again from the UI side; the source-level
 * guard that ties the SENTENCE to a reader lives in src/legal/PrivacyPolicy.test.tsx.
 *
 * All four states are named: loading, error, empty, data.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ClosedAccountBalanceHistory } from './ClosedAccountBalanceHistory'
import { mockUseGetAccountBalanceHistory } from '@/test/mocks/accounts-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import type { AccountBalanceHistoryPoint } from '@boostedbudget/api-client/models/accountBalanceHistoryPoint'

const point = (o: Partial<AccountBalanceHistoryPoint> = {}): AccountBalanceHistoryPoint => ({
  date: '2026-03-04',
  current_balance: 2500,
  available_balance: 2300,
  currency_code: 'USD',
  ...o,
})

function withPoints(points: AccountBalanceHistoryPoint[]) {
  mockUseGetAccountBalanceHistory.mockReturnValue({
    data: { account_id: 'acct-closed', points },
    isLoading: false,
    isError: false,
    error: null,
  })
}

function renderIt(props: Partial<{ accountId: string; accountType: string; closedOn: string }> = {}) {
  return render(
    <ClosedAccountBalanceHistory
      accountId="acct-closed"
      accountType="depository"
      // Deliberately a DIFFERENT day from the point date below: with both the same, a mutant that
      // rendered the wrong one still produced the expected text.
      closedOn="2026-03-10T00:00:00Z"
      {...props}
    />
  )
}

beforeEach(() => {
  mockTrackEvent.mockClear()
  withPoints([point()])
})

describe('ClosedAccountBalanceHistory (#1569)', () => {
  it('renders the retained day-by-day balances', () => {
    withPoints([
      point({ date: '2026-03-04', current_balance: 2500, available_balance: 2300 }),
      point({ date: '2026-03-05', current_balance: 1800, available_balance: 1700 }),
    ])
    renderIt()

    expect(screen.getByText('Mar 4, 2026')).toBeInTheDocument()
    expect(screen.getByText('Mar 5, 2026')).toBeInTheDocument()
    expect(screen.getByText('$2,500.00')).toBeInTheDocument()
    expect(screen.getByText('$1,800.00')).toBeInTheDocument()
    expect(screen.getByText('$2,300.00')).toBeInTheDocument()
  })

  // The whole two-universes rule, stated to the user. A retained figure must never read as a
  // current one.
  it('says the figures are historical, and names the closure date', () => {
    renderIt()
    expect(screen.getByText(/not what the account holds today/i)).toBeInTheDocument()
    expect(screen.getByText(/closed on Mar 10, 2026/i)).toBeInTheDocument()
  })

  // Never colour alone: the badge is a tint on a WORD that carries the state by itself.
  it('carries the closed state as text, not as colour', () => {
    renderIt()
    expect(screen.getByText('Closed account')).toBeInTheDocument()
  })

  it('masks both money columns and does not mask the date', () => {
    const { container } = renderIt()
    const masked = container.querySelectorAll('td[data-ph-mask]')
    // One row, two money columns. DataTable masks a <td> ONLY when its column id is listed, so a
    // column left off the consumer's maskedColumnIds gets no attribute at all.
    expect(masked.length).toBe(2)
    const maskedText = Array.from(masked).map((td) => td.textContent)
    expect(maskedText).toContain('$2,500.00')
    expect(maskedText).toContain('$2,300.00')
    expect(maskedText).not.toContain('Mar 4, 2026')
  })

  // An attribute is recorded verbatim by session replay and cannot be covered by any mask, so no
  // aria-label anywhere in this component may carry an amount.
  it('puts no amount in any attribute', () => {
    // TWO points on purpose. The chart — the one element here that carries a descriptive
    // aria-label — only renders with more than one point, so a single-point fixture made this
    // assertion vacuous: a mutant that put a formatted amount straight into that label survived it.
    withPoints([
      point({ date: '2026-03-04', current_balance: 2500, available_balance: 2300 }),
      point({ date: '2026-03-05', current_balance: 1800, available_balance: 1700 }),
    ])
    const { container } = renderIt()
    expect(container.querySelectorAll('[aria-label]').length).toBeGreaterThan(0)
    for (const el of Array.from(container.querySelectorAll('[aria-label]'))) {
      expect(el.getAttribute('aria-label') ?? '').not.toMatch(/\d[\d,]*\.\d{2}|\$\s*\d/)
    }
  })

  // A day the provider reported no balance for must render as absent, never as zero: zero claims
  // the account held nothing.
  it('renders an unreported balance as absent rather than as zero', () => {
    withPoints([point({ current_balance: 1200, available_balance: undefined })])
    renderIt()
    expect(screen.getByText('$1,200.00')).toBeInTheDocument()
    expect(screen.queryByText('$0.00')).toBeNull()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows a loading state while the read is in flight', () => {
    mockUseGetAccountBalanceHistory.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
    })
    renderIt()
    expect(screen.getByRole('status', { name: /loading retained balance history/i })).toBeInTheDocument()
  })

  // A failed read must never fall through to the empty state: "we kept nothing" is a claim about
  // the user's money, and making it wrongly says the retention failed when it did not.
  it('shows an error that does not claim the history was lost', () => {
    mockUseGetAccountBalanceHistory.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
      error: new Error('boom'),
    })
    renderIt()
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent(/could not load/i)
    expect(alert).toHaveTextContent(/still kept/i)
    expect(screen.queryByText(/no day-by-day balances were recorded/i)).toBeNull()
  })

  it('states an empty history as a fact, and says nothing was deleted', () => {
    withPoints([])
    renderIt()
    const note = screen.getByRole('note')
    expect(note).toHaveTextContent(/no day-by-day balances were recorded/i)
    expect(note).toHaveTextContent(/nothing was deleted/i)
  })

  // The chart is a canvas and is invisible to assistive tech, so the table is the accessible
  // rendering of the same numbers — and it must be present whether or not the chart is.
  it('renders the rows even when there are too few points to chart', () => {
    withPoints([point()])
    const { container } = renderIt()
    expect(container.querySelector('table')).not.toBeNull()
    expect(screen.getByText('Mar 4, 2026')).toBeInTheDocument()
  })

  // ── The measurement (#1569) ──────────────────────────────────────────────────────────────
  //
  // A VIEW event, not a change-only one: the thesis of #1569 is what the user can SEE, so the
  // measurement that has to exist answers "did anyone actually go and look at the money we kept?".
  it('fires a VIEW event once the read resolves', () => {
    renderIt()
    expect(mockTrackEvent).toHaveBeenCalledWith('account_balance_history_viewed', {
      account_type: 'depository',
      retained_days: 1,
    })
  })

  it('fires the VIEW event even when nothing was retained', () => {
    withPoints([])
    renderIt()
    expect(mockTrackEvent).toHaveBeenCalledWith('account_balance_history_viewed', {
      account_type: 'depository',
      retained_days: 0,
    })
  })

  it('does not fire the VIEW event while the read is still loading', () => {
    mockUseGetAccountBalanceHistory.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
      error: null,
    })
    renderIt()
    expect(mockTrackEvent).not.toHaveBeenCalled()
  })

  // The event is a rate, not a record: no dates, no amounts, no account identity.
  it('sends no amount, no date and no account id with the event', () => {
    renderIt()
    // mockTrackEvent is an untyped shared stub, so bun infers its `calls` as `[][]`. Narrowed
    // here rather than left to add two errors to the test-typecheck ratchet.
    const calls = mockTrackEvent.mock.calls as unknown as Array<[string, Record<string, unknown>]>
    const props = calls.find((c) => c[0] === 'account_balance_history_viewed')?.[1]
    expect(JSON.stringify(props)).not.toContain('acct-closed')
    expect(JSON.stringify(props)).not.toContain('2026-03')
    expect(JSON.stringify(props)).not.toContain('2500')
  })
})

// Issue #1569 T7-FIX, finding B. The API carries currency_code per point precisely so a retained
// figure is never re-denominated, and this table dropped it: formatCurrencyFull hardcoded USD, so a
// 1000 EUR balance rendered as $1,000.00. On a screen whose entire purpose is showing retained
// truth, that is a wrong number, not a missing one.
describe('ClosedAccountBalanceHistory currency (#1569)', () => {
  it('renders a non-USD point in its own currency, not as dollars', () => {
    withPoints([point({ current_balance: 1000, available_balance: 900, currency_code: 'EUR' })])
    renderIt()

    expect(screen.queryByText('$1,000.00')).toBeNull()
    expect(screen.queryByText('$900.00')).toBeNull()
    expect(screen.getByText(/€1,000\.00/)).toBeInTheDocument()
    expect(screen.getByText(/€900\.00/)).toBeInTheDocument()
  })

  // Per POINT, not per series: a re-linked account can change currency, and borrowing the first
  // row's code for the whole table would silently re-denominate the others.
  it('uses each row’s own currency rather than the first row’s', () => {
    withPoints([
      point({ date: '2026-03-04', current_balance: 1000, available_balance: 900, currency_code: 'EUR' }),
      point({ date: '2026-03-05', current_balance: 2000, available_balance: 1900, currency_code: 'GBP' }),
    ])
    renderIt()

    expect(screen.getByText(/€1,000\.00/)).toBeInTheDocument()
    expect(screen.getByText(/£2,000\.00/)).toBeInTheDocument()
    expect(screen.queryByText('$2,000.00')).toBeNull()
  })

  it('still renders USD points as dollars', () => {
    withPoints([point({ current_balance: 1000, available_balance: 900, currency_code: 'USD' })])
    renderIt()

    expect(screen.getByText('$1,000.00')).toBeInTheDocument()
  })
})
