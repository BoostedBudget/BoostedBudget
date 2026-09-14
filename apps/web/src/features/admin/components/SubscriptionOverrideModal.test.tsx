/**
 * SubscriptionOverrideModal tests
 * Issue #646: Admin Subscription Management UI
 * Issue #1413/#1414 Fix 5/6/7: PII-mask the email, warn on deletion-requested users, and
 * migrate off mock.module() for the app hook.
 * Issue #1413/#1414 Task 13b: Remove Override button + confirmation + analytics.
 *
 * Issue #1413 Fix 7: this file previously did `mock.module('../hooks/useAdminSubscription', ...)`
 * — mocking an *app hook* with bun:test's process-global mock.module(), the exact pattern this
 * repo's rules forbid because it leaks across test files (see
 * feedback_bun_test_mock_module_global.md). It only coexisted with useAdminSubscription.test.tsx
 * because that file re-mocks the api-client module underneath it. Now it consumes the
 * centralized api-client mocks in src/test/mocks/admin-subscriptions-api.ts (preloaded via
 * bunfig.toml) instead, one abstraction layer below the app hook — the real
 * useAdminSubscriptionDetail/useUpdateUserTier/useRemoveUserTierOverride hooks run for real and
 * simply call through to these mocked api-client functions.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'fs'
import { join } from 'path'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode } from 'react'
// Issue #1323: Use centralized sonner mock (preloaded via bunfig.toml).
// Inline mock.module('sonner') would clobber the global mock and pollute other tests.
import { mockToastSuccess, mockToastError, mockToastWarning, mockToastInfo } from '@/test/mocks/sonner'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
    mockUseGetAdminUserSubscription,
    mockUseUpdateAdminUserTier,
    mockUseRemoveAdminUserTierOverride,
} from '@/test/mocks/admin-subscriptions-api'
// Issue #1452: the pre-apply seat-capacity warning needs the TARGET tier's seat count, which
// lives in the tier catalogue (GET /subscription/tiers), not on the subscription-detail response.
import { mockUseGetSubscriptionTiers } from '@/test/mocks/subscription-api'

function wrapper({ children }: { children: ReactNode }) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return (
        <QueryClientProvider client={qc}>
            {children}
        </QueryClientProvider>
    )
}

const SUB_DETAIL = {
    base_tier: 'premium',
    effective_tier: 'premium',
    subscription_status: 'active',
    effective_status: 'active',
    limits: {
        transaction_history_days: 730,
    },
    features: { api_access: true },
    manual_tier_override: null,
    manual_tier_ends_at: null,
}

const SUB_DETAIL_WITH_OVERRIDE = {
    ...SUB_DETAIL,
    base_tier: 'premium',
    effective_tier: 'household_4',
    manual_tier_override: 'household_4',
    manual_tier_ends_at: null,
}

it('reports a RevenueCat grant as queued rather than applied', async () => {
    mockUseGetAdminUserSubscription.mockReturnValue({isLoading:false,data:SUB_DETAIL} as unknown as ReturnType<typeof mockUseGetAdminUserSubscription>)
    const mutate=mock((_data:unknown, options?:{onSuccess?:(data:{stage:string;message:string})=>void})=>options?.onSuccess?.({stage:'pending',message:'Complimentary access change queued.'}))
    mockUseUpdateAdminUserTier.mockReturnValue({mutate,isPending:false} as unknown as ReturnType<typeof mockUseUpdateAdminUserTier>)
    render(<SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open onClose={()=>{}}/>,{wrapper})
    fireEvent.change(screen.getByLabelText(/new tier/i),{target:{value:'household_2'}})
    fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i),{target:{value:'A grant'}})
    fireEvent.click(screen.getByRole('button',{name:/save override/i}))
    await waitFor(()=>expect(mockToastInfo).toHaveBeenCalledWith('Complimentary access change queued.'))
    expect(mockTrackEvent).not.toHaveBeenCalledWith(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED, expect.anything())
})

// G10 (AC 6): the raw subscription is inactive, but a LIVE manual override forces effective
// access to active — a fixture where raw and effective genuinely diverge, so a test can tell
// "shows both" apart from "shows raw only" (the pre-fix behavior).
const SUB_DETAIL_EFFECTIVE_VIA_OVERRIDE = {
    ...SUB_DETAIL,
    subscription_status: 'inactive',
    effective_status: 'active',
    effective_tier: 'household_4',
    manual_tier_override: 'household_4',
    manual_tier_ends_at: null,
}

// Issue #1413 AC6: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint,
// so it can hold a value naming no configured tier. The server correctly falls back to the raw
// base_tier/effective_status in this situation (pinned server-side by
// TestGetUserSubscription_MalformedOverride_FallsBackToRawTier_NotServerError in
// admin_subscriptions_test.go) — subscription_status/effective_status are deliberately "active"
// (a real, actively-paying user), so a wrongly-comped "Complimentary" badge would be an especially
// bad answer, not just a cosmetic slip.
const SUB_DETAIL_MALFORMED_OVERRIDE = {
    ...SUB_DETAIL,
    base_tier: 'premium',
    effective_tier: 'premium',
    subscription_status: 'active',
    effective_status: 'active',
    manual_tier_override: 'not_a_tier',
    manual_tier_ends_at: null,
}

// Round 8 / Task MC: the chip's status was computed from an inline `isExpired` that never
// consulted tier validity at all — only the fixture above (malformed tier + NULL expiry) had
// coverage, which cannot distinguish "the chip is gone because the tier is invalid" from "the
// chip is gone because isExpired collapsed the whole thing". Pairing the same malformed tier with
// a genuinely live, FUTURE, relative expiry isolates the reachable defect: a malformed tier with
// an expiry that hasn't lapsed. Computed from Date.now(), never a hardcoded literal — a fixed date
// here would silently drift onto (or off of) the expired branch as real time passes.
const SUB_DETAIL_MALFORMED_TIER_FUTURE_EXPIRY = {
    ...SUB_DETAIL_MALFORMED_OVERRIDE,
    manual_tier_ends_at: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString(),
}

// Round 8 / Task MC: `manual_tier_ends_at` is a TIMESTAMPTZ serialised from a typed Go *time.Time,
// so an unparseable value is not reachable through the normal API — fixed for defense in depth,
// not because it's a live production bug. household_4 is a validly-configured tier, isolating the
// date axis from the tier-name-validity axis exercised by SUB_DETAIL_MALFORMED_TIER_FUTURE_EXPIRY.
const SUB_DETAIL_MALFORMED_DATE = {
    ...SUB_DETAIL,
    base_tier: 'premium',
    effective_tier: 'premium',
    subscription_status: 'inactive',
    effective_status: 'inactive',
    manual_tier_override: 'household_4',
    manual_tier_ends_at: 'not-a-real-date',
}

// Issue #1413: the expired-override branch (' (expired)', the "Expired:" date label, and the
// effective badge falling back off a lapsed override) had zero test coverage before this fix —
// every other fixture in this file sets manual_tier_ends_at: null. household_4 is a validly
// configured tier name, so this fixture isolates the expiry axis from the tier-name-validity axis
// exercised by SUB_DETAIL_MALFORMED_OVERRIDE above.
const SUB_DETAIL_EXPIRED_OVERRIDE = {
    ...SUB_DETAIL,
    base_tier: 'premium',
    effective_tier: 'premium',
    subscription_status: 'inactive',
    effective_status: 'inactive',
    manual_tier_override: 'household_4',
    manual_tier_ends_at: '2020-01-01T00:00:00Z',
}

// deletion_requested_at/deletion_scheduled_at (Issue #1413 Fix 6/AC 10) are now real fields on
// the generated AdminSubscriptionDetailOutputBody type (backend + codegen plumbed them through),
// not an off-type field hand-fed onto the mock — these fixtures read exactly like SUB_DETAIL_WITH_OVERRIDE
// above, no intersection/cast required.
const SUB_DETAIL_DELETION_REQUESTED = {
    ...SUB_DETAIL,
    deletion_requested_at: '2026-08-01T00:00:00Z',
}

// Both dates present: deletion_scheduled_at must win over deletion_requested_at (the notice
// period has resolved into a firm date). Months are chosen far apart (January vs. November) so
// the assertion is unambiguous regardless of the test runner's local timezone.
const SUB_DETAIL_DELETION_SCHEDULED = {
    ...SUB_DETAIL,
    deletion_requested_at: '2026-01-15T12:00:00Z',
    deletion_scheduled_at: '2026-11-15T12:00:00Z',
}

// Issue #1452: the tier catalogue's per-tier `pricing.users_included` — the seat count for each
// TARGET tier a downgrade might be applied to. Mirrors config/subscription_tiers.yaml's real
// seat counts (premium=1 ... household_5=5) so the fixture reads like the real data.
const TIER_CATALOGUE = {
    tiers: {
        premium: { pricing: { users_included: 1 } },
        household_2: { pricing: { users_included: 2 } },
        household_3: { pricing: { users_included: 3 } },
        household_4: { pricing: { users_included: 4 } },
        household_5: { pricing: { users_included: 5 } },
    },
}

describe('SubscriptionOverrideModal', () => {
    beforeEach(() => {
        mockUseGetAdminUserSubscription.mockClear()
        mockUseUpdateAdminUserTier.mockClear()
        mockUseRemoveAdminUserTierOverride.mockClear()
        mockToastSuccess.mockClear()
        mockToastError.mockClear()
        mockToastWarning.mockClear()
        mockToastInfo.mockClear()
        mockTrackEvent.mockClear()
        mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: false })
        // Issue #1452: default to "not loaded" so tests that don't care about the seat-capacity
        // warning never accidentally inherit a previous test's tier catalogue.
        mockUseGetSubscriptionTiers.mockReturnValue({ data: undefined, isLoading: true, error: null, refetch: () => { } })
    })

    it('renders date picker for expires at field', () => {
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )
        expect(screen.getByPlaceholderText(/select expiry date/i)).toBeInTheDocument()
    })

    it('renders loading state', () => {
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: true, data: undefined })
        render(
            <SubscriptionOverrideModal userId="u1" open={true} onClose={() => { }} />,
            { wrapper }
        )
        expect(screen.getByText(/loading subscription details/i)).toBeInTheDocument()
    })

    it('renders current subscription info', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        expect(await screen.findByText(/test@example\.com/i)).toBeInTheDocument()
        expect(screen.getAllByText(/Premium/i)).not.toHaveLength(0)
        expect(screen.getByText(/Status: active/i)).toBeInTheDocument()
        expect(screen.getByText('730d')).toBeInTheDocument()
    })

    // G10 (AC 6): this is the surface where an admin actually decides to grant or revoke a
    // comp, so it must show BOTH raw and effective status — not raw alone. Reuses the shared
    // subscriptionPresentation derivation + existing badge variants, and follows the users
    // table's colour-independence precedent (an aria-label and a word, not colour alone).
    it('shows both the raw and the effective status, not raw alone (G10)', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({
            isLoading: false,
            data: SUB_DETAIL_EFFECTIVE_VIA_OVERRIDE,
        })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        // Raw status renders unchanged.
        expect(await screen.findByText(/Status: inactive/i)).toBeInTheDocument()
        // Effective status renders alongside it — a live override makes this user "Complimentary"
        // (the shared derivation's comped kind/label), which genuinely diverges from the raw
        // "inactive" status above, so this cannot be satisfied by raw status alone.
        const effectiveBadge = screen.getByLabelText(/effective status: complimentary/i)
        expect(effectiveBadge).toBeInTheDocument()
        expect(effectiveBadge).toHaveTextContent('Complimentary')
    })

    // Issue #1413 AC6: SubscriptionOverrideModal re-implemented override liveness inline without
    // validating manual_tier_override against known tier names, unlike UsersPage.tsx (which uses
    // isOverrideActive + KNOWN_TIER_NAMES). Because getSubscriptionPresentation's resolveKind gives
    // isComped absolute precedence over the server-computed effective_status passed one line above
    // it, the unvalidated value silently overrode the server's correct answer.
    describe('Malformed override tier-name validation (Issue #1413 AC6)', () => {
        it('treats an override naming no configured tier as no override at all — no false Complimentary badge or green chip', async () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_MALFORMED_OVERRIDE,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // The server-computed effective_status ('active') must render — NOT "Complimentary".
            // Before the fix, isComped was true whenever manual_tier_override was merely non-empty,
            // regardless of whether it named a real tier, so this rendered "Complimentary" instead.
            const effectiveBadge = await screen.findByLabelText(/effective status: active/i)
            expect(effectiveBadge).toHaveTextContent('Active')
            expect(screen.queryByLabelText(/effective status: complimentary/i)).not.toBeInTheDocument()

            // The green "Manual override active" chip must not render at all for a malformed
            // override — on the access-representation axis it is treated as no override, exactly
            // like UsersPage's Tier/Comped columns already treat the same value.
            expect(screen.queryByText(/manual override active/i)).not.toBeInTheDocument()

            // The hero "Effective tier" cell is server-computed and was already correct — pinned
            // here so the regression is visible as "two cells lying, one correct" if reintroduced.
            const effectiveTierLabel = screen.getByText('Effective tier')
            expect(effectiveTierLabel.nextElementSibling).toHaveTextContent('Premium')

            // THE COUNTERWEIGHT, and the reason the fix splits raw presence from validity rather
            // than replacing one with the other: Remove Override must STILL render for a malformed
            // override, because this row is precisely the one an admin needs to clear. Gating the
            // button on validity instead of on `hasOverride` would make a malformed comp
            // permanently unremovable — trading a display bug for a lockout, with no way out of the
            // admin UI. Nothing else pins this, so an over-tight future fix would pass every other
            // assertion in this file.
            expect(screen.getByRole('button', { name: /remove override/i })).toBeInTheDocument()
        })

        it('still shows the green chip and Complimentary badge for a validly-configured, live override (positive control)', async () => {
            // An over-tight fix that rejects every override (not just malformed ones) would
            // silently un-comp every legitimately comped user — worse than the bug being fixed.
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_WITH_OVERRIDE,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            const effectiveBadge = await screen.findByLabelText(/effective status: complimentary/i)
            expect(effectiveBadge).toHaveTextContent('Complimentary')

            const chip = screen.getByText(/manual override active/i)
            expect(chip).toBeInTheDocument()
            expect(chip.textContent).not.toMatch(/expired/i)
            expect(chip.closest('div')).toHaveClass('bg-success/10')
        })
    })

    // Round 8 / Task MC: the chip's status ("Manual override active" / "(expired)" / "Expires:"
    // vs "Expired:") was driven by an inline `isExpired` that only looked at the date, never the
    // tier — while the effective badge above it already goes through the shared, validated
    // isOverrideActive/isLiveOverride. That let the card contradict itself on the same row.
    describe('Chip status must agree with the effective badge on both axes (Issue #1413 round 8)', () => {
        it('never claims an active override for a malformed tier, even with a live future expiry (reachable case)', async () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_MALFORMED_TIER_FUTURE_EXPIRY,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // The effective badge is server-driven and already correct — this is the "other half"
            // of the same card that the chip must not contradict.
            const effectiveBadge = await screen.findByLabelText(/effective status: active/i)
            expect(effectiveBadge).toHaveTextContent('Active')
            expect(screen.queryByLabelText(/effective status: complimentary/i)).not.toBeInTheDocument()

            // The chip must not claim an active (or any) override state for a tier name that
            // names no configured tier — a live-looking future expiry must not resurrect it.
            expect(screen.queryByText(/manual override active/i)).not.toBeInTheDocument()
        })

        it('never renders the green "active" chip when manual_tier_ends_at is unparseable, for a validly-named tier (defense in depth)', async () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_MALFORMED_DATE,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // isLiveOverride already fails closed on an unparseable date (Number.isNaN check in
            // isOverrideActive), so the effective badge is already correct here — pinned as the
            // contradiction check, same as the malformed-tier test above.
            const effectiveBadge = await screen.findByLabelText(/effective status: inactive/i)
            expect(effectiveBadge).toHaveTextContent('Inactive')
            expect(screen.queryByLabelText(/effective status: complimentary/i)).not.toBeInTheDocument()

            // The tier itself is validly named, so the chip still renders (unlike the malformed-tier
            // case above) — but it must render in its non-live "(expired)" treatment, never the
            // unqualified green "active" one. `NaN < now` is `false`, which is exactly what let the
            // old inline `isExpired` fall through to the live/green branch.
            const chip = screen.getByText(/manual override active/i)
            expect(chip.textContent).toMatch(/expired/i)
            expect(chip.closest('div')).toHaveClass('bg-warning/10')
            expect(chip.closest('div')).not.toHaveClass('bg-success/10')
        })
    })

    // Issue #1413: this branch had ZERO test coverage before this fix — every fixture in this file
    // set manual_tier_ends_at: null, so ' (expired)', the "Expired:" date label, and the effective
    // badge falling back off a lapsed override were all unexercised. household_4 is a validly
    // configured tier name here, isolating the expiry axis from the tier-name-validity axis above.
    describe('Expired override branch (Issue #1413 coverage gap)', () => {
        it('shows the true base tier/status for an expired override, not a stale comp', async () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_EXPIRED_OVERRIDE,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // Raw status renders unchanged, and the effective badge must show the true (lapsed)
            // status — not "Complimentary".
            expect(await screen.findByText(/Status: inactive/i)).toBeInTheDocument()
            const effectiveBadge = screen.getByLabelText(/effective status: inactive/i)
            expect(effectiveBadge).toHaveTextContent('Inactive')
            expect(screen.queryByLabelText(/effective status: complimentary/i)).not.toBeInTheDocument()

            // The hero "Effective tier" cell shows the true base tier, not the stale override tier.
            const effectiveTierLabel = screen.getByText('Effective tier')
            expect(effectiveTierLabel.nextElementSibling).toHaveTextContent('Premium')

            // The chip still renders for a lapsed-but-validly-named override — in its existing
            // amber "(expired)" state, not hidden and not the live green state.
            const chip = screen.getByText(/manual override active \(expired\)/i)
            expect(chip.closest('div')).toHaveClass('bg-warning/10')
            expect(screen.getByText(/expired:/i)).toBeInTheDocument()
        })
    })

    // Codex adversarial review (round 9, Part 3 — Low): Round 8 stopped a malformed
    // manual_tier_ends_at from being presented as a LIVE comp (isOverrideActive/getOverrideStatus
    // both fail closed on it), but the date TEXT itself was untouched — formatDate() fed straight
    // into `new Date('not-a-real-date').toLocaleDateString(...)`, which renders the literal string
    // "Invalid Date". The chip already correctly read "(expired)" after round 8; the date after it
    // still read "Expired: Invalid Date" — user-facing nonsense downstream of the round-8 fix.
    //
    // Reachability, honestly: manual_tier_ends_at is a TIMESTAMPTZ serialised from a typed Go
    // *time.Time, so this is NOT reachable through the normal API today. This is defense-in-depth
    // and internal consistency, not a live bug.
    describe('Malformed expiry date text (Issue #1413 round 9, Part 3 — defense in depth)', () => {
        it('never renders the literal string "Invalid Date" for an unparseable manual_tier_ends_at', async () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: SUB_DETAIL_MALFORMED_DATE,
            })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            const chip = await screen.findByText(/manual override active/i)
            const chipText = chip.closest('div')?.textContent ?? ''
            expect(chipText).not.toMatch(/invalid date/i)
            // formatDate falls back to the same em dash it already uses for a missing date, rather
            // than inventing new copy that could disagree with getOverrideStatus's "Invalid expiry"
            // label used elsewhere (UsersPage.tsx / UserDetailModal.tsx).
            expect(chipText).toMatch(/expired: —/i)
        })
    })

    it('masks the user email for PostHog session replay (Fix 5)', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        const emailEl = await screen.findByText('test@example.com')
        expect(emailEl).toHaveAttribute('data-ph-mask')
    })

    it('warns when the target user has requested account deletion (Fix 6)', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({
            isLoading: false,
            data: SUB_DETAIL_DELETION_REQUESTED,
        })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        expect(await screen.findByText(/requested account deletion/i)).toBeInTheDocument()
        expect(screen.getByText(/will not cancel it/i)).toBeInTheDocument()
    })

    it('does not show the deletion warning when no deletion has been requested', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        expect(await screen.findByText(/test@example\.com/i)).toBeInTheDocument()
        expect(screen.queryByText(/requested account deletion/i)).not.toBeInTheDocument()
    })

    it('prefers deletion_scheduled_at over deletion_requested_at when both are present (Fix 6)', async () => {
        mockUseGetAdminUserSubscription.mockReturnValue({
            isLoading: false,
            data: SUB_DETAIL_DELETION_SCHEDULED,
        })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        const alert = await screen.findByText(/requested account deletion/i)
        expect(alert).toBeInTheDocument()
        // deletion_scheduled_at (November) must be the date shown, not deletion_requested_at
        // (January) — the resolution is `deletion_scheduled_at ?? deletion_requested_at`.
        expect(screen.getByText(/Nov/)).toBeInTheDocument()
        expect(screen.queryByText(/Jan/)).not.toBeInTheDocument()
    })

    it('submits override when form is valid', async () => {
        const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
            if (options?.onSuccess) options.onSuccess()
        })
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

        const onClose = mock(() => { })

        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
            { wrapper }
        )

        const select = screen.getByLabelText(/new tier/i)
        fireEvent.change(select, { target: { value: 'household_2' } })

        const textarea = screen.getByPlaceholderText(/gifted upgrade/i)
        fireEvent.change(textarea, { target: { value: 'Happy birthday' } })

        const saveBtn = screen.getByRole('button', { name: /save override/i })
        fireEvent.click(saveBtn)

        await waitFor(() => {
            expect(mockMutate).toHaveBeenCalled()
            expect(mockToastSuccess).toHaveBeenCalled()
            expect(onClose).toHaveBeenCalled()
        })
    })

    it('fires ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED on a successful save', async () => {
        const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
            if (options?.onSuccess) options.onSuccess()
        })
        mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'household_2' } })
        fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Happy birthday' } })
        fireEvent.click(screen.getByRole('button', { name: /save override/i }))

        await waitFor(() => {
            // G7.3: assert the exact applied payload (mirrors the ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED
            // assertion below) — `expect.anything()` would pass even if `had_expiry` were dropped or
            // `tier` sent the wrong value.
            expect(mockTrackEvent).toHaveBeenCalledWith(
                ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED,
                { tier: 'household_2', had_expiry: false }
            )
        })
    })

    // G4: the backend computes a warning when comping a household owner DOWN to a smaller tier
    // risks LIFO-offboarding active members (admin_user_service.go:253-267), and appends it to
    // the response `message` (admin_subscriptions.go:117-120). Before this fix the modal's
    // onSuccess took no argument at all, so that warning never reached the admin — they only
    // ever saw a generic "Subscription tier updated successfully" toast.
    describe('Downgrade seat/offboard warning (G4)', () => {
        const WARNING_MESSAGE =
            'User subscription tier updated successfully Warning: "premium" includes 1 seat(s), ' +
            'but this household has 4 active member(s); excess members may eventually be ' +
            'offboarded unless the tier is increased or members are removed.'

        it('shows a warning-styled toast (not a plain success) when the response message carries a seat warning', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { message: string }) => void } | undefined) => {
                options?.onSuccess?.({ message: WARNING_MESSAGE })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'premium' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Gift for the holidays' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            await waitFor(() => {
                expect(mockToastWarning).toHaveBeenCalledWith(expect.stringContaining('Warning:'))
                expect(mockToastSuccess).not.toHaveBeenCalled()
                expect(onClose).toHaveBeenCalled()
            })
        })

        it('keeps the plain success toast when the response has no warning', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { message: string }) => void } | undefined) => {
                options?.onSuccess?.({ message: 'User subscription tier updated successfully' })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'household_2' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Happy birthday' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            await waitFor(() => {
                expect(mockToastSuccess).toHaveBeenCalledWith('Subscription tier updated successfully')
                expect(mockToastWarning).not.toHaveBeenCalled()
            })
        })
    })

    // Issue #1452: previously this warning was surfaced only AFTER an override was applied (a
    // post-apply toast — see "Downgrade seat/offboard warning (G4)" above, PR #1443). This proves
    // it now appears BEFORE the admin commits, computed from the new
    // `household_active_member_count` field plus the TARGET tier's `pricing.users_included` from
    // the tier catalogue (GET /subscription/tiers) — never from the CURRENT tier's own limits,
    // which describe the wrong tier for this comparison.
    describe('Pre-apply seat-capacity warning (#1452)', () => {
        it('shows the warning and does not call the mutation yet, for an over-capacity downgrade', () => {
            const mockMutate = mock(() => { })
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, base_tier: 'household_4', effective_tier: 'household_4', household_active_member_count: 4 },
            })
            mockUseGetSubscriptionTiers.mockReturnValue({ data: TIER_CATALOGUE, isLoading: false, error: null, refetch: () => { } })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'premium' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Downgrade for billing reasons' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            const dialog = screen.getByRole('alertdialog')
            expect(dialog).toHaveTextContent(/4 active member/i)
            expect(dialog).toHaveTextContent(/1 seat/i)
            expect(mockMutate).not.toHaveBeenCalled()
        })

        it('does not show the warning for an upgrade', () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                options?.onSuccess?.()
            })
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, base_tier: 'household_2', effective_tier: 'household_2', household_active_member_count: 2 },
            })
            mockUseGetSubscriptionTiers.mockReturnValue({ data: TIER_CATALOGUE, isLoading: false, error: null, refetch: () => { } })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'household_5' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Gifted upgrade' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
            expect(mockMutate).toHaveBeenCalled()
        })

        it('does not show the warning for a same-seat change', () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                options?.onSuccess?.()
            })
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, base_tier: 'household_3', effective_tier: 'household_3', household_active_member_count: 3 },
            })
            mockUseGetSubscriptionTiers.mockReturnValue({ data: TIER_CATALOGUE, isLoading: false, error: null, refetch: () => { } })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'household_3' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Extend the same tier' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
            expect(mockMutate).toHaveBeenCalled()
        })

        it('still applies the override when the admin proceeds anyway (advisory only, does not block)', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                options?.onSuccess?.()
            })
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, base_tier: 'household_4', effective_tier: 'household_4', household_active_member_count: 4 },
            })
            mockUseGetSubscriptionTiers.mockReturnValue({ data: TIER_CATALOGUE, isLoading: false, error: null, refetch: () => { } })
            mockUseUpdateAdminUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByLabelText(/new tier/i), { target: { value: 'premium' } })
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'Downgrade for billing reasons' } })
            fireEvent.click(screen.getByRole('button', { name: /save override/i }))

            const dialog = screen.getByRole('alertdialog')
            fireEvent.click(within(dialog).getByRole('button', { name: /apply override anyway/i }))

            await waitFor(() => {
                expect(mockMutate).toHaveBeenCalledWith(
                    {
                        userId: 'u1',
                        data: { tier: 'premium', reason: 'Downgrade for billing reasons', expires_at: null },
                    },
                    expect.anything()
                )
                expect(onClose).toHaveBeenCalled()
            })
        })
    })

    describe('Remove Override', () => {
        it('is not rendered when there is no override', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            expect(screen.queryByRole('button', { name: /remove override/i })).not.toBeInTheDocument()
        })

        it('renders as a destructive button when an override exists', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            const removeBtn = screen.getByRole('button', { name: /remove override/i })
            expect(removeBtn).toBeInTheDocument()
            expect(removeBtn).toHaveAttribute('data-variant', 'destructive')
        })

        it('is disabled until a reason is entered', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            const removeBtn = screen.getByRole('button', { name: /remove override/i })
            expect(removeBtn).toBeDisabled()

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            expect(removeBtn).not.toBeDisabled()
        })

        it('opens the confirmation on click and does not call the mutation', () => {
            const mockMutate = mock(() => { })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))

            expect(screen.getByRole('alertdialog')).toBeInTheDocument()
            expect(mockMutate).not.toHaveBeenCalled()
        })

        it('names the tier being removed and the fallback tier in the confirmation', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))

            const dialog = screen.getByRole('alertdialog')
            expect(dialog).toHaveTextContent(/household \(4\)/i)
            expect(dialog).toHaveTextContent(/premium/i)
        })

        it('requires typing REMOVE before the confirm button enables', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))

            const dialog = screen.getByRole('alertdialog')
            const confirmBtn = within(dialog).getByRole('button', { name: /remove override/i })
            expect(confirmBtn).toBeDisabled()

            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'wrong' } })
            expect(confirmBtn).toBeDisabled()

            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            expect(confirmBtn).not.toBeDisabled()
        })

        it('never calls the mutation when the confirmation is dismissed', () => {
            const mockMutate = mock(() => { })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))

            const dialog = screen.getByRole('alertdialog')
            fireEvent.click(within(dialog).getByRole('button', { name: /cancel/i }))

            expect(mockMutate).not.toHaveBeenCalled()
        })

        it('confirming calls the mutation with exactly { userId, data: { reason } }', async () => {
            // This test's only concern is the mutate() call arguments, so onSuccess is given a
            // well-formed `{ removed: true }` response — a real one, not `undefined` — so it
            // doesn't incidentally (and unassertedly) exercise the malformed-response branch that
            // has its own dedicated test below (Codex round 9 follow-up: this used to call
            // `onSuccess()` with no argument and assert nothing about the result, which is the
            // "test that cannot fail" pattern this file has produced repeatedly).
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { removed: boolean }) => void } | undefined) => {
                options?.onSuccess?.({ removed: true })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))

            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                expect(mockMutate).toHaveBeenCalledWith(
                    { userId: 'u1', data: { reason: 'No longer needed' } },
                    expect.anything()
                )
            })
        })

        it('reports a queued RevenueCat revocation without claiming the override was removed', async () => {
            const response = {removed: false, stage: 'pending', message: 'Revocation queued; waiting for provider readback.'}
            const mutate = mock((_data: unknown, options?: {onSuccess?: (data: typeof response) => void}) => options?.onSuccess?.(response))
            mockUseGetAdminUserSubscription.mockReturnValue({isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE} as unknown as ReturnType<typeof mockUseGetAdminUserSubscription>)
            mockUseRemoveAdminUserTierOverride.mockReturnValue({mutate, isPending: false} as unknown as ReturnType<typeof mockUseRemoveAdminUserTierOverride>)
            const onClose = mock(() => {})
            render(<SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open onClose={onClose} />, {wrapper})
            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), {target: {value: 'No longer needed'}})
            fireEvent.click(screen.getByRole('button', {name: /remove override/i}))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), {target: {value: 'REMOVE'}})
            fireEvent.click(within(dialog).getByRole('button', {name: /remove override/i}))
            await waitFor(() => expect(mockToastInfo).toHaveBeenCalledWith(response.message))
            expect(mockToastSuccess).not.toHaveBeenCalled()
            expect(mockTrackEvent).toHaveBeenCalledWith(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_CHANGE_QUEUED, {action: 'revoke', tier: SUB_DETAIL_WITH_OVERRIDE.manual_tier_override})
            expect(JSON.stringify(mockTrackEvent.mock.calls)).not.toContain(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED)
            expect(onClose).toHaveBeenCalled()
        })

        it('shows a success toast and closes the modal on success', async () => {
            // Positive control (Issue #1413 round 9 / Part 1): the real endpoint is idempotent and
            // always returns a `removed` flag (AdminRemoveTierOverrideOutputBody, confirmed in
            // packages/api-client/src/models/adminRemoveTierOverrideOutputBody.ts). Prior to this
            // fix, onSuccess never read the response at all, so this test passed while asserting
            // nothing about `removed` — it would have passed identically for a no-op response. Now
            // wired to the response so the "actually removed" branch is what's exercised here.
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { removed: boolean }) => void } | undefined) => {
                options?.onSuccess?.({ removed: true })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                expect(mockToastSuccess).toHaveBeenCalled()
                expect(onClose).toHaveBeenCalled()
            })
        })

        it('shows an error toast and keeps the modal open on failure', async () => {
            const mockMutate = mock((data: unknown, options: { onError?: (err: Error) => void } | undefined) => {
                if (options?.onError) options.onError(new Error('boom'))
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                expect(mockToastError).toHaveBeenCalled()
            })
            expect(onClose).not.toHaveBeenCalled()
            // The main modal (identifiable via the still-present Reason textarea) stays open.
            expect(screen.getByPlaceholderText(/gifted upgrade/i)).toBeInTheDocument()
        })

        it('fires ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED with { tier, had_expiry } on success', async () => {
            // Positive control (Issue #1413 round 9 / Part 1): see the note on the test above —
            // this must supply `removed: true` explicitly now that the event is gated on it.
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { removed: boolean }) => void } | undefined) => {
                options?.onSuccess?.({ removed: true })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                // Must stay LIVE (non-expired) forever, unlike SUB_DETAIL_EXPIRED_OVERRIDE's
                // deliberately fixed past date above — a hardcoded future literal here would
                // silently flip this fixture onto the expired branch once the calendar caught up,
                // changing what the test exercises without failing. Computed relative to now,
                // matching the established pattern in overrideStatus.test.ts.
                data: { ...SUB_DETAIL_WITH_OVERRIDE, manual_tier_ends_at: new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString() },
            })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                expect(mockTrackEvent).toHaveBeenCalledWith(
                    ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED,
                    { tier: 'household_4', had_expiry: true }
                )
            })
        })

        // Codex adversarial review (round 9, Part 1 — Medium): the clear endpoint is deliberately
        // idempotent (backend/internal/huma/operations/admin_subscriptions.go ~:127-155) and returns
        // 200 with `removed: false` when there was no override to remove (two admins racing, or one
        // double-submit). onSuccess previously ignored the response body entirely, so a no-op
        // removal still produced a false "removed successfully" toast AND a false
        // ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED analytics event — the exact "UI tells the admin
        // something untrue" bug this whole PR exists to fix. The backend's own equivalent event is
        // already gated on `row.Removed` (admin_user_service.go:427); this brings the frontend in
        // line so the two layers agree about when a removal actually happened.
        it('does not toast success or fire ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED for a no-op removal (removed: false)', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: { removed: boolean }) => void } | undefined) => {
                options?.onSuccess?.({ removed: false })
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                // The modal still closes — a definitive "nothing to remove" answer is a completed
                // interaction, not a pending one — but it must not claim a removal happened.
                expect(onClose).toHaveBeenCalled()
            })
            expect(mockToastSuccess).not.toHaveBeenCalled()
            expect(mockTrackEvent).not.toHaveBeenCalledWith(
                ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED,
                expect.anything()
            )
            // A truthful message must still reach the admin instead of silence.
            expect(mockToastInfo).toHaveBeenCalledWith(expect.stringMatching(/no override/i))
        })

        // Codex adversarial review (round 9 follow-up, on the fix above): the generated contract
        // requires `removed: boolean` to ALWAYS be present on a 200 response
        // (adminRemoveTierOverrideOutputBody.ts:37-38) — there is no "success but ambiguous" shape
        // on the wire. A response missing it isn't a real third outcome, it's the client and
        // server disagreeing about the contract. This must not be silently folded into EITHER the
        // "removed" branch (could fire a false analytics event for a removal that may not have
        // happened) or the "not removed" branch (the original bug this whole fix targets — it
        // would swallow a REAL removal with zero admin-visible signal). It must surface as its own
        // anomaly, exactly like any other malformed response, with the modal staying open (not
        // auto-closing on an outcome nobody can actually vouch for).
        it('treats a response missing `removed` as an anomaly — no success/info toast, no analytics event, modal stays open', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: (resp: unknown) => void } | undefined) => {
                options?.onSuccess?.(undefined)
            })
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: mockMutate, isPending: false })
            const onClose = mock(() => { })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={onClose} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            const dialog = screen.getByRole('alertdialog')
            fireEvent.change(within(dialog).getByLabelText(/type/i), { target: { value: 'REMOVE' } })
            fireEvent.click(within(dialog).getByRole('button', { name: /remove override/i }))

            await waitFor(() => {
                expect(mockToastError).toHaveBeenCalledWith(expect.stringMatching(/unexpected response/i))
            })
            expect(mockToastSuccess).not.toHaveBeenCalled()
            expect(mockToastInfo).not.toHaveBeenCalled()
            expect(mockTrackEvent).not.toHaveBeenCalledWith(
                ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED,
                expect.anything()
            )
            // An outcome nobody can vouch for must not auto-close the modal — mirrors onError.
            expect(onClose).not.toHaveBeenCalled()
            expect(screen.getByPlaceholderText(/gifted upgrade/i)).toBeInTheDocument()
        })

        it('disables both footer buttons while the removal is pending', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: true })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            expect(screen.getByRole('button', { name: /save override/i })).toBeDisabled()
            expect(screen.getByRole('button', { name: /removing/i })).toBeDisabled()
        })
    })

    // G7 Fix 8: two structural properties that are implemented correctly today but were
    // asserted nowhere, so a refactor could silently reintroduce either with a fully green
    // suite. These assertions would fail if either were moved.
    describe('Structural invariants (G7 Fix 8)', () => {
        it('keeps the confirmation dialog mounted independently of the main dialog (sibling, not nested)', () => {
            // ConfirmationDialog sits alongside <Dialog>, both children of the same top-level
            // fragment — NOT nested inside <Dialog>/<DialogContent>. If it were ever moved to be a
            // JSX descendant of the main Dialog instead, Radix would unmount it the moment the main
            // dialog's `open` prop goes false, even though the confirmation's own `confirmRemove`
            // state is untouched. Toggling `open` on the wrapper (a prop this test fully controls)
            // while the confirmation is showing exercises exactly that.
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            const { rerender } = render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            fireEvent.change(screen.getByPlaceholderText(/gifted upgrade/i), { target: { value: 'No longer needed' } })
            fireEvent.click(screen.getByRole('button', { name: /remove override/i }))
            expect(screen.getByRole('alertdialog')).toBeInTheDocument()

            rerender(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={false} onClose={() => { }} />
            )

            expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
            expect(screen.getByRole('alertdialog')).toBeInTheDocument()
        })

        it('renders the Reason field outside the "Set Override" section so it visibly governs both actions', () => {
            // Reason is shared by Save Override AND Remove Override — it sits as its own sibling
            // section below "Set Override", not nested inside that fieldset. If it were ever moved
            // inside "Set Override", it would misleadingly read as governing only Save Override.
            mockUseGetAdminUserSubscription.mockReturnValue({ isLoading: false, data: SUB_DETAIL_WITH_OVERRIDE })
            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            const setOverrideSection = screen.getByText('Set Override').closest('div')
            const reasonTextarea = screen.getByPlaceholderText(/gifted upgrade/i)

            expect(setOverrideSection).not.toBeNull()
            expect(setOverrideSection?.contains(reasonTextarea)).toBe(false)
        })
    })

    // Codex adversarial review (round 9 follow-up, on the Part 2 fix) originally pinned that this
    // component's three call sites PASS a shared `now` rather than each independently reading the
    // clock. Issue #1462 changes WHERE that shared `now` comes from (the server's `sub.as_of`
    // snapshot, not a fresh browser-clock read) — obsoleting the original pin's literal
    // `const now = new Date()` assertion — but the underlying same-render invariant (one shared
    // instant, threaded into every call site) still holds and is still worth pinning structurally,
    // for the same "test that cannot fail behaviorally" reason the original comment gave: a
    // genuine clock-skew/race window isn't reliably forceable through a rendered component.
    describe('Shared `now` anchored to the server snapshot, not the browser clock (Issue #1462)', () => {
        const SOURCE = readFileSync(join(import.meta.dir, 'SubscriptionOverrideModal.tsx'), 'utf-8')

        it('derives `now` from `sub.as_of`, never a fresh browser-clock read', () => {
            // The round-9 pin asserted a literal `const now = new Date()`. That shape is now
            // OBSOLETE BY DESIGN (Issue #1462) — asserting its absence here is deliberate: a
            // regression that restores a bare `new Date()` would silently reintroduce the
            // browser/server clock-skew divergence this fix exists to close.
            expect(SOURCE).not.toMatch(/const now = new Date\(\)/)
            expect(SOURCE).toMatch(/sub\?\.as_of/)
            // The round-9 "exactly one shared `now` binding per render" invariant is unchanged —
            // only its source changed from a literal clock read to the server snapshot.
            const declarations = SOURCE.match(/const now = /g) ?? []
            expect(declarations).toHaveLength(1)
        })

        it('still passes that single binding into getOverrideStatus and both isOverrideActive calls (round 9 same-render invariant)', () => {
            expect(SOURCE).toMatch(/getOverrideStatus\([^)]*,\s*now\)/)

            const isOverrideActiveCalls = SOURCE.match(/isOverrideActive\([^)]*\)/g) ?? []
            // isKnownTierOverride and isLiveOverride — the two liveness checks this component makes.
            expect(isOverrideActiveCalls).toHaveLength(2)
            for (const call of isOverrideActiveCalls) {
                expect(call).toMatch(/,\s*now\)$/)
            }
        })
    })

    // Issue #1462: a fixture where the SERVER's snapshot (`effective_status` + `as_of`) and the
    // BROWSER's real-time clock disagree about whether the override has expired — the exact
    // "stale server snapshot" axis the round-9 fix never covered (that fix only proved both
    // client-side call sites shared ONE clock read, not that the clock read agreed with the
    // server). Before this fix, the chip (client-clock liveness) and the effective badge
    // (server-computed status) could contradict each other in this window; after it, both are
    // anchored to `as_of` and must agree.
    describe('Clock-skew / stale-server-snapshot boundary (Issue #1462)', () => {
        it('treats the override as LIVE per the server snapshot even if the browser clock alone would call it expired', async () => {
            // as_of is BEFORE manual_tier_ends_at (override still live as of the server's snapshot
            // instant) — effective_status reflects that (a live comp forces "active"). The real
            // wall clock at test-run time may already be past manual_tier_ends_at (simulating
            // request latency / clock skew between the snapshot and this render); the component
            // must still render as live, because it evaluates `manual_tier_ends_at` against
            // `as_of`, never against `Date.now()`.
            const endsAt = new Date(Date.now() - 60 * 60 * 1000) // 1 hour in the past, real-clock-wise
            const asOf = new Date(endsAt.getTime() - 60 * 60 * 1000).toISOString() // 1 hour before that
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: {
                    ...SUB_DETAIL,
                    subscription_status: 'inactive',
                    effective_status: 'active',
                    effective_tier: 'household_4',
                    manual_tier_override: 'household_4',
                    manual_tier_ends_at: endsAt.toISOString(),
                    as_of: asOf,
                },
            })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // The chip must read as a LIVE override (no "(expired)"), consistent with the
            // "Complimentary" badge effective_status: 'active' produces — not a stale-clock
            // "(expired)" chip beside a live-looking badge.
            expect(await screen.findByText(/manual override active/i)).toBeInTheDocument()
            expect(screen.queryByText(/\(expired\)/i)).not.toBeInTheDocument()
            expect(screen.getByText(/complimentary/i)).toBeInTheDocument()
        })

        it('treats the override as EXPIRED per the server snapshot even though manual_tier_ends_at is still in the FUTURE relative to the real browser clock', async () => {
            // manual_tier_ends_at is 30 minutes in the future relative to the real wall clock at
            // test-run time — a naive browser-clock-only check (the pre-#1462 behavior) would call
            // this LIVE. But as_of (the server's snapshot instant) is an hour AFTER that expiry —
            // the server had already observed it lapse by the time it computed effective_status,
            // which reflects that (no live comp; raw status shows through). This is the
            // differentiating case: the OLD `now = new Date()` code would render a live-looking
            // "Manual override active" chip (and, since isComped wins outright, a wrongly
            // "Complimentary" badge) contradicting effective_status: 'past_due' — the exact
            // one-card-two-answers bug #1462 exists to close.
            const endsAt = new Date(Date.now() + 30 * 60 * 1000)
            const asOf = new Date(endsAt.getTime() + 60 * 60 * 1000).toISOString() // 1 hour after expiry
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: {
                    ...SUB_DETAIL,
                    subscription_status: 'past_due',
                    effective_status: 'past_due',
                    effective_tier: 'premium',
                    manual_tier_override: 'household_4',
                    manual_tier_ends_at: endsAt.toISOString(),
                    as_of: asOf,
                },
            })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // The chip must read as EXPIRED, consistent with effective_status: 'past_due' NOT
            // rendering the "Complimentary" badge — not a live-looking chip beside a non-comped
            // badge.
            expect(await screen.findByText(/\(expired\)/i)).toBeInTheDocument()
            expect(screen.queryByText(/complimentary/i)).not.toBeInTheDocument()
        })
    })

    // Issue #1462 G3: `as_of` is consumed (anchors `now`, above) but was never RENDERED — so an
    // admin had no way to tell a freshly-opened modal from one that had been sitting open for
    // several minutes off a frozen snapshot (useAdminSubscriptionDetail has no refetchInterval,
    // and the global QueryClient sets staleTime: 5 minutes + refetchOnWindowFocus: false — see
    // main.tsx — so nothing refreshes an open modal on a timer). #1462 option 3 was conjunctive:
    // anchor to the server clock AND label the snapshot's age; only the first half shipped
    // originally.
    describe('Snapshot age label (Issue #1462 G3)', () => {
        it("renders the snapshot's age next to the effective-status badge when as_of is present", () => {
            const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString()
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, as_of: fiveMinutesAgo },
            })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // Bucketed by formatRelativeTime (minutes, not an exact value) — the point is that a
            // real, non-empty relative-age label renders from `as_of`, not the precise minute
            // boundary (real wall-clock time elapses between fixture construction and assertion).
            expect(screen.getByText(/snapshot:\s*\d+m ago/i)).toBeInTheDocument()
        })

        it('degrades to a placeholder rather than crashing when as_of is absent (e.g. an older cached response)', () => {
            // SUB_DETAIL (the base fixture used throughout this file) never sets `as_of` — this
            // deliberately exercises that "absent" shape, matching an older cached response from
            // before this field existed.
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL },
            })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // Must not crash, and must not silently render an empty/misleading age — falls back
            // to the same em-dash placeholder convention formatDate already uses in this file.
            expect(screen.getByText(/snapshot:\s*—/i)).toBeInTheDocument()
            expect(screen.queryByText(/ago/i)).not.toBeInTheDocument()
        })

        // Codex adversarial review finding (recurrence of the round-9 Part 3 "Invalid Date"
        // defect, this time on `as_of` rather than `manual_tier_ends_at`): unlike the "absent"
        // case above, a MALFORMED-but-truthy `as_of` used to reach `formatRelativeTime` (the
        // label) and `new Date(...).toLocaleString()` (the title) with no parse guard at all —
        // both render the literal string "Invalid Date" for an unparseable input.
        // `resolveServerSnapshotTime` already failed closed for `now`'s own liveness arithmetic,
        // but that only protects the chip/badge — this label and title are a separate code path
        // that had no equivalent guard until this fix.
        it('never renders the literal string "Invalid Date" for a malformed (present but unparseable) as_of', () => {
            mockUseGetAdminUserSubscription.mockReturnValue({
                isLoading: false,
                data: { ...SUB_DETAIL, as_of: 'not-a-real-date' },
            })

            render(
                <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
                { wrapper }
            )

            // Degrades to the same em-dash placeholder as the "absent" case, in both the visible
            // label and the title attribute — never the literal string "Invalid Date" in either.
            const snapshotEl = screen.getByText(/snapshot:\s*—/i)
            expect(snapshotEl).toBeInTheDocument()
            expect(document.body.textContent ?? '').not.toMatch(/invalid date/i)
            const titledAncestor = snapshotEl.closest('[title]')
            expect(titledAncestor?.getAttribute('title') ?? '').not.toMatch(/invalid date/i)
        })
    })
})
