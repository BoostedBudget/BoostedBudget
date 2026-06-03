// Issue #930: ReadOnlyGuard tests
// Verifies that mutation actions are visually disabled for read-only users.

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ReadOnlyProvider } from './ReadOnlyContext'
import { ReadOnlyGuard } from './ReadOnlyGuard'

function renderGuard(isReadOnly: boolean, message?: string) {
  return render(
    <ReadOnlyProvider isReadOnly={isReadOnly}>
      <ReadOnlyGuard message={message}>
        <button>Click me</button>
      </ReadOnlyGuard>
    </ReadOnlyProvider>
  )
}

describe('ReadOnlyGuard', () => {
  it('renders children normally when not read-only', () => {
    renderGuard(false)
    expect(screen.getByRole('button', { name: /click me/i })).toBeInTheDocument()
  })

  it('renders children when read-only (disabled visually)', () => {
    renderGuard(true)
    expect(screen.getByRole('button', { name: /click me/i })).toBeInTheDocument()
  })

  it('applies pointer-events-none when read-only', () => {
    renderGuard(true)
    const disabledWrapper = screen.getByRole('button', { name: /click me/i }).parentElement
    expect(disabledWrapper?.className).toContain('pointer-events-none')
  })

  it('applies opacity-50 when read-only', () => {
    renderGuard(true)
    const disabledWrapper = screen.getByRole('button', { name: /click me/i }).parentElement
    expect(disabledWrapper?.className).toContain('opacity-50')
  })

  it('sets aria-disabled on wrapper when read-only', () => {
    renderGuard(true)
    const disabledWrapper = screen.getByRole('button', { name: /click me/i }).parentElement
    expect(disabledWrapper).toHaveAttribute('aria-disabled', 'true')
  })

  it('shows default upgrade message in title when read-only', () => {
    renderGuard(true)
    // The outer wrapper div should have the title attribute
    const container = screen.getByRole('button', { name: /click me/i }).closest('[title]')
    expect(container).toHaveAttribute('title', 'Subscribe to use this feature')
  })

  it('shows custom message when provided', () => {
    renderGuard(true, 'Resubscribe to edit transactions')
    const container = screen.getByRole('button', { name: /click me/i }).closest('[title]')
    expect(container).toHaveAttribute('title', 'Resubscribe to edit transactions')
  })

  it('does not wrap with disabled div when not read-only', () => {
    renderGuard(false)
    // When not read-only, returns children fragment directly with no wrapper with pointer-events-none
    const button = screen.getByRole('button', { name: /click me/i })
    expect(button.parentElement?.className).not.toContain('pointer-events-none')
  })

  it('applies inert attribute to outer wrapper when read-only (blocks keyboard activation)', () => {
    renderGuard(true)
    // Use hasAttribute directly — avoids jest-dom/bun single-arg toHaveAttribute edge case
    const container = screen.getByRole('button', { name: /click me/i }).closest('[title]')
    expect(container?.hasAttribute('inert')).toBe(true)
  })

  it('does not apply inert attribute when not read-only', () => {
    renderGuard(false)
    // When not read-only, children render without inert wrapper
    const button = screen.getByRole('button', { name: /click me/i })
    expect(button.closest('[inert]')).toBeNull()
  })
})
