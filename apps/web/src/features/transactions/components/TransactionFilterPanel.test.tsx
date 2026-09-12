/**
 * TransactionFilterPanel Component Tests
 * Issue #529: Transaction page filtering
 * Issue #1193: Bug fix — Date Range + Status filters overlap at narrow viewports
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { TransactionFilterPanel } from './TransactionFilterPanel'
import type { TransactionFilters } from '../types'

// Issue #1323: toggle-group is centrally mocked via bunfig.toml preload (toggle-group.tsx)

const defaultFilters: TransactionFilters = {}

const defaultProps = {
  filters: defaultFilters,
  datePreset: 'all' as const,
  statusFilter: 'all' as const,
  onDatePresetChange: () => {},
  onCustomDateChange: () => {},
  onStatusFilterChange: () => {},
  onReviewedChange: (_value: boolean | undefined) => {},
  onIncludeInvestmentActivityChange: () => {},
  onActivityTypesChange: () => {},
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: width })
  window.dispatchEvent(new Event('resize'))
}

describe.each([1280, 375])('Review status at width %i (#1638)', (width) => {
  beforeEach(() => setViewportWidth(width))
  afterEach(() => setViewportWidth(0))

  it('offers three distinct states with boolean callbacks', () => {
    const onReviewedChange = mock((_value: boolean | undefined) => {})
    render(<TransactionFilterPanel {...defaultProps} onReviewedChange={onReviewedChange} />)
    fireEvent.click(screen.getByRole('button', { name: /^Filters/ }))
    const group = within(screen.getByRole('group', { name: 'Review status' }))
    expect(group.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    for (const [label, value] of [['Unreviewed', false], ['Reviewed', true], ['All', undefined]] as const) {
      fireEvent.click(group.getByRole('button', { name: label }))
      expect(onReviewedChange).toHaveBeenLastCalledWith(value)
    }
  })

  it.each([false, true])('counts and clears reviewed=%s independently', (reviewed) => {
    const onReviewedChange = mock((_value: boolean | undefined) => {})
    render(<TransactionFilterPanel {...defaultProps} filters={{ reviewed }} onReviewedChange={onReviewedChange} />)
    expect(screen.getByRole('button', { name: /^Filters/ })).toHaveTextContent('1')
    expect(screen.getByText(reviewed ? 'Reviewed' : 'Unreviewed')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove Review status filter' }))
    expect(onReviewedChange).toHaveBeenCalledWith(undefined)
  })

  it('clears review in the fallback clear-all path', () => {
    const onReviewedChange = mock((_value: boolean | undefined) => {})
    render(<TransactionFilterPanel {...defaultProps} filters={{ reviewed: false }} onReviewedChange={onReviewedChange} />)
    if (width < 640) fireEvent.click(screen.getByRole('button', { name: /^Filters/ }))
    fireEvent.click(screen.getByRole('button', { name: /Clear all/i }))
    expect(onReviewedChange).toHaveBeenCalledWith(undefined)
  })

  it('disables every review control when the panel is disabled', () => {
    render(<TransactionFilterPanel {...defaultProps} disabled />)
    fireEvent.click(screen.getByRole('button', { name: /^Filters/ }))
    for (const button of within(screen.getByRole('group', { name: 'Review status' })).getAllByRole('button')) {
      expect(button).toBeDisabled()
    }
  })
})

describe('TransactionFilterPanel — mobile label consistency (F4)', () => {
  beforeEach(() => setViewportWidth(375))
  afterEach(() => setViewportWidth(0))

  it('mobile trigger button is labeled "Filters", not "Date & Status"', () => {
    render(<TransactionFilterPanel {...defaultProps} />)
    // The mobile Sheet trigger must say "Filters" — consistent with desktop header
    // and mobile SheetTitle. "Date & Status" is stale now that the investment filter
    // is also inside the sheet.
    expect(screen.getByText('Filters')).toBeInTheDocument()
    expect(screen.queryByText('Date & Status')).not.toBeInTheDocument()
  })
})

describe('TransactionFilterPanel - Bug #1193 layout fix', () => {
  beforeEach(() => setViewportWidth(1280))
  afterEach(() => setViewportWidth(0))

  it('expanded filter grid uses lg:flex-row, not sm:grid-cols-2 (prevents overlap)', () => {
    const { container } = render(<TransactionFilterPanel {...defaultProps} />)

    // Expand the panel to reveal the filter controls
    const expandBtn = screen.getByRole('button', { name: /Filters/i })
    fireEvent.click(expandBtn)

    // Bug #1193: sm:grid-cols-2 causes the DateRangePicker ToggleGroup (~826px) to
    // overflow its 50% grid column (~440px) at 1024px viewport, physically covering
    // the Status section. The fix replaces the grid with lg:flex-row.
    expect(container.querySelector('.sm\\:grid-cols-2')).toBeNull()
    expect(container.querySelector('.lg\\:flex-row')).not.toBeNull()
  })

  it('all 8 date preset buttons are present in the DOM when panel is expanded', () => {
    render(<TransactionFilterPanel {...defaultProps} />)

    const expandBtn = screen.getByRole('button', { name: /Filters/i })
    fireEvent.click(expandBtn)

    // These three were previously hidden behind the Status section at 1024px
    expect(screen.getByRole('button', { name: 'This Week' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'This Month' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last Month' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last 3 Months' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last 6 Months' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Year to Date' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'All Time' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Custom' })).toBeInTheDocument()
  })

  it('Status filter buttons are present alongside Date presets when expanded', () => {
    render(<TransactionFilterPanel {...defaultProps} />)

    const expandBtn = screen.getByRole('button', { name: /Filters/i })
    fireEvent.click(expandBtn)

    // Status buttons must co-exist in DOM with date presets (not covered)
    expect(screen.getByRole('button', { name: 'Posted' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /pending/i })).toBeInTheDocument()
  })
})

// ── #1569 T12 gap 1: the reversed-transactions opt-in ─────────────────────────────────────
describe('TransactionFilterPanel — reversed transactions (#1569 T12)', () => {
  beforeEach(() => setViewportWidth(1280))
  afterEach(() => setViewportWidth(0))

  const expand = () => fireEvent.click(screen.getByRole('button', { name: /Filters/i }))

  it('offers the opt-in, switched off, because reversals are hidden by default', () => {
    render(<TransactionFilterPanel {...defaultProps} onIncludeReversedChange={() => {}} />)
    expand()

    const toggle = screen.getByRole('switch', { name: /reversed/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle.getAttribute('aria-checked')).toBe('false')
  })

  it('reflects an opted-in filter as switched on', () => {
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ include_reversed: true }}
        onIncludeReversedChange={() => {}}
      />,
    )
    expand()

    expect(screen.getByRole('switch', { name: /reversed/i }).getAttribute('aria-checked')).toBe('true')
  })

  it('reports the opt-in as an active filter, so the user can see it is on', () => {
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ include_reversed: true }}
        onIncludeReversedChange={() => {}}
      />,
    )
    // The collapsed header carries a count badge; opting in must move it off zero.
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  it('does not count as an active filter when off', () => {
    render(<TransactionFilterPanel {...defaultProps} onIncludeReversedChange={() => {}} />)
    expect(screen.queryByText('1')).not.toBeInTheDocument()
  })

  // Fix round 1 (I6). The hint had an id and nothing referenced it, so the explanation the
  // docblock calls essential was invisible to assistive tech. "Reversed" is a word most people
  // have not had to think about; a user who turns this on blind reads struck-through rows as an
  // error in their data.
  it('describes the switch with the hint text, not just labels it', () => {
    render(<TransactionFilterPanel {...defaultProps} onIncludeReversedChange={() => {}} />)
    expand()

    const toggle = screen.getByRole('switch', { name: /reversed/i })
    const describedBy = toggle.getAttribute('aria-describedby')
    expect(describedBy).toBeTruthy()
    const hint = document.getElementById(describedBy!)
    expect(hint).not.toBeNull()
    expect(hint?.textContent).toMatch(/withdrew after they posted/i)
  })

  it('notifies the page when the user opts in', () => {
    let received: boolean | undefined
    render(
      <TransactionFilterPanel
        {...defaultProps}
        onIncludeReversedChange={(v: boolean) => { received = v }}
      />,
    )
    expand()
    fireEvent.click(screen.getByRole('switch', { name: /reversed/i }))
    expect(received).toBe(true)
  })
})

// ── #1626 Phase 2: the uncategorized filter needs a visible way out ───────────────────────
//
// The reports Sankey navigates here with ?uncategorized=true. Without a pill the user arrives at
// a filtered list with no on-screen indication of WHY it is filtered and no way to clear it
// except editing the URL — which is how a deep link becomes a bug report.
//
// A pill rather than a toggle switch is deliberate: the Sankey is the affordance that turns this
// on, and a switch for it in the panel would invite turning it on with nothing to explain what
// "uncategorized" means in a list that has a Category column.
//
// ⚠ Known limitation, recorded rather than hidden: pfc_primaries, pfc_detaileds and
// custom_category_ids are equally invisible today and equally reachable from the Sankey. This
// closes the one filter this issue introduced, not the whole class.
describe('TransactionFilterPanel — uncategorized filter (#1626)', () => {
  beforeEach(() => setViewportWidth(1280))
  afterEach(() => setViewportWidth(0))

  it('shows no uncategorized pill when the filter is off', () => {
    render(<TransactionFilterPanel {...defaultProps} onUncategorizedChange={() => {}} />)

    // 'Uncategorized:' — FilterPill renders the label with its trailing colon.
    expect(screen.queryByText('Uncategorized:')).not.toBeInTheDocument()
  })

  it('shows a pill when the filter is on, so the user can see why the list is short', () => {
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ uncategorized: true }}
        onUncategorizedChange={() => {}}
      />,
    )

    expect(screen.getByText('Uncategorized:')).toBeInTheDocument()
    // 'Included', not 'Only': the server ORs this with the other category filters, so a pill
    // reading "Only" would misdescribe a combined selection.
    expect(screen.getByText('Included')).toBeInTheDocument()
  })

  // ⚠ This assertion is load-bearing beyond the badge: hasDateStatusFilters gates whether the
  // pills section renders AT ALL. If uncategorized is not counted here, then arriving from the
  // Sankey with only this filter set renders no pills section and therefore no pill — the count
  // and the pill are the same switch, not two independent features.
  it('counts as an active filter, which is also what makes the pill render', () => {
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ uncategorized: true }}
        onUncategorizedChange={() => {}}
      />,
    )

    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByText('Uncategorized:')).toBeInTheDocument()
  })

  it('removing the pill clears the filter rather than toggling it on', () => {
    let received: boolean | undefined
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ uncategorized: true }}
        onUncategorizedChange={(v: boolean) => { received = v }}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /remove uncategorized filter/i }))
    expect(received).toBe(false)
  })

  it('is cleared by Clear all along with everything else', () => {
    let received: boolean | undefined
    render(
      <TransactionFilterPanel
        {...defaultProps}
        filters={{ uncategorized: true }}
        onUncategorizedChange={(v: boolean) => { received = v }}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /clear all/i }))
    expect(received).toBe(false)
  })
})
