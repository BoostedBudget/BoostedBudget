/**
 * UserDetailModal Component Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for user detail modal dialog.
 */

import { describe, it, expect, vi, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { UserDetailModal } from './UserDetailModal'

// Mock user data
const mockUser = {
  id: 'user-1',
  email: 'admin@example.com',
  subscription_tier: 'premium',
  subscription_status: 'active',
  primary_region: 'iad',
  created_at: '2024-01-15T10:00:00Z',
  last_login_at: '2024-03-15T14:30:00Z',
  is_active: true,
  linked_accounts_count: 5,
  boosts_count: 10,
  transactions_count: 1500,
  budgets_count: 4,
}

describe('UserDetailModal', () => {
  const mockOnClose = vi.fn()

  beforeEach(() => {
    mockOnClose.mockClear()
  })

  it('should not render when open is false', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={false}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('should render when open is true', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('should show loading state when loading', () => {
    render(
      <UserDetailModal
        user={undefined}
        loading={true}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('should display user email in title', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
  })

  it('should display user profile information', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Subscription tier and status are displayed together as "Premium (active)"
    expect(screen.getByText(/premium/i)).toBeInTheDocument()
    expect(screen.getByText(/active/i)).toBeInTheDocument()
    expect(screen.getByText(/USA \(Virginia\)/i)).toBeInTheDocument()
  })

  it('should display user statistics', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText('5')).toBeInTheDocument() // accounts
    expect(screen.getByText('10')).toBeInTheDocument() // rules
    expect(screen.getByText('1,500')).toBeInTheDocument() // transactions
    expect(screen.getByText('4')).toBeInTheDocument() // budgets
  })

  it('should display created at date', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Date is formatted - just check it's there
    expect(screen.getByText(/jan/i)).toBeInTheDocument()
  })

  it('should call onClose when close button is clicked', async () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Find the footer close button (not the dialog X button)
    const closeButtons = screen.getAllByRole('button', { name: /close/i })
    // Click the last one (footer button)
    await userEvent.click(closeButtons[closeButtons.length - 1])

    expect(mockOnClose).toHaveBeenCalledTimes(1)
  })

  // Issue #1413 M10 follow-up: the admin list row already shows comp state (Comped chip +
  // Override Status column) for a user with a manual tier override; the detail modal must show
  // the same information, not "Premium (inactive)" with zero indication of the override.
  describe('manual tier override (comp) rendering', () => {
    it('should show a comp indicator and override label for a live, known-tier override', () => {
      const futureEndsAt = new Date(Date.now() + 7 * 864e5).toISOString()
      render(
        <UserDetailModal
          user={{
            ...mockUser,
            manual_tier_override: 'household_2',
            manual_tier_ends_at: futureEndsAt,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Comped')).toBeInTheDocument()
      expect(screen.getByText(/Expires in \d+d/)).toBeInTheDocument()
      // Effective tier reflects the override, not the raw subscription_tier ('premium')
      expect(screen.getByText(/household \(2\)/i)).toBeInTheDocument()
    })

    it('should NOT show a comp indicator when manual_tier_override names no configured tier', () => {
      const futureEndsAt = new Date(Date.now() + 7 * 864e5).toISOString()
      render(
        <UserDetailModal
          user={{
            ...mockUser,
            manual_tier_override: 'not_a_tier',
            manual_tier_ends_at: futureEndsAt,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('Comped')).not.toBeInTheDocument()
      // Falls back to the raw subscription_tier, never renders the malformed value itself
      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.queryByText('not_a_tier')).not.toBeInTheDocument()
    })

    it('should show the true base tier and an Expired label when the override has lapsed', () => {
      const pastEndsAt = new Date(Date.now() - 7 * 864e5).toISOString()
      render(
        <UserDetailModal
          user={{
            ...mockUser,
            subscription_tier: 'household_2',
            manual_tier_override: 'premium',
            manual_tier_ends_at: pastEndsAt,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('Comped')).not.toBeInTheDocument()
      expect(screen.getByText('Expired')).toBeInTheDocument()
      // Renders the true base tier ('household_2'), not the expired override tier ('premium')
      expect(screen.getByText(/household \(2\)/i)).toBeInTheDocument()
    })

    it('should render no comp indicator for a plain non-comped user (positive control)', () => {
      render(
        <UserDetailModal
          user={mockUser}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('Comped')).not.toBeInTheDocument()
      expect(screen.queryByText(/Expires in|Permanent|Expired/)).not.toBeInTheDocument()
    })

    // Issue #1413 M10, the CONFLICTING-SIGNAL case — this is the one that pins the regression.
    //
    // Every other test above leaves `effective_status` unset, so they cannot distinguish the correct
    // predicate from one that ALSO consults effective_status, e.g.
    //   effective_status === 'active' || isOverrideActive(...)
    // Under that regression each of them still passes: the ones asserting "Comped" is absent see an
    // unset (falsy) effective_status, and the one asserting it is present has a genuinely live
    // override anyway. That is fixture-zero masking, and it is why this case exists.
    //
    // Here the two signals DISAGREE: the user's own subscription is genuinely active — so
    // effective_status is 'active' for a reason that has nothing to do with any override — while the
    // override itself has expired. effective_status answers "does this user have write access"
    // (also true when covered by someone else's household plan); it must never answer "is there a
    // live admin override on this row". Only isOverrideActive(..., KNOWN_TIER_NAMES) may.
    // Mirrors UsersPage.test.tsx's 'Comped chip vs real active status' case.
    // Found by Codex adversarial review of the round-7 diff.
    it('does NOT show a comp indicator when effective_status is active for a non-override reason and the override has expired', () => {
      const pastEndsAt = new Date(Date.now() - 7 * 864e5).toISOString()
      render(
        <UserDetailModal
          user={{
            ...mockUser,
            subscription_tier: 'household_2',
            subscription_status: 'active',
            effective_status: 'active',
            manual_tier_override: 'premium',
            manual_tier_ends_at: pastEndsAt,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('Comped')).not.toBeInTheDocument()
      expect(screen.getByText('Expired')).toBeInTheDocument()
      // The genuine base tier, never the expired override's tier ('premium')
      expect(screen.getByText(/household \(2\)/i)).toBeInTheDocument()
    })
  })
})
