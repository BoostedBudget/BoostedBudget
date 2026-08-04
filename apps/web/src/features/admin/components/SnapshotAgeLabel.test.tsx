/**
 * SnapshotAgeLabel Component Tests
 * Issue #1462 G3 / #1432 follow-up R3-10 / #1432 follow-up S4
 */
import { describe, it, expect, setSystemTime, afterEach } from 'bun:test'
import { act, render, screen } from '@testing-library/react'
import { SnapshotAgeLabel } from './SnapshotAgeLabel'

describe('SnapshotAgeLabel', () => {
  it("renders the snapshot's age when as_of is present and parsable", () => {
    const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString()
    render(<SnapshotAgeLabel asOf={fiveMinutesAgo} />)

    // Bucketed by formatRelativeTime (minutes, not an exact value) — the point is that a real,
    // non-empty relative-age label renders, not the precise minute boundary.
    expect(screen.getByText(/snapshot:\s*\d+m ago/i)).toBeInTheDocument()
  })

  it('degrades to an em-dash placeholder rather than crashing when as_of is absent', () => {
    render(<SnapshotAgeLabel asOf={null} />)

    expect(screen.getByText(/snapshot:\s*—/i)).toBeInTheDocument()
    expect(screen.queryByText(/ago/i)).not.toBeInTheDocument()
  })

  it('degrades to the same em-dash placeholder rather than crashing when as_of is undefined', () => {
    render(<SnapshotAgeLabel asOf={undefined} />)

    expect(screen.getByText(/snapshot:\s*—/i)).toBeInTheDocument()
  })

  it('never renders the literal string "Invalid Date" for a malformed (present but unparseable) as_of', () => {
    render(<SnapshotAgeLabel asOf="not-a-real-date" />)

    const snapshotEl = screen.getByText(/snapshot:\s*—/i)
    expect(snapshotEl).toBeInTheDocument()
    expect(document.body.textContent ?? '').not.toMatch(/invalid date/i)
    const titledAncestor = snapshotEl.closest('[title]')
    expect(titledAncestor?.getAttribute('title') ?? '').not.toMatch(/invalid date/i)
  })

  // Same ISO-strict guard as resolveServerSnapshotTime/isParsableDate (Defect R3-4) — a
  // malformed-but-numeric as_of must not slip through here either.
  it('treats a malformed-but-numeric as_of (e.g. a bare year) as unparseable, not a valid snapshot', () => {
    render(<SnapshotAgeLabel asOf="2026" />)

    expect(screen.getByText(/snapshot:\s*—/i)).toBeInTheDocument()
  })

  it('renders a title attribute with the absolute snapshot time when as_of is parsable', () => {
    const asOf = '2026-01-15T09:00:00Z'
    render(<SnapshotAgeLabel asOf={asOf} />)

    const snapshotEl = screen.getByText(/snapshot:/i)
    const titledAncestor = snapshotEl.closest('[title]')
    expect(titledAncestor?.getAttribute('title')).toMatch(/snapshot captured/i)
  })

  it('renders no title attribute when as_of is absent', () => {
    render(<SnapshotAgeLabel asOf={null} />)

    const snapshotEl = screen.getByText(/snapshot:/i)
    expect(snapshotEl.closest('[title]')).toBeNull()
  })

  // #1432 follow-up S4: before the strictness fix, a PARTIAL Go time.Time-shaped string (no
  // time-of-day at all) was accepted by the old isParsableDate (via a bare `RFC3339_DATE_TIME_
  // PREFIX` + lenient `parseISO`), so `asOfIsParsable` was true but `formatRelativeTime`'s
  // internal `new Date('2026-08-02T')` — and the title's own `new Date(...)` — both produced an
  // Invalid Date, and the component rendered the literal string "Snapshot: Invalid Date" /
  // "Snapshot captured Invalid Date". Reproduced directly against the pre-fix component before
  // writing this test (see task report). The strict Go-time-wire-format check now rejects this
  // shape outright, so it degrades to the em-dash placeholder like any other malformed value.
  it('degrades to em-dash (never "Invalid Date") for a partial ISO string with no time-of-day', () => {
    render(<SnapshotAgeLabel asOf="2026-08-02T" />)

    const snapshotEl = screen.getByText(/snapshot:\s*—/i)
    expect(snapshotEl).toBeInTheDocument()
    expect(document.body.textContent ?? '').not.toMatch(/invalid date/i)
    expect(snapshotEl.closest('[title]')).toBeNull()
  })

  // Task 5 (#1432 follow-up S4): the existing tests above all sit at ~5 minutes, so the
  // hours-old and days-old formatting branches SnapshotAgeLabel exists to disclose never ran.
  it('renders the snapshot age in hours when as_of is hours old', () => {
    const threeHoursAgo = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString()
    render(<SnapshotAgeLabel asOf={threeHoursAgo} />)

    expect(screen.getByText(/snapshot:\s*\d+h ago/i)).toBeInTheDocument()
  })

  it('renders the snapshot age in days when as_of is days old', () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString()
    render(<SnapshotAgeLabel asOf={threeDaysAgo} />)

    expect(screen.getByText(/snapshot:\s*\d+d ago/i)).toBeInTheDocument()
  })
})

// Task 4 (#1432 follow-up S4): SnapshotAgeLabel's own doc comment claims its text "keeps growing
// ('2m ago' -> '5m ago')" via a re-render interval, distinct from the surface's frozen
// `resolveServerSnapshotTime` snapshot. These tests drive that interval directly rather than
// waiting on a real 30s timer: they replace `globalThis.setInterval`/`clearInterval` with spies
// that capture the callback (real bun:test has no fake-timer/advance-timers API), then use
// `setSystemTime` to move the wall clock forward and manually invoke the captured callback inside
// `act()` to simulate the interval firing.
describe('SnapshotAgeLabel ticking', () => {
  const realSetInterval = globalThis.setInterval
  const realClearInterval = globalThis.clearInterval

  afterEach(() => {
    globalThis.setInterval = realSetInterval
    globalThis.clearInterval = realClearInterval
    setSystemTime()
  })

  it('advances its label from "2m ago" to "5m ago" when the tick interval fires', () => {
    let capturedCallback: (() => void) | null = null
    // @ts-expect-error -- test spy, narrower signature than the full setInterval overload set
    globalThis.setInterval = (fn: () => void) => {
      capturedCallback = fn
      return 1 as unknown as ReturnType<typeof setInterval>
    }
    globalThis.clearInterval = () => {}

    const frozenStart = new Date('2026-08-02T18:00:00.000Z')
    setSystemTime(frozenStart)
    const twoMinutesAgo = new Date(frozenStart.getTime() - 2 * 60 * 1000).toISOString()
    render(<SnapshotAgeLabel asOf={twoMinutesAgo} />)
    expect(screen.getByText(/snapshot:\s*2m ago/i)).toBeInTheDocument()

    // Move the wall clock forward 3 more minutes (asOf is now 5 minutes in the past) and fire
    // the captured interval callback to simulate the 30s tick — without it, `asOf` staying fixed
    // while only the system clock advances would NOT rerender or update the text on its own.
    setSystemTime(new Date(frozenStart.getTime() + 3 * 60 * 1000))
    expect(capturedCallback).not.toBeNull()
    act(() => {
      capturedCallback?.()
    })

    expect(screen.getByText(/snapshot:\s*5m ago/i)).toBeInTheDocument()
  })

  it('clears the tick interval on unmount', () => {
    let clearedCount = 0
    // @ts-expect-error -- test spy
    globalThis.setInterval = () => 42 as unknown as ReturnType<typeof setInterval>
    // @ts-expect-error -- test spy
    globalThis.clearInterval = (id: number) => {
      if (id === 42) clearedCount += 1
    }

    const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString()
    const { unmount } = render(<SnapshotAgeLabel asOf={fiveMinutesAgo} />)
    expect(clearedCount).toBe(0)

    unmount()
    expect(clearedCount).toBe(1)
  })
})
