/**
 * HeroSection Component Tests
 *
 * Issue #1398: the PostHog feature-flag kill-switch (and its "Coming Soon" disabled
 * state) was removed — the CTA always renders "Start Free Today" and opens Clerk sign-up.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router'
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

  // Issue #1666 Wave 2 item 5: the hero carries the agentic story.
  it('renders the agentic badge', () => {
    renderHeroSection()
    expect(screen.getByText('Now with an MCP server for your agent')).toBeInTheDocument()
  })

  it('renders the agentic subheadline verbatim', () => {
    renderHeroSection()
    expect(
      screen.getByText(
        'Connect every account, automate what you can, and hand the busywork to your AI agent. It can read everything — and changes nothing without your confirmation.'
      )
    ).toBeInTheDocument()
  })

  it('keeps the headline unchanged', () => {
    renderHeroSection()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      'Take Control of Your Money'
    )
  })

  it('drops the unverifiable "thousands" adoption claim', () => {
    const { container } = renderHeroSection()
    expect(container.textContent ?? '').not.toContain('thousands')
  })
})
