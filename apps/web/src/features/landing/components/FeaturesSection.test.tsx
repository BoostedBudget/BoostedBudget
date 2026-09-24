/**
 * FeaturesSection Tests
 *
 * Issue #1666: add the agentic capability as a sixth landing feature card and
 * correct the span rule so six cards lay out cleanly (2x3, no spanning last card)
 * while five cards still close the grid with a full-width last card.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { FeaturesSection, features, cardSpanClass } from './FeaturesSection'

describe('FeaturesSection', () => {
  it('renders six feature cards', () => {
    expect(features.length).toBe(6)
    render(<FeaturesSection />)
    const headings = screen.getAllByRole('heading', { level: 3 })
    expect(headings.length).toBe(6)
  })

  it('renders the agentic capability card', () => {
    render(<FeaturesSection />)
    expect(screen.getByText('Your Agent Can Do the Digging')).toBeTruthy()
    expect(
      screen.getByText(
        'Connect BoostedBudget to Claude, Cursor, or VS Code through our MCP server. Your agent can read across your accounts, transactions, budgets, and portfolio — and every change it proposes waits for your confirmation.'
      )
    ).toBeTruthy()
  })

  it('does not span the last card when there are six cards', () => {
    const { container } = render(<FeaturesSection />)
    const cards = container.querySelectorAll('.group')
    expect(cards.length).toBe(6)
    expect(cards[5].className).not.toContain('sm:col-span-2')
  })

  it('spans the last card when there are five cards (regression guard)', () => {
    expect(cardSpanClass(4, 5)).toBe('sm:col-span-2')
    expect(cardSpanClass(5, 6)).toBe('')
  })
})
