/**
 * SubscriptionOverrideModal tests
 * Issue #646: Admin Subscription Management UI
 * 
 * Tests the form logic and display in the override modal.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SubscriptionOverrideModal } from './SubscriptionOverrideModal'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode } from 'react'
import { toast } from 'sonner'

// Mock the hook module directly
mock.module('../hooks/useAdminSubscription', () => ({
    useAdminSubscriptionDetail: mock(() => ({})),
    useUpdateUserTier: mock(() => ({})),
}))

import { useAdminSubscriptionDetail, useUpdateUserTier } from '../hooks/useAdminSubscription'

mock.module('sonner', () => ({
    toast: {
        success: mock(() => ({})),
        error: mock(() => ({})),
    },
}))

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
        accounts: 30,
        transaction_history_days: 730,
        executions_per_day: 6,
    },
    features: { ai_insights: true },
    manual_tier_override: null,
    manual_tier_ends_at: null,
}

describe('SubscriptionOverrideModal', () => {
    beforeEach(() => {
        // @ts-expect-error mock method added by bun:test mock.module
        useAdminSubscriptionDetail.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module
        useUpdateUserTier.mockClear()
        // @ts-expect-error mock method added by bun:test mock.module
        toast.success.mockClear()
    })

    it('renders loading state', () => {
        // @ts-expect-error mock method added by bun:test mock.module
        useAdminSubscriptionDetail.mockReturnValue({ isLoading: true, data: undefined })
        render(
            <SubscriptionOverrideModal userId="u1" open={true} onClose={() => { }} />,
            { wrapper }
        )
        expect(screen.getByText(/loading subscription details/i)).toBeInTheDocument()
    })

    it('renders current subscription info', async () => {
        // @ts-expect-error mock method added by bun:test mock.module
        useAdminSubscriptionDetail.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        render(
            <SubscriptionOverrideModal userId="u1" userEmail="test@example.com" open={true} onClose={() => { }} />,
            { wrapper }
        )

        expect(await screen.findByText(/test@example\.com/i)).toBeInTheDocument()
        expect(screen.getAllByText(/Premium/i)).not.toHaveLength(0)
        expect(screen.getByText(/Status: active/i)).toBeInTheDocument()
        expect(screen.getByText('30')).toBeInTheDocument()
    })

    it('submits override when form is valid', async () => {
        const mockMutate = mock((data: unknown, options: { onSuccess?: () => void } | undefined) => {
            if (options?.onSuccess) options.onSuccess()
        })
        // @ts-expect-error mock method added by bun:test mock.module
        useAdminSubscriptionDetail.mockReturnValue({ isLoading: false, data: SUB_DETAIL })
        // @ts-expect-error mock method added by bun:test mock.module
        useUpdateUserTier.mockReturnValue({ mutate: mockMutate, isPending: false })

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
            // @ts-expect-error mock method added by bun:test mock.module
            expect(toast.success).toHaveBeenCalled()
            expect(onClose).toHaveBeenCalled()
        })
    })
})
