/**
 * LandingHeader Component Tests
 *
 * Issue #1398: the PostHog feature-flag kill-switch (and its "Coming Soon" disabled
 * state) was removed — Log In / Start Free always render, on desktop and in the mobile menu.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { LandingHeader } from './LandingHeader'
import { mockUseClerk } from '@/test/mocks/clerk'

function renderLandingHeader() {
  return render(
    <BrowserRouter>
      <LandingHeader />
    </BrowserRouter>
  )
}

describe('LandingHeader', () => {
  beforeEach(() => {
    mockUseClerk.mockClear()

    mockUseClerk.mockReturnValue({
      openSignIn: () => {},
      openSignUp: () => {},
    })
  })

  it('renders Log In button (desktop)', () => {
    renderLandingHeader()
    expect(screen.getByRole('button', { name: /log in/i })).toBeInTheDocument()
  })

  it('renders Start Free button (desktop)', () => {
    renderLandingHeader()
    expect(screen.getByRole('button', { name: /start free/i })).toBeInTheDocument()
  })

  it('does not render Coming Soon badge (desktop)', () => {
    renderLandingHeader()
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument()
  })

  it('renders Log In and Start Free buttons in mobile menu', () => {
    renderLandingHeader()

    // Open the mobile menu
    const menuButton = screen.getByRole('button', { name: /open menu/i })
    fireEvent.click(menuButton)

    // Desktop CTAs remain in the DOM (only CSS-hidden), so opening the mobile
    // menu adds a second set of Log In / Start Free buttons.
    expect(screen.getAllByRole('button', { name: /log in/i })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: /start free/i })).toHaveLength(2)
  })

  it('does not render Coming Soon badge in mobile menu', () => {
    renderLandingHeader()

    // Open the mobile menu
    const menuButton = screen.getByRole('button', { name: /open menu/i })
    fireEvent.click(menuButton)

    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument()
  })
})
