/**
 * LandingHeader Component Tests
 *
 * Tests for the landing page header with login-enabled feature flag behavior.
 * Issue #1184: Coming Soon state when login is disabled
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { LandingHeader } from './LandingHeader'
import { mockUseClerk } from '@/test/mocks/clerk'
import { mockUseFeatureFlagEnabled } from '@/test/mocks/posthog'

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
    mockUseFeatureFlagEnabled.mockClear()

    mockUseClerk.mockReturnValue({
      openSignIn: () => {},
      openSignUp: () => {},
    })

    // Default: no POSTHOG_KEY → useLoginEnabled returns { enabled: true }
    delete (window as Window & { ENV?: Record<string, string> }).ENV
  })

  describe('when loginEnabled=true (default, no PostHog key)', () => {
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
  })

  describe('when loginEnabled=false (PostHog flag disabled)', () => {
    beforeEach(() => {
      // Set PostHog key so isPostHogConfigured() returns true
      ;(window as Window & { ENV?: Record<string, string> }).ENV = { POSTHOG_KEY: 'test-key' }
      // Feature flag returns false → login disabled
      mockUseFeatureFlagEnabled.mockReturnValue(false)
    })

    afterEach(() => {
      delete (window as Window & { ENV?: Record<string, string> }).ENV
      mockUseFeatureFlagEnabled.mockReturnValue(undefined)
    })

    it('renders Coming Soon badge when login is disabled (desktop)', () => {
      renderLandingHeader()
      expect(screen.getByText(/coming soon/i)).toBeInTheDocument()
    })

    it('does not render Log In button when login is disabled (desktop)', () => {
      renderLandingHeader()
      expect(screen.queryByRole('button', { name: /log in/i })).not.toBeInTheDocument()
    })

    it('does not render Start Free button when login is disabled (desktop)', () => {
      renderLandingHeader()
      expect(screen.queryByRole('button', { name: /start free/i })).not.toBeInTheDocument()
    })

    it('renders Coming Soon badge in mobile menu when login is disabled', () => {
      renderLandingHeader()

      // Open the mobile menu
      const menuButton = screen.getByRole('button', { name: /open menu/i })
      fireEvent.click(menuButton)

      // Coming Soon badge is present in mobile menu
      const badges = screen.getAllByText(/coming soon/i)
      expect(badges.length).toBeGreaterThanOrEqual(1)
    })

    it('does not render Log In or Start Free buttons in mobile menu when login is disabled', () => {
      renderLandingHeader()

      // Open the mobile menu
      const menuButton = screen.getByRole('button', { name: /open menu/i })
      fireEvent.click(menuButton)

      // Auth buttons must be absent — Coming Soon replaces them
      expect(screen.queryAllByRole('button', { name: /log in/i })).toHaveLength(0)
      expect(screen.queryAllByRole('button', { name: /start free/i })).toHaveLength(0)
    })
  })
})
