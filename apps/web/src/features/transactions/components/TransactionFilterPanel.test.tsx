/**
 * TransactionFilterPanel Component Tests
 * Issue #529: Transaction page filtering
 * Issue #1193: Bug fix — Date Range + Status filters overlap at narrow viewports
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
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
  onIncludeInvestmentActivityChange: () => {},
  onActivityTypesChange: () => {},
}

function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: width })
  window.dispatchEvent(new Event('resize'))
}

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
