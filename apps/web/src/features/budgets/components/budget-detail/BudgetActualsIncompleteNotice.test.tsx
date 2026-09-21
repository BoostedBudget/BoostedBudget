/**
 * BudgetActualsIncompleteNotice tests
 * Issue #1685: the reasons the last actuals publish persisted must reach the user.
 *
 * TDD RED: written before the component exists.
 *
 * The reload guarantee is the point of this component: `actuals_incomplete_reasons` is
 * persisted server-side, so the notice is a pure function of server state. There is no
 * dismissal control and nothing is written to web storage — a persisted dismissal would
 * contradict "renders again after a page reload".
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BudgetActualsIncompleteNotice } from './BudgetActualsIncompleteNotice'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

const NOTICE = 'budget-actuals-incomplete-notice'
const REASONS = 'budget-actuals-incomplete-reasons'

/** Shapes a financial amount would take if one ever leaked into the notice. */
const CURRENCY_FORMATTED = [
  /\$\s?\d/,
  /\b\d[\d,]*\.\d{2}\b/,
  /\b\d{1,3}(?:,\d{3})+\b/,
  /[€£¥]\s?\d/,
]

describe('BudgetActualsIncompleteNotice', () => {
  beforeEach(() => {
    mockTrackEvent.mockClear()
  })

  it('lists every persisted reason', () => {
    render(
      <BudgetActualsIncompleteNotice
        reasons={['overlapping_individual_lines', 'currency_mismatch']}
      />
    )

    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    expect(screen.getByText('overlapping_individual_lines')).toBeInTheDocument()
    expect(screen.getByText('currency_mismatch')).toBeInTheDocument()
  })

  it('renders nothing when there is no reason to report', () => {
    const cases: Array<string[] | null | undefined> = [
      [],
      null,
      undefined,
      ['', '   '],
    ]

    for (const reasons of cases) {
      const { container, unmount } = render(<BudgetActualsIncompleteNotice reasons={reasons} />)
      expect(container).toBeEmptyDOMElement()
      unmount()
    }
  })

  it('renders again after a reload because it is derived from server state', () => {
    const reasons = ['overlapping_individual_lines']
    const storageBefore = window.localStorage.length
    const sessionStorageBefore = window.sessionStorage.length

    const first = render(<BudgetActualsIncompleteNotice reasons={reasons} />)
    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    first.unmount()

    // A reload re-mounts against the same persisted server state; nothing was dismissed.
    render(<BudgetActualsIncompleteNotice reasons={reasons} />)
    expect(screen.getByTestId(NOTICE)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /dismiss/i })).toBeNull()
    expect(window.localStorage.length).toBe(storageBefore)
    expect(window.sessionStorage.length).toBe(sessionStorageBefore)
  })

  it('masks the element that renders the server-provided reasons', () => {
    render(<BudgetActualsIncompleteNotice reasons={['mixed_currency']} />)

    expect(screen.getByTestId(REASONS)).toHaveAttribute('data-ph-mask')
  })

  it('never renders a financial amount', () => {
    render(
      <BudgetActualsIncompleteNotice
        reasons={['currency_mismatch', 'savings_currency_mismatch', 'invalid_manual_income']}
      />
    )

    const text = screen.getByTestId(NOTICE).textContent ?? ''
    for (const pattern of CURRENCY_FORMATTED) {
      expect(text).not.toMatch(pattern)
    }
  })

  it('fires BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED once with the distinct reason count', () => {
    render(
      <BudgetActualsIncompleteNotice
        reasons={['currency_mismatch', 'currency_mismatch', 'legacy_unknown']}
      />
    )

    expect(mockTrackEvent).toHaveBeenCalledTimes(1)
    expect(mockTrackEvent).toHaveBeenCalledWith(
      ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED,
      { reason_count: 2 }
    )
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })

  it('does not fire when there is nothing to display', () => {
    render(<BudgetActualsIncompleteNotice reasons={[]} />)

    expect(mockTrackEvent).not.toHaveBeenCalled()
  })

  it('does not re-fire for an equal reason set returned by a refetch', () => {
    const { rerender } = render(
      <BudgetActualsIncompleteNotice reasons={['currency_mismatch']} />
    )
    rerender(<BudgetActualsIncompleteNotice reasons={['currency_mismatch']} />)

    expect(mockTrackEvent).toHaveBeenCalledTimes(1)
  })

  it('fires again when the persisted reason set changes', () => {
    const { rerender } = render(
      <BudgetActualsIncompleteNotice reasons={['currency_mismatch']} />
    )
    rerender(
      <BudgetActualsIncompleteNotice reasons={['currency_mismatch', 'legacy_unknown']} />
    )

    expect(mockTrackEvent).toHaveBeenCalledTimes(2)
    expect(mockTrackEvent).toHaveBeenLastCalledWith(
      ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED,
      { reason_count: 2 }
    )
  })
})
