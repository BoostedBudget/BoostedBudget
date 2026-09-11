import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ChangeIndicator } from './ChangeIndicator'

describe('ChangeIndicator', () => {
  it('renders positive variant with up glyph and success color', () => {
    const { container } = render(
      <ChangeIndicator variant="positive" amount={1234} percent={5.2} />
    )
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('text-success')
    expect(el.getAttribute('aria-label')).toBe('Net worth up versus previous period')
  })

  it('renders negative variant with down glyph and destructive color', () => {
    const { container } = render(
      <ChangeIndicator variant="negative" amount={-500} percent={-2.1} />
    )
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('text-destructive')
    // aria-label omits numeric percent to prevent PostHog session replay capturing financial figures
    expect(el.getAttribute('aria-label')).toBe('Net worth down versus previous period')
  })

  it('renders neutral variant with muted color', () => {
    const { container } = render(
      <ChangeIndicator variant="neutral" amount={0} percent={0} />
    )
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('text-muted-foreground')
  })

  it('hides glyph from screen readers', () => {
    render(<ChangeIndicator variant="positive" amount={100} percent={1} />)
    const glyphs = screen.getAllByText(/[↑↓→]/)
    glyphs.forEach((g) => expect(g).toHaveAttribute('aria-hidden', 'true'))
  })

  it('displays formatted amount and percent', () => {
    render(<ChangeIndicator variant="positive" amount={2500} percent={10.5} />)
    expect(screen.getByText(/\$2,500/)).toBeDefined()
    expect(screen.getByText(/10\.50%/)).toBeDefined()
  })

  it('shows absolute value for negative amounts in display', () => {
    render(<ChangeIndicator variant="negative" amount={-750} percent={-3.2} />)
    expect(screen.getByText(/\$750/)).toBeDefined()
  })

  it('applies data-ph-mask to financial amount', () => {
    const { container } = render(<ChangeIndicator variant="positive" amount={1234} percent={5.2} />)
    const masked = container.querySelectorAll('[data-ph-mask]')
    expect(masked.length).toBeGreaterThan(0)
  })

  it('defaults aria-label prefix to "Net worth" when label prop is omitted', () => {
    const { container } = render(
      <ChangeIndicator variant="positive" amount={100} percent={2} />
    )
    const el = container.firstChild as HTMLElement
    expect(el.getAttribute('aria-label')).toContain('Net worth')
  })

  it('uses custom label prop in aria-label', () => {
    const { container } = render(
      <ChangeIndicator variant="positive" amount={100} percent={2} label="Portfolio" />
    )
    const el = container.firstChild as HTMLElement
    expect(el.getAttribute('aria-label')).toBe('Portfolio up versus previous period')
    expect(el.getAttribute('aria-label')).not.toContain('Net worth')
    // Numeric percent must NOT appear in aria-label (captured by PostHog attribute channel)
    expect(el.getAttribute('aria-label')).not.toMatch(/\d+(\.\d+)?%/)
  })

  it('uses custom label prop with negative variant', () => {
    const { container } = render(
      <ChangeIndicator variant="negative" amount={-300} percent={-4.5} label="Day change" />
    )
    const el = container.firstChild as HTMLElement
    expect(el.getAttribute('aria-label')).toBe('Day change down versus previous period')
  })

  // #1631 Codex round 2: this formatted every amount as USD unconditionally, and the investments
  // Portfolio card renders it directly beneath a currency-aware headline. A GBP portfolio with a
  // GBP 1,000 gain therefore printed "GBP 1,000.00" above and "$1,000" below -- the same figure
  // labelled as two different currencies. The formatter is injected rather than imported so this
  // component gains no dependency on the investments feature: `shared/`-to-feature and
  // feature-to-feature import edges are what reopened the #1601 landing-route regression on this
  // very branch.
  it('formats the amount with an injected formatter when one is given', () => {
    render(
      <ChangeIndicator
        variant="positive"
        amount={1000}
        percent={5}
        formatAmount={(n) => `\u00a3${n.toFixed(0)}`}
      />
    )
    expect(screen.getByText(/\u00a31000/)).toBeDefined()
    expect(screen.queryByText(/\$1,000/)).toBeNull()
  })

  it('is given the ABSOLUTE amount, so a loss is not double-signed by the glyph', () => {
    const seen: number[] = []
    render(
      <ChangeIndicator
        variant="negative"
        amount={-500}
        percent={-2}
        formatAmount={(n) => {
          seen.push(n)
          return `\u00a3${n.toFixed(0)}`
        }}
      />
    )
    expect(seen).toEqual([500])
  })

  it('still defaults to dollars when no formatter is injected', () => {
    render(<ChangeIndicator variant="positive" amount={1234} percent={5.2} />)
    expect(screen.getByText(/\$1,234/)).toBeDefined()
  })
})
