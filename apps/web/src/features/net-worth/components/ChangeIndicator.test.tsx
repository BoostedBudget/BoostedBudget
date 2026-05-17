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
    expect(el.getAttribute('aria-label')).toBe('Net worth up $1,234 (5.20%) versus previous period')
  })

  it('renders negative variant with down glyph and destructive color', () => {
    const { container } = render(
      <ChangeIndicator variant="negative" amount={-500} percent={-2.1} />
    )
    const el = container.firstChild as HTMLElement
    expect(el.className).toContain('text-destructive')
    expect(el.getAttribute('aria-label')).toBe('Net worth down $500 (2.10%) versus previous period')
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
})
