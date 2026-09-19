/**
 * UserDetailModal Component Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for user detail modal dialog.
 */

import { describe, it, expect, vi, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
  it('offers RevenueCat diagnostics without exposing the user ID unmasked', async () => {
    render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}><UserDetailModal user={{...mockUser, as_of: '2026-09-13T10:00:00Z', effective_status: 'active', is_admin: false, referrals_count: 0}} loading={false} open={true} onClose={() => {}} /></QueryClientProvider>)
    expect(screen.getByRole('button', {name: 'Show RevenueCat diagnostics'})).toBeInTheDocument()
    expect(screen.getByText('user-1').closest('[data-ph-mask]')).not.toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Show RevenueCat diagnostics'}))
    expect(screen.getByRole('dialog').className).toContain('sm:max-w-[1100px]')
  })
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

  // Issue #1462 follow-up G2: `user.as_of` now exists on AdminUserDetail — this proves the modal
  // actually anchors its override-liveness arithmetic to it, not just that some shared `now`
  // variable was threaded through unchanged (a purely structural pin the ticket explicitly says
  // does not satisfy it). Each fixture makes the SERVER snapshot and the REAL browser clock
  // disagree about whether the override has lapsed, and asserts the rendered Comped/tier/status
  // cells follow the server's answer, not the browser's. Mirrors
  // UsersPage.test.tsx's identically-named describe block.
  describe('Clock-skew / stale-server-snapshot boundary (Issue #1462 follow-up G2)', () => {
    it('treats the override as LIVE per the server snapshot even if the browser clock alone would call it expired', () => {
      // manual_tier_ends_at is 1 hour in the PAST relative to the real wall clock at test-run
      // time — a browser-clock-only check would call this expired. But `as_of` is an hour BEFORE
      // that, i.e. the override had not yet lapsed as of the server's own snapshot.
      const endsAt = new Date(Date.now() - 60 * 60 * 1000)
      const asOf = new Date(endsAt.getTime() - 60 * 60 * 1000).toISOString()

      render(
        <UserDetailModal
          user={{
            ...mockUser,
            subscription_tier: 'household_2',
            manual_tier_override: 'premium',
            manual_tier_ends_at: endsAt.toISOString(),
            as_of: asOf,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Comped')).toBeInTheDocument()
      // Effective tier reflects the LIVE override, not the raw base tier ('household_2')
      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.queryByText('Expired')).not.toBeInTheDocument()
    })

    it('treats the override as EXPIRED per the server snapshot even though manual_tier_ends_at is still in the FUTURE relative to the real browser clock', () => {
      // manual_tier_ends_at is 30 minutes in the FUTURE relative to the real wall clock — a
      // browser-clock-only check would call this live. But `as_of` is an hour AFTER that expiry —
      // the server's own snapshot had already observed it lapse. This is the differentiating
      // case: the OLD browser-clock code would render a live-looking Comped badge for an override
      // the server already considers expired.
      const endsAt = new Date(Date.now() + 30 * 60 * 1000)
      const asOf = new Date(endsAt.getTime() + 60 * 60 * 1000).toISOString()

      render(
        <UserDetailModal
          user={{
            ...mockUser,
            subscription_tier: 'household_2',
            manual_tier_override: 'premium',
            manual_tier_ends_at: endsAt.toISOString(),
            as_of: asOf,
          }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('Comped')).not.toBeInTheDocument()
      expect(screen.getByText('Expired')).toBeInTheDocument()
      // Falls back to the genuine base tier, never the expired override's tier ('premium')
      expect(screen.getByText(/household \(2\)/i)).toBeInTheDocument()
    })
  })

  // Issue #1462 follow-up G2 / R3-10: hasLiveOverride/overrideStatus anchor their liveness
  // arithmetic to `now`, a FROZEN read of `user.as_of` (never refreshed on a timer) — this modal
  // had no disclosure of that staleness, unlike SubscriptionOverrideModal's identical
  // `SnapshotAgeLabel`. Mirrors SubscriptionOverrideModal.test.tsx's "Snapshot age label" block.
  describe('Snapshot age label (Issue #1462 G3 / R3-10)', () => {
    it("renders the snapshot's age when user.as_of is present", () => {
      const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString()

      render(
        <UserDetailModal
          user={{ ...mockUser, as_of: fiveMinutesAgo }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText(/snapshot:\s*\d+m ago/i)).toBeInTheDocument()
    })

    it('degrades to a placeholder rather than crashing when user.as_of is absent (e.g. an older cached response)', () => {
      render(
        <UserDetailModal
          user={mockUser} // no as_of field
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText(/snapshot:\s*—/i)).toBeInTheDocument()
      expect(screen.queryByText(/\bago\b/i)).not.toBeInTheDocument()
    })

    it('never renders the literal string "Invalid Date" for a malformed (present but unparseable) user.as_of', () => {
      render(
        <UserDetailModal
          user={{ ...mockUser, as_of: 'not-a-real-date' }}
          loading={false}
          open={true}
          onClose={mockOnClose}
        />
      )

      const snapshotEl = screen.getByText(/snapshot:\s*—/i)
      expect(snapshotEl).toBeInTheDocument()
      expect(document.body.textContent ?? '').not.toMatch(/invalid date/i)
    })
  })
})
