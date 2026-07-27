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
import { mockToastSuccess, mockToastError } from '@/test/mocks/sonner'
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

describe('SubscriptionOverrideModal', () => {
    beforeEach(() => {
        mockUseGetAdminUserSubscription.mockClear()
        mockUseUpdateAdminUserTier.mockClear()
        mockUseRemoveAdminUserTierOverride.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
        mockToastSuccess.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module (centralized preload)
        mockToastError.mockClear()
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
            data: { ...SUB_DETAIL, deletion_requested_at: '2026-08-01T00:00:00Z' },
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
            expect(mockTrackEvent).toHaveBeenCalledWith(
                ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED,
                expect.anything()
            )
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
                data: { ...SUB_DETAIL_WITH_OVERRIDE, manual_tier_ends_at: '2026-09-01T00:00:00Z' },
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
})
