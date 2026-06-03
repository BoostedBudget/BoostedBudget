/**
 * FinalCTA Component Tests
 *
 * Tests for the final CTA section with login-enabled feature flag behavior.
 * Issue #1184: Coming Soon state when login is disabled
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { mockUseClerk, createMockClerk } from '@/test/mocks/clerk'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'

// Mock useTrackEvent before FinalCTA is imported so the component always picks
// up our spy regardless of module-mock ordering from other test files.
const mockTrackEvent = mock(() => {})
mock.module('../../../shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

// Import after mock.module so the component's static dependency on useTrackEvent
// resolves to the mock above.
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
    mockUseFeatureFlagEnabled.mockClear()
    mockTrackEvent.mockClear()

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

    it('fires landing_cta_clicked analytics event when CTA button is clicked', () => {
      renderFinalCTA()
      const btn = screen.getByRole('button', { name: /start free today/i })
      fireEvent.click(btn)
      expect(mockTrackEvent).toHaveBeenCalledWith('landing_cta_clicked')
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
