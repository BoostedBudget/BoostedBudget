/**
 * FinalCTA Component Tests
 *
 * Issue #1398: the PostHog feature-flag kill-switch (and its "Coming Soon" disabled
 * state) was removed — the CTA always renders "Start Free Today" and opens Clerk sign-up.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
import { mockUseClerk, createMockClerk } from '@/test/mocks/clerk'
import { mockTrackEvent } from '@/test/mocks/track-event'

import { FinalCTA } from './FinalCTA'

function renderFinalCTA() {
  return render(
    <BrowserRouter>
      <FinalCTA />
    </BrowserRouter>
  )
}

describe('FinalCTA', () => {
  beforeEach(() => {
    mockUseClerk.mockClear()
    mockTrackEvent.mockClear()

    mockUseClerk.mockReturnValue(createMockClerk())
  })

  it('renders "Start Free Today" button', () => {
    renderFinalCTA()
    expect(screen.getByRole('button', { name: /start free today/i })).toBeInTheDocument()
  })

  it('"Start Free Today" button is not disabled', () => {
    renderFinalCTA()
    const btn = screen.getByRole('button', { name: /start free today/i })
    expect(btn).not.toBeDisabled()
  })

  it('does not render "Coming Soon" button', () => {
    renderFinalCTA()
    expect(screen.queryByRole('button', { name: /coming soon/i })).not.toBeInTheDocument()
  })

  it('calls openSignUp when CTA button is clicked', () => {
    const clerkMock = createMockClerk()
    mockUseClerk.mockReturnValue(clerkMock)

    renderFinalCTA()
    const btn = screen.getByRole('button', { name: /start free today/i })
    fireEvent.click(btn)
    expect(clerkMock.openSignUp).toHaveBeenCalledTimes(1)
  })

  it('fires landing_cta_clicked analytics event when CTA button is clicked', () => {
    renderFinalCTA()
    const btn = screen.getByRole('button', { name: /start free today/i })
    fireEvent.click(btn)
    expect(mockTrackEvent).toHaveBeenCalledWith('landing_cta_clicked')
  })

  it('renders the exact verbatim subhead copy', () => {
    renderFinalCTA()
    expect(
      screen.getByText(
        "Start with a 60-day free trial. Connect your accounts, automate your budget, and bring your agent along when you're ready. No credit card required."
      )
    ).toBeInTheDocument()
  })

  it('does not make the unverifiable "thousands" claim', () => {
    const { container } = renderFinalCTA()
    expect(container.textContent).not.toMatch(/thousands/i)
  })
})
