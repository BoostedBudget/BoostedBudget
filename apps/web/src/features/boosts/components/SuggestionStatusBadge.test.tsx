/**
 * SuggestionStatusBadge Tests
 * Issue #1267: Boosts page — status pill for transfer suggestions
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { SuggestionStatusBadge } from './SuggestionStatusBadge'

describe('SuggestionStatusBadge', () => {
  it('renders "Pending" label for pending status', () => {
    render(<SuggestionStatusBadge status="pending" />)
    expect(screen.getByText('Pending')).toBeTruthy()
  })

  it('renders "Accepted" label for accepted status', () => {
    render(<SuggestionStatusBadge status="accepted" />)
    expect(screen.getByText('Accepted')).toBeTruthy()
  })

  it('renders "Completed" label for completed status', () => {
    render(<SuggestionStatusBadge status="completed" />)
    expect(screen.getByText('Completed')).toBeTruthy()
  })

  it('renders "Rejected" label for rejected status', () => {
    render(<SuggestionStatusBadge status="rejected" />)
    expect(screen.getByText('Rejected')).toBeTruthy()
  })

  it('renders "Expired" label for expired status', () => {
    render(<SuggestionStatusBadge status="expired" />)
    expect(screen.getByText('Expired')).toBeTruthy()
  })

  it('renders a span element (Badge)', () => {
    const { container } = render(<SuggestionStatusBadge status="pending" />)
    expect(container.querySelector('span')).toBeTruthy()
  })

  it('applies a data-status attribute matching the status value', () => {
    const { container } = render(<SuggestionStatusBadge status="completed" />)
    const badge = container.querySelector('[data-status="completed"]')
    expect(badge).toBeTruthy()
  })

  it('applies different Badge variants per status (pending is secondary/muted, completed is success)', () => {
    const { container: pendingContainer } = render(<SuggestionStatusBadge status="pending" />)
    const { container: completedContainer } = render(<SuggestionStatusBadge status="completed" />)

    const pendingBadge = pendingContainer.querySelector('span')
    const completedBadge = completedContainer.querySelector('span')

    // They should have different class names (different CVA variants)
    expect(pendingBadge?.className).not.toBe(completedBadge?.className)
  })
})
