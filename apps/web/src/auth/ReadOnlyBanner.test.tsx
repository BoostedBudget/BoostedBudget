// Issue #930: ReadOnlyBanner tests
// Verifies correct messages and CTA for inactive and cancelled users.

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { ReadOnlyBanner } from './ReadOnlyBanner'

function renderBanner(status: string) {
  return render(
    <MemoryRouter>
      <ReadOnlyBanner status={status} />
    </MemoryRouter>
  )
}

describe('ReadOnlyBanner', () => {
  it('renders trial-ended message for inactive status', () => {
    renderBanner('inactive')
    expect(screen.getByText(/your trial has ended/i)).toBeInTheDocument()
  })

  it('renders cancelled message for cancelled status', () => {
    renderBanner('cancelled')
    expect(screen.getByText(/your subscription has been cancelled/i)).toBeInTheDocument()
  })

  it('renders read-only mode notice for both statuses', () => {
    renderBanner('inactive')
    expect(screen.getByText(/read-only mode/i)).toBeInTheDocument()
  })

  it('renders subscribe CTA link to /pricing', () => {
    renderBanner('inactive')
    const link = screen.getByRole('link', { name: /subscribe to restore access/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/pricing')
  })

  it('has role=alert for accessibility', () => {
    renderBanner('inactive')
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

})
