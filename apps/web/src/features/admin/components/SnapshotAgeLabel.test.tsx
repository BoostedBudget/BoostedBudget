/**
 * SnapshotAgeLabel Component Tests
 * Issue #1462 G3 / #1432 follow-up R3-10
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
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
})
