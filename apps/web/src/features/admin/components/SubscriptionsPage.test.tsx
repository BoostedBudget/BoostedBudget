/**
 * SubscriptionsPage tests
 * Issue #646: Admin Subscription Management UI
 * 
 * Migrated to bun:test to match project patterns.
 */

import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { SubscriptionsPage } from './SubscriptionsPage'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode } from 'react'

// Mock dependencies
const mockUseListAdminUsers = mock(() => ({}))

mock.module('@boostedbudget/api-client/endpoints/admin/admin', () => ({
    useListAdminUsers: mockUseListAdminUsers,
}))

// Mock SubscriptionOverrideModal
mock.module('./SubscriptionOverrideModal', () => ({
    SubscriptionOverrideModal: ({ open, userEmail }: { open: boolean; userEmail?: string }) =>
        open ? <div data-testid="override-modal">{userEmail}</div> : null,
}))

// Mock useUserFilters
mock.module('../hooks/useUserFilters', () => ({
    useUserFilters: () => ({
        filters: { search: '', tier: '', page: 1 },
        setSearch: mock(() => ({})),
        setTier: mock(() => ({})),
        setPage: mock(() => ({})),
        apiParams: {},
        hasActiveFilters: false,
    }),
}))

function wrapper({ children }: { children: ReactNode }) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return (
        <QueryClientProvider client={qc}>
            {children}
        </QueryClientProvider>
    )
}

const USERS = [
    {
        id: 'u1',
        email: 'alice@example.com',
        subscription_tier: 'premium',
        subscription_status: 'active',
        primary_region: 'us-east-1',
        linked_accounts_count: 3,
        automation_rules_count: 2,
        referrals_count: 0,
        is_admin: false,
        last_login_at: '2025-01-01T00:00:00Z',
        transactions_count: 0,
        budgets_count: 0,
    },
    {
        id: 'u2',
        email: 'bob@example.com',
        subscription_tier: 'trial',
        subscription_status: 'trial',
        primary_region: 'us-east-1',
        linked_accounts_count: 1,
        automation_rules_count: 0,
        referrals_count: 0,
        is_admin: false,
        last_login_at: null,
        transactions_count: 0,
        budgets_count: 0,
    },
]

describe('SubscriptionsPage', () => {
    beforeEach(() => {
        mockUseListAdminUsers.mockClear()
    })

    it('shows loading state initially', () => {
        mockUseListAdminUsers.mockReturnValue({ isLoading: true, data: undefined, error: null })
        render(<SubscriptionsPage />, { wrapper })
        expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('shows error state on fetch failure', () => {
        mockUseListAdminUsers.mockReturnValue({
            isLoading: false,
            data: undefined,
            error: new Error('Network'),
        })
        render(<SubscriptionsPage />, { wrapper })
        expect(screen.getByText(/failed to load users/i)).toBeInTheDocument()
    })

    it('shows empty state when no users', () => {
        mockUseListAdminUsers.mockReturnValue({
            isLoading: false,
            error: null,
            data: { users: [], total_count: 0, total_pages: 0, page: 1, has_more: false },
        })
        render(<SubscriptionsPage />, { wrapper })
        expect(screen.getByText(/no users found/i)).toBeInTheDocument()
    })

    it('renders user rows with override buttons', () => {
        mockUseListAdminUsers.mockReturnValue({
            isLoading: false,
            error: null,
            data: { users: USERS, total_count: 2, total_pages: 1, page: 1, has_more: false },
        })
        render(<SubscriptionsPage />, { wrapper })

        expect(screen.getByText('alice@example.com')).toBeInTheDocument()
        expect(screen.getByText('bob@example.com')).toBeInTheDocument()
        expect(screen.getAllByRole('button', { name: /override subscription/i })).toHaveLength(2)
    })

    it('opens the override modal when Override button is clicked', () => {
        mockUseListAdminUsers.mockReturnValue({
            isLoading: false,
            error: null,
            data: { users: USERS, total_count: 2, total_pages: 1, page: 1, has_more: false },
        })
        render(<SubscriptionsPage />, { wrapper })

        const overrideButtons = screen.getAllByRole('button', { name: /override subscription/i })
        fireEvent.click(overrideButtons[0])

        const modal = screen.getByTestId('override-modal')
        expect(modal).toBeInTheDocument()
        expect(modal).toHaveTextContent('alice@example.com')
    })
})
