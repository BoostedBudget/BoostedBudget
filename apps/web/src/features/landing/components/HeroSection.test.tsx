/**
 * HeroSection Component Tests
 *
 * Issue #1398: the PostHog feature-flag kill-switch (and its "Coming Soon" disabled
 * state) was removed — the CTA always renders "Start Free Today" and opens Clerk sign-up.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { HeroSection } from './HeroSection'
import { mockUseClerk, createMockClerk } from '@/test/mocks/clerk'

function renderHeroSection() {
  return render(
    <BrowserRouter>
      <HeroSection />
    </BrowserRouter>
  )
}

describe('HeroSection', () => {
  beforeEach(() => {
    mockUseClerk.mockClear()

    mockUseClerk.mockReturnValue(createMockClerk())
  })

  it('renders "Start Free Today" button', () => {
    renderHeroSection()
    expect(screen.getByRole('button', { name: /start free today/i })).toBeInTheDocument()
  })

  it('"Start Free Today" button is not disabled', () => {
    renderHeroSection()
    const btn = screen.getByRole('button', { name: /start free today/i })
    expect(btn).not.toBeDisabled()
  })

  it('does not render "Coming Soon" button', () => {
    renderHeroSection()
    expect(screen.queryByRole('button', { name: /coming soon/i })).not.toBeInTheDocument()
  })

  it('calls openSignUp when CTA button is clicked', () => {
    const clerkMock = createMockClerk()
    mockUseClerk.mockReturnValue(clerkMock)

    renderHeroSection()
    const btn = screen.getByRole('button', { name: /start free today/i })
    fireEvent.click(btn)
    expect(clerkMock.openSignUp).toHaveBeenCalledTimes(1)
  })
})
