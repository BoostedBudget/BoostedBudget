/**
 * AccountSummaryHeader Component Tests
 *
 * Tests the purely presentational header region extracted from AccountCard:
 * institution row, icon+name row, type label, and balance rows.
 *
 * Issue #1298: Extract AccountSummaryHeader from AccountCard
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { AccountSummaryHeader } from './AccountSummaryHeader'

const baseProps = {
  institutionName: 'Chase',
  displayName: 'Chase Total Checking',
  typeLabel: 'Bank Account - Checking',
  currentBalance: 2500.5,
  availableBalance: 2300.5,
  currencyCode: 'USD',
}

describe('AccountSummaryHeader', () => {
  it('renders institution name with data-ph-mask', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    const el = screen.getByText('Chase')
    expect(el).toBeInTheDocument()
    expect(el).toHaveAttribute('data-ph-mask')
  })

  it('renders masked account number as ****1234 when mask provided', () => {
    render(<AccountSummaryHeader {...baseProps} mask="1234" />)
    expect(screen.getByText('****1234')).toBeInTheDocument()
  })

  it('does not render mask row when mask is not provided', () => {
    const { container } = render(<AccountSummaryHeader {...baseProps} />)
    expect(container.textContent).not.toContain('****')
  })

  it('renders display name in h3 with data-ph-mask', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    const h3 = screen.getByRole('heading', { level: 3 })
    expect(h3).toBeInTheDocument()
    // The h3 wraps a span with data-ph-mask
    const maskedSpan = h3.querySelector('[data-ph-mask]')
    expect(maskedSpan).not.toBeNull()
    expect(maskedSpan?.textContent).toBe('Chase Total Checking')
  })

  it('renders type label', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    expect(screen.getByText('Bank Account - Checking')).toBeInTheDocument()
  })

  it('renders current balance with data-ph-mask', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    const balanceEl = screen.getByText('$2,500.50')
    expect(balanceEl).toBeInTheDocument()
    expect(balanceEl).toHaveAttribute('data-ph-mask')
  })

  it('renders available balance when it differs from current', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    expect(screen.getByText('$2,300.50')).toBeInTheDocument()
  })

  it('does NOT render available balance when it equals current', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        currentBalance={2500.5}
        availableBalance={2500.5}
      />
    )
    const instances = screen.getAllByText('$2,500.50')
    expect(instances).toHaveLength(1)
  })

  it('does NOT render available balance when undefined', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        availableBalance={undefined}
      />
    )
    // Only current balance should be present
    expect(screen.getByText('$2,500.50')).toBeInTheDocument()
    expect(screen.queryByText('Available Balance')).toBeNull()
  })

  it('renders available balance as N/A when null (null treated as defined, differs from current)', () => {
    // null !== undefined, null !== currentBalance — so the row is shown with N/A
    render(
      <AccountSummaryHeader
        {...baseProps}
        availableBalance={null}
      />
    )
    expect(screen.getByText('Available Balance')).toBeInTheDocument()
    expect(screen.getByText('N/A')).toBeInTheDocument()
  })

  it('renders N/A for undefined current balance', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        currentBalance={undefined}
        availableBalance={undefined}
      />
    )
    const nas = screen.getAllByText('N/A')
    expect(nas.length).toBeGreaterThan(0)
  })

  it('renders emoji when customEmoji is set', () => {
    render(<AccountSummaryHeader {...baseProps} customEmoji="🏦" />)
    const emojiEl = screen.getByLabelText('Custom emoji')
    expect(emojiEl).toBeInTheDocument()
    expect(emojiEl.textContent).toBe('🏦')
  })

  it('does not render emoji span when customEmoji is not set', () => {
    render(<AccountSummaryHeader {...baseProps} />)
    expect(screen.queryByLabelText('Custom emoji')).toBeNull()
  })

  it('uses AccountIconDisplay when customIcon is set and no emoji', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        customIcon="wallet"
        customColor="#ff0000"
      />
    )
    // AccountIconDisplay renders an svg icon; we just verify no emoji is shown
    expect(screen.queryByLabelText('Custom emoji')).toBeNull()
    // And the display name is still rendered
    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
  })

  it('applies customColor to display name', () => {
    render(<AccountSummaryHeader {...baseProps} customColor="#ff0000" />)
    const h3 = screen.getByRole('heading', { level: 3 })
    expect(h3).toHaveStyle({ color: '#ff0000' })
  })

  it('renders "Unknown Bank" when institutionName is empty string', () => {
    render(<AccountSummaryHeader {...baseProps} institutionName="" />)
    expect(screen.getByText('Unknown Bank')).toBeInTheDocument()
  })

  it('renders institutionRowLeadingSlot inside the institution row', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        institutionRowLeadingSlot={<span data-testid="drag-handle">drag</span>}
      />
    )
    expect(screen.getByTestId('drag-handle')).toBeInTheDocument()
  })

  it('renders institutionRowTrailingSlot inside the institution row alongside mask', () => {
    render(
      <AccountSummaryHeader
        {...baseProps}
        mask="9999"
        institutionRowTrailingSlot={<span data-testid="badge">Hidden</span>}
      />
    )
    expect(screen.getByTestId('badge')).toBeInTheDocument()
    expect(screen.getByText('****9999')).toBeInTheDocument()
  })
})
