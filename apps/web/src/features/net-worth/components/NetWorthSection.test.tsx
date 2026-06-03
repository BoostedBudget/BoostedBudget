import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { NetWorthSection } from './NetWorthSection'

describe('NetWorthSection', () => {
  it('renders title and children', () => {
    render(
      <NetWorthSection title="History">
        <p>chart content</p>
      </NetWorthSection>
    )
    expect(screen.getByText('History')).toBeDefined()
    expect(screen.getByText('chart content')).toBeDefined()
  })

  it('renders optional action slot', () => {
    render(
      <NetWorthSection title="Breakdown" action={<button>Export</button>}>
        <p>table</p>
      </NetWorthSection>
    )
    expect(screen.getByText('Export')).toBeDefined()
  })

  it('applies card styles', () => {
    const { container } = render(
      <NetWorthSection title="Test">
        <span />
      </NetWorthSection>
    )
    const card = container.firstChild as HTMLElement
    expect(card.className).toContain('rounded-xl')
    expect(card.className).toContain('border-border')
    expect(card.className).toContain('bg-card')
  })

  it('renders title in h2', () => {
    render(
      <NetWorthSection title="My Section">
        <span />
      </NetWorthSection>
    )
    const h2 = screen.getByRole('heading', { level: 2 })
    expect(h2.textContent).toBe('My Section')
  })
})
