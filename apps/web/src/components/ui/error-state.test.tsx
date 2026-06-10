/**
 * ErrorState Tests
 * Issue #1267: Boosts error states — shared error UI primitive
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { ErrorState } from './error-state'

describe('ErrorState', () => {
  it('renders the title', () => {
    render(<ErrorState title="Something went wrong" />)
    expect(screen.getByText('Something went wrong')).toBeTruthy()
  })

  it('renders the description when provided', () => {
    render(<ErrorState title="Error" description="Could not load data" />)
    expect(screen.getByText('Could not load data')).toBeTruthy()
  })

  it('omits description element when not provided', () => {
    const { container } = render(<ErrorState title="Error" />)
    expect(container.querySelector('p')).toBeNull()
  })

  it('renders a Retry button when onRetry is provided', () => {
    render(<ErrorState title="Error" onRetry={() => {}} />)
    expect(screen.getByRole('button', { name: /retry/i })).toBeTruthy()
  })

  it('omits Retry button when onRetry is not provided', () => {
    render(<ErrorState title="Error" />)
    expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
  })

  it('calls onRetry when Retry button is clicked', () => {
    const onRetry = mock(() => {})
    render(<ErrorState title="Error" onRetry={onRetry} />)
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('has data-slot="error-state" attribute and role="alert" for accessibility', () => {
    const { container } = render(<ErrorState title="Error" />)
    expect(container.querySelector('[data-slot="error-state"][role="alert"]')).toBeTruthy()
  })
})
