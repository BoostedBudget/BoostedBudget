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
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode } from 'react'
// Issue #1323: Use centralized sonner mock (preloaded via bunfig.toml).
// Inline mock.module('sonner') would clobber the global mock and pollute other tests.
import { mockToastSuccess, mockToastError, mockToastWarning } from '@/test/mocks/sonner'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
    mockUseGetAdminUserSubscription,
    mockUseUpdateAdminUserTier,
    mockUseRemoveAdminUserTierOverride,
} from '@/test/mocks/admin-subscriptions-api'

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

describe('SubscriptionOverrideModal', () => {
    beforeEach(() => {
        mockUseGetAdminUserSubscription.mockClear()
        mockUseUpdateAdminUserTier.mockClear()
        mockUseRemoveAdminUserTierOverride.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
        mockToastSuccess.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
        mockToastError.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
        mockToastWarning.mockClear()
        mockTrackEvent.mockClear()
        mockUseRemoveAdminUserTierOverride.mockReturnValue({ mutate: () => { }, isPending: false })
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
            // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
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
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
                expect(mockToastWarning).toHaveBeenCalledWith(expect.stringContaining('Warning:'))
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
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
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
                expect(mockToastSuccess).toHaveBeenCalledWith('Subscription tier updated successfully')
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
                expect(mockToastWarning).not.toHaveBeenCalled()
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
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                if (options?.onSuccess) options.onSuccess()
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

        it('shows a success toast and closes the modal on success', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                if (options?.onSuccess) options.onSuccess()
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
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
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
                // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
                expect(mockToastError).toHaveBeenCalled()
            })
            expect(onClose).not.toHaveBeenCalled()
            // The main modal (identifiable via the still-present Reason textarea) stays open.
            expect(screen.getByPlaceholderText(/gifted upgrade/i)).toBeInTheDocument()
        })

        it('fires ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED with { tier, had_expiry } on success', async () => {
            const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
                if (options?.onSuccess) options.onSuccess()
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
})
