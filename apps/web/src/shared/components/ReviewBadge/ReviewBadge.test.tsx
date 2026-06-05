import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ReviewBadge } from './ReviewBadge'
import { reviewBadgeVariantFromTransaction } from './reviewBadgeUtils'

// ── ReviewBadge rendering ───────────────────────────────────────────────────

describe('ReviewBadge', () => {
  it('renders "Unreviewed" for unreviewed variant', () => {
    render(<ReviewBadge variant="unreviewed" />)
    expect(screen.getByText('Unreviewed')).toBeInTheDocument()
  })

  it('renders "Categorized" for plaid variant', () => {
    render(<ReviewBadge variant="plaid" />)
    expect(screen.getByText('Categorized')).toBeInTheDocument()
  })

  it('renders "By Rule" for rule variant', () => {
    render(<ReviewBadge variant="rule" />)
    expect(screen.getByText('By Rule')).toBeInTheDocument()
  })

  it('renders "Reviewed" for manual variant', () => {
    render(<ReviewBadge variant="manual" />)
    expect(screen.getByText('Reviewed')).toBeInTheDocument()
  })

  it('includes aria-label with status for unreviewed', () => {
    render(<ReviewBadge variant="unreviewed" />)
    expect(screen.getByLabelText('Review status: Unreviewed')).toBeInTheDocument()
  })

  it('includes aria-label with status for manual', () => {
    render(<ReviewBadge variant="manual" />)
    expect(screen.getByLabelText('Review status: Reviewed')).toBeInTheDocument()
  })

  it('applies warning styling for unreviewed variant', () => {
    render(<ReviewBadge variant="unreviewed" />)
    expect(screen.getByLabelText('Review status: Unreviewed')).toHaveClass('text-warning')
  })

  it('applies success styling for manual variant', () => {
    render(<ReviewBadge variant="manual" />)
    expect(screen.getByLabelText('Review status: Reviewed')).toHaveClass('text-success')
  })

  it('applies info styling for plaid variant', () => {
    render(<ReviewBadge variant="plaid" />)
    expect(screen.getByLabelText('Review status: Categorized')).toHaveClass('text-info')
  })

  it('applies info styling for rule variant', () => {
    render(<ReviewBadge variant="rule" />)
    expect(screen.getByLabelText('Review status: By Rule')).toHaveClass('text-info')
  })

  it('accepts className override', () => {
    render(<ReviewBadge variant="manual" className="custom-class" />)
    expect(screen.getByLabelText('Review status: Reviewed')).toHaveClass('custom-class')
  })
})

// ── reviewBadgeVariantFromTransaction helper ────────────────────────────────

describe('reviewBadgeVariantFromTransaction', () => {
  it('returns unreviewed when reviewed=false', () => {
    expect(reviewBadgeVariantFromTransaction({ reviewed: false, category_source: 'plaid' })).toBe('unreviewed')
  })

  it('returns unreviewed when reviewed is undefined', () => {
    expect(reviewBadgeVariantFromTransaction({})).toBe('unreviewed')
  })

  it('returns plaid when reviewed=true and category_source=plaid', () => {
    expect(reviewBadgeVariantFromTransaction({ reviewed: true, category_source: 'plaid' })).toBe('plaid')
  })

  it('returns rule when reviewed=true and category_source=rule', () => {
    expect(reviewBadgeVariantFromTransaction({ reviewed: true, category_source: 'rule' })).toBe('rule')
  })

  it('returns manual when reviewed=true and category_source=manual', () => {
    expect(reviewBadgeVariantFromTransaction({ reviewed: true, category_source: 'manual' })).toBe('manual')
  })

  it('returns manual when reviewed=true and category_source is undefined (fallback)', () => {
    expect(reviewBadgeVariantFromTransaction({ reviewed: true })).toBe('manual')
  })
})
