/**
 * FinalCTA Component Tests
 *
 * Tests for the final CTA section with login-enabled feature flag behavior.
 * Issue #1184: Coming Soon state when login is disabled
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { FinalCTA } from './FinalCTA'
import { mockUseClerk, createMockClerk } from '@/test/mocks/clerk'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'

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
    mockUseFeatureFlagEnabled.mockClear()

    // Default: no POSTHOG_KEY → useLoginEnabled returns { enabled: true }
    delete (window as Window & { ENV?: Record<string, string> }).ENV

    mockUseClerk.mockReturnValue(createMockClerk())
  })

  describe('when loginEnabled=true (default, no PostHog key)', () => {
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
  })

  describe('when loginEnabled=false (PostHog flag disabled)', () => {
    beforeEach(() => {
      // Set PostHog key so isPostHogConfigured() returns true
      ;(window as Window & { ENV?: Record<string, string> }).ENV = { POSTHOG_KEY: 'test-key' }
      // Feature flag returns false → login disabled
      mockUseFeatureFlagEnabled.mockReturnValue(false)
    })

    it('renders "Coming Soon" button', () => {
      renderFinalCTA()
      expect(screen.getByRole('button', { name: /coming soon/i })).toBeInTheDocument()
    })

    it('"Coming Soon" button is disabled', () => {
      renderFinalCTA()
      const btn = screen.getByRole('button', { name: /coming soon/i })
      expect(btn).toBeDisabled()
    })

    it('does not render "Start Free Today" button', () => {
      renderFinalCTA()
      expect(screen.queryByRole('button', { name: /start free today/i })).not.toBeInTheDocument()
    })

    it('does not call openSignUp when Coming Soon button is in the DOM', () => {
      const clerkMock = createMockClerk()
      mockUseClerk.mockReturnValue(clerkMock)

      renderFinalCTA()
      // Button is disabled — clicking it should not call openSignUp
      const btn = screen.getByRole('button', { name: /coming soon/i })
      fireEvent.click(btn)
      expect(clerkMock.openSignUp).not.toHaveBeenCalled()
    })
  })
})
